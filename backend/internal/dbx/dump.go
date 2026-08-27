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
	// SQL Server is the one engine whose driver takes the database as a query
	// parameter rather than as the path, so reading only the path reported every
	// SQL Server connection as having no database — and the dump then refused to
	// run for want of one the connection string had all along.
	if driver == DriverMSSQL && info.Database == "" {
		q := u.Query()
		info.Database = firstNonEmpty(q.Get("database"), q.Get("Database"))
	}
	if info.Port == "" {
		info.Port = defaultPort(driver)
	}
	return info, nil
}

// defaultPort is what the engine listens on when the connection string does not
// say. Only three engines had an entry here, which left the connection detail
// the databases list shows blank for the other four.
func defaultPort(driver Driver) string {
	switch driver {
	case DriverPostgres:
		return "5432"
	case DriverMySQL:
		return "3306"
	case DriverMongo:
		return "27017"
	case DriverRedis:
		return "6379"
	case DriverMSSQL:
		return "1433"
	case DriverClickHouse:
		return "9000"
	case DriverOracle:
		return "1521"
	}
	return ""
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
	Path string `json:"path"`
	// File is the base name of Path. The browser needs it to ask for the dump
	// back, and splitting a path in TypeScript is a separator assumption this
	// side of the wire already knows the answer to.
	File     string `json:"file"`
	Size     int64  `json:"size"`
	Duration string `json:"duration"`
	Database string `json:"database"`
	Driver   Driver `json:"driver"`
	// Summary is one line describing what is in the file — "4 tables, 1054
	// rows", "306 keys". Separate from Output because Output is whatever the
	// tool said, and mongodump says a timestamped paragraph: a dump of nothing
	// and a dump of everything both end in success, and the only thing that
	// tells them apart on screen is this.
	Summary   string    `json:"summary,omitempty"`
	StartedAt time.Time `json:"startedAt"`
	Output    string    `json:"output,omitempty"`
}

// Dump writes a backup of one database into outDir and returns where it landed.
//
// Every engine this dashboard can connect to can be dumped. Where a native tool
// exists and is installed it is used, because its format is the one the engine's
// own restore path is fastest and most faithful with; where it does not, or
// where it fails, dbx writes the dump itself over the connection it already has
// (dump_sql.go, dump_nosql.go).
//
// The fallback is not a nicety. "unsupported database driver: clickhouse" was
// the dashboard admitting, at the moment the operator pressed the button, that
// the backup they were relying on had never been possible — which is the worst
// time to find out and the reason a backup feature exists at all.
func Dump(ctx context.Context, driver Driver, dsn, database, outDir string) (*DumpResult, error) {
	res, err := runDump(ctx, driver, dsn, database, outDir)
	if res != nil {
		res.File = filepath.Base(res.Path)
	}
	return res, err
}

// runDump is Dump without the bookkeeping, so the several places that build a
// result do not each have to remember to fill in every derived field.
func runDump(ctx context.Context, driver Driver, dsn, database, outDir string) (*DumpResult, error) {
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
	if driver == DriverRedis {
		// Redis names its databases with integers, so it validates its own.
		return dumpRedis(ctx, dsn, database, outDir)
	}
	if err := validateDumpDatabase(database); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(outDir, 0o700); err != nil {
		return nil, err
	}

	fallback := func() (*DumpResult, error) {
		if driver == DriverMongo {
			return dumpMongoDriver(ctx, dsn, database, outDir)
		}
		return dumpGenericSQL(ctx, driver, dsn, database, outDir)
	}
	native, tool := nativeDumpCommand(ctx, driver, dsn, info, database, outDir)
	if native == nil {
		return fallback()
	}
	res, err := native()
	if err == nil {
		return res, nil
	}
	// The native tool is there and refused. Everything that makes it refuse a
	// dump — a version it will not read, a feature the server has and it does
	// not, a missing helper of its own — is something the driver path does not
	// care about, so it is worth the second attempt before reporting a failure.
	fb, ferr := fallback()
	if ferr != nil {
		return nil, fmt.Errorf("%w (and the built-in dump also failed: %v)", err, ferr)
	}
	fb.Summary += " (built-in dump; " + tool + " refused)"
	fb.Output = strings.TrimSpace(fmt.Sprintf("%s\n%s reported: %v", fb.Output, tool, err))
	return fb, nil
}

