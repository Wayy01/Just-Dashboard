package dbx

import (
	"bytes"
	"context"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// ConnInfo is the parsed form of a DSN, used to build dump and restore command
// lines. Credentials are deliberately kept out of argv — anyone with a shell on
// the box can read /proc/*/cmdline — and are passed via the environment or a
// mode-0600 defaults file instead.
type ConnInfo struct {
	Host     string
	Port     string
	User     string
	Password string
	Database string
}

// ParseDSN understands the URL form used by Postgres and Mongo, and both the
// URL and the go-sql-driver form for MySQL.
func ParseDSN(driver Driver, dsn string) (*ConnInfo, error) {
	if driver == DriverMySQL && !strings.Contains(dsn, "://") {
		return parseMySQLDSN(dsn)
	}
	u, err := url.Parse(dsn)
	if err != nil {
		return nil, fmt.Errorf("cannot parse connection string: %w", err)
	}
	info := &ConnInfo{
		Host:     u.Hostname(),
		Port:     u.Port(),
		Database: strings.TrimPrefix(u.Path, "/"),
	}
	if u.User != nil {
		info.User = u.User.Username()
		info.Password, _ = u.User.Password()
	}
	if info.Host == "" {
		info.Host = "127.0.0.1"
	}
	if info.Port == "" {
		switch driver {
		case DriverPostgres:
			info.Port = "5432"
		case DriverMySQL:
			info.Port = "3306"
		case DriverMongo:
			info.Port = "27017"
		}
	}
	return info, nil
}

// parseMySQLDSN handles user:pass@tcp(host:port)/dbname.
func parseMySQLDSN(dsn string) (*ConnInfo, error) {
	info := &ConnInfo{Host: "127.0.0.1", Port: "3306"}
	rest := dsn
	if at := strings.LastIndex(rest, "@"); at >= 0 {
		creds := rest[:at]
		rest = rest[at+1:]
		if colon := strings.Index(creds, ":"); colon >= 0 {
			info.User, info.Password = creds[:colon], creds[colon+1:]
		} else {
			info.User = creds
		}
	}
	if open := strings.Index(rest, "("); open >= 0 {
		if close := strings.Index(rest, ")"); close > open {
			hostPort := rest[open+1 : close]
			if colon := strings.LastIndex(hostPort, ":"); colon >= 0 {
				info.Host, info.Port = hostPort[:colon], hostPort[colon+1:]
			} else if hostPort != "" {
				info.Host = hostPort
			}
			rest = rest[close+1:]
		}
	}
	if slash := strings.Index(rest, "/"); slash >= 0 {
		db := rest[slash+1:]
		if q := strings.Index(db, "?"); q >= 0 {
			db = db[:q]
		}
		info.Database = db
	}
	return info, nil
}

type DumpResult struct {
	Path     string    `json:"path"`
	Size     int64     `json:"size"`
	Duration string    `json:"duration"`
	Database string    `json:"database"`
	Driver   Driver    `json:"driver"`
	StartedAt time.Time `json:"startedAt"`
	Output   string    `json:"output,omitempty"`
}

// Dump writes a backup of one database into outDir and returns where it landed.
func Dump(ctx context.Context, driver Driver, dsn, database, outDir string) (*DumpResult, error) {
	info, err := ParseDSN(driver, dsn)
	if err != nil {
		return nil, err
	}
	if database == "" {
		database = info.Database
	}
	if database == "" {
		return nil, fmt.Errorf("no database named in the connection string; specify one explicitly")
	}
	if !identifierRe.MatchString(database) {
		return nil, fmt.Errorf("invalid database name %q", database)
	}
	if err := os.MkdirAll(outDir, 0o700); err != nil {
		return nil, err
	}
	stamp := time.Now().UTC().Format("20060102-150405")
	start := time.Now()

	var (
		cmd  *exec.Cmd
		path string
	)
	switch driver {
	case DriverPostgres:
		path = filepath.Join(outDir, fmt.Sprintf("%s-%s.dump", database, stamp))
		cmd = exec.CommandContext(ctx, "pg_dump",
			"--host", info.Host, "--port", info.Port, "--username", info.User,
			"--format", "custom", "--no-password", "--file", path, database)
		cmd.Env = append(os.Environ(), "PGPASSWORD="+info.Password)
	case DriverMySQL:
		path = filepath.Join(outDir, fmt.Sprintf("%s-%s.sql", database, stamp))
		defaults, cleanup, err := mysqlDefaultsFile(info)
		if err != nil {
			return nil, err
		}
		defer cleanup()
		cmd = exec.CommandContext(ctx, "mysqldump",
			"--defaults-extra-file="+defaults,
			"--single-transaction", "--quick", "--routines", "--triggers",
			"--result-file="+path, database)
	case DriverMongo:
		path = filepath.Join(outDir, fmt.Sprintf("%s-%s.archive", database, stamp))
		cmd = exec.CommandContext(ctx, "mongodump",
			"--uri", dsn, "--db", database, "--archive="+path, "--gzip")
	default:
		return nil, fmt.Errorf("%w: %s", ErrUnsupported, driver)
	}

	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	if err := cmd.Run(); err != nil {
		os.Remove(path)
		return nil, fmt.Errorf("dump failed: %s", strings.TrimSpace(buf.String()))
	}
	st, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	return &DumpResult{
		Path: path, Size: st.Size(), Driver: driver, Database: database,
		Duration: time.Since(start).Round(time.Millisecond).String(),
		StartedAt: start.UTC(), Output: strings.TrimSpace(buf.String()),
	}, nil
}

// Restore loads a dump back into a database. This overwrites live data, which
// is why the handler in front of it requires a typed confirmation naming the
// target database.
func Restore(ctx context.Context, driver Driver, dsn, database, dumpPath string) (string, error) {
	info, err := ParseDSN(driver, dsn)
	if err != nil {
		return "", err
	}
	if database == "" {
		database = info.Database
	}
	if !identifierRe.MatchString(database) {
		return "", fmt.Errorf("invalid database name %q", database)
	}
	if _, err := os.Stat(dumpPath); err != nil {
		return "", fmt.Errorf("dump file not readable: %w", err)
	}

	var cmd *exec.Cmd
	switch driver {
	case DriverPostgres:
		cmd = exec.CommandContext(ctx, "pg_restore",
			"--host", info.Host, "--port", info.Port, "--username", info.User,
			"--no-password", "--clean", "--if-exists", "--dbname", database, dumpPath)
		cmd.Env = append(os.Environ(), "PGPASSWORD="+info.Password)
	case DriverMySQL:
		defaults, cleanup, err := mysqlDefaultsFile(info)
		if err != nil {
			return "", err
		}
		defer cleanup()
		f, err := os.Open(dumpPath)
		if err != nil {
			return "", err
		}
		defer f.Close()
		cmd = exec.CommandContext(ctx, "mysql", "--defaults-extra-file="+defaults, database)
		cmd.Stdin = f
	case DriverMongo:
		cmd = exec.CommandContext(ctx, "mongorestore",
			"--uri", dsn, "--archive="+dumpPath, "--gzip", "--drop")
	default:
		return "", fmt.Errorf("%w: %s", ErrUnsupported, driver)
	}

	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	if err := cmd.Run(); err != nil {
		return buf.String(), fmt.Errorf("restore failed: %s", strings.TrimSpace(buf.String()))
	}
	return strings.TrimSpace(buf.String()), nil
}

// mysqlDefaultsFile writes credentials to a mode-0600 temporary file. Passing
// --password on the command line would expose it in the process table, and
// MYSQL_PWD is readable through /proc/<pid>/environ.
func mysqlDefaultsFile(info *ConnInfo) (string, func(), error) {
	f, err := os.CreateTemp("", "vpsd-my-*.cnf")
	if err != nil {
		return "", func() {}, err
	}
	cleanup := func() { os.Remove(f.Name()) }
	if err := f.Chmod(0o600); err != nil {
		f.Close()
		cleanup()
		return "", func() {}, err
	}
	port, _ := strconv.Atoi(info.Port)
	if port == 0 {
		port = 3306
	}
	content := fmt.Sprintf("[client]\nhost=%s\nport=%d\nuser=%s\npassword=\"%s\"\n",
		info.Host, port, info.User, strings.ReplaceAll(info.Password, `"`, `\"`))
	if _, err := f.WriteString(content); err != nil {
		f.Close()
		cleanup()
		return "", func() {}, err
	}
	if err := f.Close(); err != nil {
		cleanup()
		return "", func() {}, err
	}
	return f.Name(), cleanup, nil
}
