package dbx

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"io"
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
	if driver == DriverSQLite {
		// A SQLite DSN is a file path (optionally with ?_pragma=… query), not a
		// URL. The "database" is the file, which is what the UI shows and what
		// the dump copies.
		path := dsn
		if strings.HasPrefix(path, "file:") {
			path = strings.TrimPrefix(path, "file:")
		}
		if q := strings.IndexByte(path, '?'); q >= 0 {
			path = path[:q]
		}
		return &ConnInfo{Host: "localhost", Database: path}, nil
	}
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
	Path      string    `json:"path"`
	Size      int64     `json:"size"`
	Duration  string    `json:"duration"`
	Database  string    `json:"database"`
	Driver    Driver    `json:"driver"`
	StartedAt time.Time `json:"startedAt"`
	Output    string    `json:"output,omitempty"`
}

// Dump writes a backup of one database into outDir and returns where it landed.
func Dump(ctx context.Context, driver Driver, dsn, database, outDir string) (*DumpResult, error) {
	info, err := ParseDSN(driver, dsn)
	if err != nil {
		return nil, err
	}
	if driver == DriverSQLite {
		return dumpSQLite(ctx, dsn, info.Database, outDir)
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
		cmd = exec.CommandContext(ctx, postgresTool("pg_dump", postgresServerMajor(ctx, dsn)),
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
		conf, cleanup, err := mongoConfigFile(dsn)
		if err != nil {
			return nil, err
		}
		defer cleanup()
		cmd = exec.CommandContext(ctx, "mongodump",
			"--config", conf, "--db", database, "--archive="+path, "--gzip")
	default:
		return nil, fmt.Errorf("%w: %s", ErrUnsupported, driver)
	}

	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	if err := cmd.Run(); err != nil {
		os.Remove(path)
		// The tool's own output when it produced any, and the exec error when
		// it did not. A missing binary writes nothing to stderr at all, so the
		// message was "dump failed:" followed by nothing — which says something
		// went wrong and withholds the only useful part, the name of the tool
		// that is not installed.
		if detail := strings.TrimSpace(buf.String()); detail != "" {
			return nil, fmt.Errorf("dump failed: %s", detail)
		}
		return nil, fmt.Errorf("dump failed: %w", err)
	}
	st, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	return &DumpResult{
		Path: path, Size: st.Size(), Driver: driver, Database: database,
		Duration:  time.Since(start).Round(time.Millisecond).String(),
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
	if _, err := os.Stat(dumpPath); err != nil {
		return "", fmt.Errorf("dump file not readable: %w", err)
	}
	if driver == DriverSQLite {
		return restoreSQLite(dumpPath, info.Database)
	}
	if database == "" {
		database = info.Database
	}
	if !identifierRe.MatchString(database) {
		return "", fmt.Errorf("invalid database name %q", database)
	}

	var cmd *exec.Cmd
	switch driver {
	case DriverPostgres:
		cmd = exec.CommandContext(ctx, postgresTool("pg_restore", postgresServerMajor(ctx, dsn)),
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
		conf, cleanup, err := mongoConfigFile(dsn)
		if err != nil {
			return "", err
		}
		defer cleanup()
		cmd = exec.CommandContext(ctx, "mongorestore",
			"--config", conf, "--archive="+dumpPath, "--gzip", "--drop")
	default:
		return "", fmt.Errorf("%w: %s", ErrUnsupported, driver)
	}

	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	if err := cmd.Run(); err != nil {
		if detail := strings.TrimSpace(buf.String()); detail != "" {
			return buf.String(), fmt.Errorf("restore failed: %s", detail)
		}
		return buf.String(), fmt.Errorf("restore failed: %w", err)
	}
	return strings.TrimSpace(buf.String()), nil
}

// dumpSQLite writes a consistent snapshot of a SQLite file with VACUUM INTO,
// which the engine performs against a live database without blocking it — a
// plain file copy could capture a torn write mid-transaction. The result is an
// ordinary SQLite file, so a "restore" is just copying it back.
func dumpSQLite(ctx context.Context, dsn, path, outDir string) (*DumpResult, error) {
	if path == "" {
		return nil, fmt.Errorf("connection has no database file path")
	}
	if err := os.MkdirAll(outDir, 0o700); err != nil {
		return nil, err
	}
	start := time.Now()
	stamp := time.Now().UTC().Format("20060102-150405")
	base := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	if base == "" {
		base = "sqlite"
	}
	out := filepath.Join(outDir, fmt.Sprintf("%s-%s.sqlite", base, stamp))

	db, err := sql.Open("sqlite", sqliteDialect{}.NormaliseDSN(dsn))
	if err != nil {
		return nil, err
	}
	defer db.Close()
	// VACUUM INTO takes a string literal, not a bound parameter, and there is no
	// identifier form to quote — a single-quoted SQLite string literal escapes
	// one character (the quote, by doubling), so that is the whole escaping.
	lit := "'" + strings.ReplaceAll(out, "'", "''") + "'"
	if _, err := db.ExecContext(ctx, "VACUUM INTO "+lit); err != nil {
		os.Remove(out)
		return nil, fmt.Errorf("dump failed: %w", err)
	}
	st, err := os.Stat(out)
	if err != nil {
		return nil, err
	}
	return &DumpResult{
		Path: out, Size: st.Size(), Driver: DriverSQLite, Database: filepath.Base(path),
		Duration: time.Since(start).Round(time.Millisecond).String(), StartedAt: start.UTC(),
	}, nil
}

// restoreSQLite replaces the live database file with a dump. The previous file
// is kept alongside as <name>.bak-<stamp> rather than deleted, so a restore
// from the wrong dump is recoverable — the same guard WriteComposeFile applies.
func restoreSQLite(dumpPath, target string) (string, error) {
	if target == "" {
		return "", fmt.Errorf("connection has no database file path")
	}
	src, err := os.Open(dumpPath)
	if err != nil {
		return "", err
	}
	defer src.Close()
	if _, err := os.Stat(target); err == nil {
		backup := fmt.Sprintf("%s.bak-%s", target, time.Now().UTC().Format("20060102-150405"))
		if data, err := os.ReadFile(target); err == nil {
			_ = os.WriteFile(backup, data, 0o600)
		}
	}
	dst, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return "", err
	}
	defer dst.Close()
	if _, err := io.Copy(dst, src); err != nil {
		return "", fmt.Errorf("restore failed: %w", err)
	}
	return "restored " + target, nil
}

// mongoConfigFile writes the connection string to a mode-0600 temporary file
// for --config.
//
// --uri put "mongodb://user:password@host/db" straight into argv, which any
// local user reads out of `ps auxww` or /proc/<pid>/cmdline — and this
// container shares the host's PID namespace, so "local" means anyone on the
// box. Postgres and MySQL already avoided argv; Mongo was the odd one out.
func mongoConfigFile(dsn string) (string, func(), error) {
	f, err := os.CreateTemp("", "vpsd-mongo-*.yaml")
	if err != nil {
		return "", func() {}, err
	}
	cleanup := func() { os.Remove(f.Name()) }
	if err := f.Chmod(0o600); err != nil {
		f.Close()
		cleanup()
		return "", func() {}, err
	}
	// A YAML single-quoted scalar escapes exactly one character — the quote
	// itself, by doubling it — so there is no ambiguity to get wrong here.
	content := "uri: '" + strings.ReplaceAll(dsn, "'", "''") + "'\n"
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
	if strings.ContainsAny(info.Password, "\n\r") {
		f.Close()
		cleanup()
		return "", func() {}, fmt.Errorf("password contains a line break, which a MySQL option file cannot carry")
	}
	// Order matters: the backslash has to be doubled before the quote is
	// escaped, or the escape this adds is itself escaped away. Leaving the
	// backslash alone entirely — as this did — sent MySQL "pa\tss" as a
	// password containing a tab, and the dump failed on authentication with
	// nothing to suggest why.
	password := strings.ReplaceAll(info.Password, `\`, `\\`)
	password = strings.ReplaceAll(password, `"`, `\"`)
	content := fmt.Sprintf("[client]\nhost=%s\nport=%d\nuser=%s\npassword=\"%s\"\n",
		info.Host, port, info.User, password)
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

// Picking the right pg_dump for the server being dumped.
//
// PostgreSQL refuses point blank: "aborting because of server version
// mismatch". A pg_dump older than the server it is pointed at will not run, so
// the client that happened to be in the image decided which servers could be
// backed up at all — a Debian bookworm image ships 15, and a dashboard next to
// a Postgres 16 could not dump it.
//
// Distributions install each major version under its own directory, so the fix
// is to look for the one that matches. A newer client can read an older server,
// so the fallback is the highest installed rather than whatever is on PATH.
// Nothing is guessed about what exists: the directory is read.

var pgBinDirs = []string{"/usr/lib/postgresql", "/usr/pgsql", "/opt/homebrew/opt"}

// postgresTool returns the path to a Postgres client binary suitable for a
// server of the given major version, and the plain name if nothing better is
// found — which keeps a machine that installs the tools somewhere unusual
// working exactly as it did before.
func postgresTool(name string, serverMajor int) string {
	installed := installedPGVersions(name)
	if len(installed) == 0 {
		return name
	}
	// Exactly the server's version is always safe.
	if serverMajor > 0 {
		if path, ok := installed[serverMajor]; ok {
			return path
		}
	}
	// Otherwise the newest, which can read anything older than itself.
	best := 0
	for major := range installed {
		if major > best {
			best = major
		}
	}
	if serverMajor > 0 && best < serverMajor {
		// Everything installed is older than the server. Returning the newest
		// still produces the clearest possible failure — Postgres names both
		// versions — and there is nothing better to try.
		return installed[best]
	}
	return installed[best]
}

// installedPGVersions maps a major version to the path of `name` for it.
func installedPGVersions(name string) map[int]string {
	out := map[int]string{}
	for _, root := range pgBinDirs {
		entries, err := os.ReadDir(root)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			major, err := strconv.Atoi(strings.TrimPrefix(e.Name(), "postgresql@"))
			if err != nil {
				continue
			}
			path := filepath.Join(root, e.Name(), "bin", name)
			if _, err := os.Stat(path); err == nil {
				out[major] = path
			}
		}
	}
	return out
}

// postgresServerMajor asks the server what it is. A failure here is not fatal:
// the caller falls back to the newest client installed, which is the right
// answer far more often than not.
func postgresServerMajor(ctx context.Context, dsn string) int {
	d, err := DialectFor(DriverPostgres)
	if err != nil {
		return 0
	}
	db, err := sql.Open(d.SQLDriverName(), d.NormaliseDSN(dsn))
	if err != nil {
		return 0
	}
	defer db.Close()
	var num int
	// server_version_num is 160015 for 16.15, which is the major without any
	// of the parsing that the display string would need.
	if err := db.QueryRowContext(ctx, "SHOW server_version_num").Scan(&num); err != nil {
		return 0
	}
	return num / 10000
}