// nativeDumpCommand returns a closure running the engine's own dump tool, or
// nil when that tool is not installed. Deciding here rather than inside Dump
// keeps "which tool, and is it present" in one place for the three engines that
// have one.
func nativeDumpCommand(ctx context.Context, driver Driver, dsn string, info *ConnInfo, database, outDir string) (func() (*DumpResult, error), string) {
	start := time.Now()
	switch driver {
	case DriverPostgres:
		tool := postgresTool("pg_dump", postgresServerMajor(ctx, dsn))
		if !toolAvailable(tool) {
			return nil, ""
		}
		path := filepath.Join(outDir, dumpFilename(database, "postgres", "dump", start))
		return func() (*DumpResult, error) {
			cmd := exec.CommandContext(ctx, tool,
				"--host", info.Host, "--port", info.Port, "--username", info.User,
				"--format", "custom", "--no-password", "--file", path, database)
			cmd.Env = append(os.Environ(), "PGPASSWORD="+info.Password)
			return runDumpCommand(cmd, tool, path, driver, database, start)
		}, tool
	case DriverMySQL:
		if !toolAvailable("mysqldump") {
			return nil, ""
		}
		path := filepath.Join(outDir, dumpFilename(database, "mysql", "sql", start))
		return func() (*DumpResult, error) {
			defaults, cleanup, err := mysqlDefaultsFile(info)
			if err != nil {
				return nil, err
			}
			defer cleanup()
			cmd := exec.CommandContext(ctx, "mysqldump",
				"--defaults-extra-file="+defaults,
				"--single-transaction", "--quick", "--routines", "--triggers",
				// Tablespace metadata needs the PROCESS privilege, which is
				// server-wide and which no sensible application login has. Asking
				// for it put "mysqldump: Error: Access denied" on the end of a
				// dump that had otherwise worked perfectly.
				"--no-tablespaces",
				"--result-file="+path, database)
			return runDumpCommand(cmd, "mysqldump", path, driver, database, start)
		}, "mysqldump"
	case DriverMongo:
		if !toolAvailable("mongodump") {
			return nil, ""
		}
		path := filepath.Join(outDir, dumpFilename(database, "mongo", "archive", start))
		return func() (*DumpResult, error) {
			conf, cleanup, err := mongoConfigFile(dsn)
			if err != nil {
				return nil, err
			}
			defer cleanup()
			cmd := exec.CommandContext(ctx, "mongodump",
				"--config", conf, "--db", database, "--archive="+path, "--gzip")
			return runDumpCommand(cmd, "mongodump", path, driver, database, start)
		}, "mongodump"
	}
	return nil, ""
}

// runDumpCommand executes one dump tool and turns its exit into a result.
func runDumpCommand(cmd *exec.Cmd, tool, path string, driver Driver, database string, start time.Time) (*DumpResult, error) {
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
		StartedAt: start.UTC(), Summary: "written by " + filepath.Base(tool),
		Output: strings.TrimSpace(buf.String()),
	}, nil
}

// toolAvailable reports whether a dump tool can actually be executed. An
// absolute path is one postgresTool already found on disk; a bare name has to
// be looked up, and not finding it is the ordinary case on a machine that never
// installed it rather than an error worth reporting.
func toolAvailable(name string) bool {
	if name == "" {
		return false
	}
	if strings.ContainsRune(name, os.PathSeparator) {
		st, err := os.Stat(name)
		return err == nil && !st.IsDir()
	}
	_, err := exec.LookPath(name)
	return err == nil
}

// validateDumpDatabase refuses only what cannot be handled safely rather than a
// conservative character class.
//
// The old rule was identifierRe, which requires an ASCII letter first and
// permits nothing but letters, digits, underscore and dollar — so a database
// called "my-app" or "café" could not be dumped at all, despite being a name
// every engine here accepts. Nothing needs that strictness now: the name goes
// into argv (never a shell), into a quoted SQL identifier, or into a filename
// that dumpFilename sanitises separately.
func validateDumpDatabase(database string) error {
	if strings.TrimSpace(database) == "" {
		return fmt.Errorf("no database named in the connection string; specify one explicitly")
	}
	return validateIdent(database)
}

// Restore loads a dump back into a database. This overwrites live data, which
// is why the handler in front of it requires a typed confirmation naming the
// target database.
//
// Which reader to use is decided by the file rather than by the engine: a
// Postgres connection may hold either a pg_dump archive or the SQL text this
// package writes, and picking by driver would refuse one of them for no reason.
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
	if driver == DriverRedis {
		return restoreRedis(ctx, dsn, database, dumpPath)
	}
	if err := validateDumpDatabase(database); err != nil {
		return "", err
	}
	switch dumpFormatOf(dumpPath) {
	case dumpFormatArchive:
		if driver != DriverMongo {
			return "", fmt.Errorf("this is a Mongo archive; the connection is %s", driver)
		}
		return restoreMongoDriver(ctx, dsn, database, dumpPath)
	case dumpFormatSQLText:
		return restoreGenericSQL(ctx, driver, dsn, database, dumpPath)
	}

	var cmd *exec.Cmd
	switch driver {
	case DriverPostgres:
		tool := postgresTool("pg_restore", postgresServerMajor(ctx, dsn))
		if !toolAvailable(tool) {
			return "", fmt.Errorf("this is a pg_dump custom-format archive and pg_restore is not installed; " +
				"re-take the backup to get one this dashboard can restore on its own")
		}
		cmd = exec.CommandContext(ctx, tool,
			"--host", info.Host, "--port", info.Port, "--username", info.User,
			"--no-password", "--clean", "--if-exists", "--dbname", database, dumpPath)
		cmd.Env = append(os.Environ(), "PGPASSWORD="+info.Password)
	case DriverMySQL:
		if !toolAvailable("mysql") {
			return restoreGenericSQL(ctx, driver, dsn, database, dumpPath)
		}
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
		if !toolAvailable("mongorestore") {
			return "", fmt.Errorf("this is a mongodump archive and mongorestore is not installed; " +
				"re-take the backup to get one this dashboard can restore on its own")
		}
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

// dumpFormat is what a dump file turns out to be.
type dumpFormat int

const (
	// dumpFormatNative is a tool's own format — a pg_dump custom archive, a
	// mongodump archive, a mysqldump script — and is replayed by that tool.
	dumpFormatNative dumpFormat = iota
	// dumpFormatSQLText is the SQL this package writes.
	dumpFormatSQLText
	// dumpFormatArchive is the gzipped JSON Lines this package writes for the
	// engines that are not SQL.
	dumpFormatArchive
)

// dumpFormatOf reads the first bytes of the file rather than trusting its name.
// A dump gets renamed, and restoring a file with the wrong reader produces an
// error about syntax that says nothing about the actual mistake.
func dumpFormatOf(path string) dumpFormat {
	f, err := os.Open(path)
	if err != nil {
		return dumpFormatNative
	}
	defer f.Close()
	head := make([]byte, 512)
	n, _ := io.ReadFull(f, head)
	head = head[:n]
	switch {
	case bytes.HasPrefix(head, []byte("PGDMP")):
		// pg_dump's custom format, which only pg_restore reads.
		return dumpFormatNative
	case bytes.HasPrefix(head, []byte("-- Just Dashboard dump")):
		return dumpFormatSQLText
	case bytes.HasPrefix(head, []byte{0x1f, 0x8b}):
		// gzip: either this package's JSON Lines archive or a gzipped
		// mongodump. Only the first has a header naming itself, and reading it
		// means decompressing, so the cheap discriminator is the extension the
		// writer chose alongside it.
		if strings.HasSuffix(path, ".jsonl.gz") {
			return dumpFormatArchive
		}
		return dumpFormatNative
	default:
		return dumpFormatNative
	}
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
	var tables int
	_ = db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%'`).
		Scan(&tables)
	return &DumpResult{
		Path: out, Size: st.Size(), Driver: DriverSQLite, Database: filepath.Base(path),
		Duration: time.Since(start).Round(time.Millisecond).String(), StartedAt: start.UTC(),
		Summary: fmt.Sprintf("%d tables", tables),
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
