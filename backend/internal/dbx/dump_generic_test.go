package dbx

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestEveryDriverHasADumpPath is the regression for the report that started
// this: pressing "Dump now" on a ClickHouse connection answered "unsupported
// database driver: clickhouse", and on a Mongo one "mongodump: executable file
// not found in $PATH". Both are the same failure — a backup button that was
// never going to work on that engine — so the assertion is about all of them.
//
// The dumps here point at nothing, so they fail to connect; what must not
// happen is a refusal that never got as far as trying.
func TestEveryDriverHasADumpPath(t *testing.T) {
	dir := t.TempDir()
	dsns := map[Driver]string{
		DriverPostgres:   "postgres://u:p@127.0.0.1:65001/x?sslmode=disable&connect_timeout=1",
		DriverMySQL:      "u:p@tcp(127.0.0.1:65002)/x?timeout=1s",
		DriverMSSQL:      "sqlserver://u:p@127.0.0.1:65003?database=x&dial+timeout=1",
		DriverClickHouse: "clickhouse://u:p@127.0.0.1:65004/x?dial_timeout=1s",
		DriverOracle:     "oracle://u:p@127.0.0.1:65005/x",
		DriverMongo:      "mongodb://127.0.0.1:65006/x?connectTimeoutMS=500&serverSelectionTimeoutMS=500",
		DriverRedis:      "redis://127.0.0.1:65007/0",
	}
	for driver, dsn := range dsns {
		t.Run(string(driver), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			_, err := Dump(ctx, driver, dsn, "x", dir)
			if err == nil {
				t.Fatal("expected a connection failure against a closed port")
			}
			if strings.Contains(err.Error(), ErrUnsupported.Error()) {
				t.Errorf("%s has no dump path at all: %v", driver, err)
			}
			if strings.Contains(err.Error(), "executable file not found") {
				t.Errorf("%s fell back to nothing when its tool was absent: %v", driver, err)
			}
		})
	}
}

func TestSplitSQLStatements(t *testing.T) {
	cases := []struct {
		name   string
		driver Driver
		in     string
		want   []string
	}{
		{
			name: "semicolon inside a string is not a terminator", driver: DriverPostgres,
			in:   "INSERT INTO t VALUES ('a;b'); SELECT 1;",
			want: []string{"INSERT INTO t VALUES ('a;b')", "SELECT 1"},
		},
		{
			name: "doubled quote does not end the string", driver: DriverPostgres,
			in:   "INSERT INTO t VALUES ('O''Brien; x'); SELECT 2;",
			want: []string{"INSERT INTO t VALUES ('O''Brien; x')", "SELECT 2"},
		},
		{
			name: "backslash escapes a quote on MySQL", driver: DriverMySQL,
			in:   `INSERT INTO t VALUES ('a\'; DROP TABLE u; --'); SELECT 3;`,
			want: []string{`INSERT INTO t VALUES ('a\'; DROP TABLE u; --')`, "SELECT 3"},
		},
		{
			// The same bytes on Postgres, where a backslash is an ordinary
			// character: the quote after it closes the string, so this really
			// is three statements. A splitter with one rule gets one of these
			// two wrong.
			name: "backslash does not escape on Postgres", driver: DriverPostgres,
			in:   `INSERT INTO t VALUES ('a\'); SELECT 3;`,
			want: []string{`INSERT INTO t VALUES ('a\')`, "SELECT 3"},
		},
		{
			name: "semicolon inside a quoted identifier", driver: DriverPostgres,
			in:   `DROP TABLE "odd;name"; SELECT 4;`,
			want: []string{`DROP TABLE "odd;name"`, "SELECT 4"},
		},
		{
			name: "brackets quote identifiers on SQL Server", driver: DriverMSSQL,
			in:   `DROP TABLE [odd;name]; SELECT 5;`,
			want: []string{`DROP TABLE [odd;name]`, "SELECT 5"},
		},
		{
			name: "line comments are dropped", driver: DriverPostgres,
			in:   "-- a comment; with a semicolon\nSELECT 6;",
			want: []string{"SELECT 6"},
		},
		{
			name: "block comments are dropped", driver: DriverPostgres,
			in:   "/* one; two */ SELECT 7;",
			want: []string{"SELECT 7"},
		},
		{
			name: "a trailing statement without a semicolon still counts", driver: DriverPostgres,
			in:   "SELECT 8",
			want: []string{"SELECT 8"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := splitSQLStatements(tc.driver, tc.in)
			if len(got) != len(tc.want) {
				t.Fatalf("got %d statements %q, want %d %q", len(got), got, len(tc.want), tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("statement %d:\n got %q\nwant %q", i, got[i], tc.want[i])
				}
			}
		})
	}
}

func TestDumpLiteralEscaping(t *testing.T) {
	// A backslash is an escape character inside a string literal on MySQL and
	// ClickHouse and a plain character everywhere else, so the same value has
	// two correct renderings and picking one for all six loses data on half of
	// them.
	const value = `back\slash and 'quote'`
	cases := map[Driver]string{
		DriverPostgres:   `'back\slash and ''quote'''`,
		DriverSQLite:     `'back\slash and ''quote'''`,
		DriverOracle:     `'back\slash and ''quote'''`,
		DriverMSSQL:      `N'back\slash and ''quote'''`,
		DriverMySQL:      `'back\\slash and ''quote'''`,
		DriverClickHouse: `'back\\slash and ''quote'''`,
	}
	for driver, want := range cases {
		if got := dumpLiteral(driver, value); got != want {
			t.Errorf("%s:\n got %s\nwant %s", driver, got, want)
		}
	}
}

func TestDumpLiteralBinaryAndNull(t *testing.T) {
	// Bytes that are not text go out as the engine's binary literal rather than
	// being forced through a string, where the invalid sequence would be
	// replaced and the value would come back different.
	raw := []byte{0x00, 0xff, 0x41}
	cases := map[Driver]string{
		DriverPostgres:   `'\x00ff41'::bytea`,
		DriverSQLite:     `X'00FF41'`,
		DriverMySQL:      `0x00FF41`,
		DriverMSSQL:      `0x00FF41`,
		DriverClickHouse: `unhex('00FF41')`,
		DriverOracle:     `HEXTORAW('00FF41')`,
	}
	for driver, want := range cases {
		if got := dumpLiteral(driver, raw); got != want {
			t.Errorf("%s bytes:\n got %s\nwant %s", driver, got, want)
		}
		if got := dumpLiteral(driver, nil); got != "NULL" {
			t.Errorf("%s nil: got %s", driver, got)
		}
	}

	// A string carrying a NUL cannot be quoted on any of them, so it takes the
	// binary path even though the column is text.
	if got := dumpLiteral(DriverPostgres, "a\x00b"); !strings.HasPrefix(got, `'\x`) {
		t.Errorf("a string with a NUL should be rendered as bytes, got %s", got)
	}

	// And a text column whose driver hands back []byte — which MySQL does for
	// every VARCHAR — must not be hexed.
	if got := dumpValue(DriverMySQL, []byte("hello"), false); got != `'hello'` {
		t.Errorf("text column: got %s", got)
	}
	if got := dumpValue(DriverMySQL, []byte("hello"), true); got != `0x68656C6C6F` {
		t.Errorf("binary column: got %s", got)
	}
}

func TestOrderByDependencyPutsParentsFirst(t *testing.T) {
	// Alphabetical order puts posts before users, which is the order the
	// catalogue returns and the order that fails: the CREATE cannot reference a
	// table that does not exist yet, and neither can the INSERT.
	tables := []dumpTable{
		{table: Table{Name: "posts"}, rel: "posts", detail: &TableDetail{
			ForeignKeys: []ForeignKey{{RefTable: "users"}},
		}},
		{table: Table{Name: "users"}, rel: "users", detail: &TableDetail{}},
	}
	got := orderByDependency(tables)
	if len(got) != 2 || got[0].table.Name != "users" || got[1].table.Name != "posts" {
		t.Fatalf("got %s then %s", got[0].table.Name, got[1].table.Name)
	}
}

func TestOrderByDependencyKeepsCycles(t *testing.T) {
	// Two tables referencing each other cannot be ordered. Dropping either from
	// the dump would lose the data, so both stay.
	tables := []dumpTable{
		{table: Table{Name: "a"}, detail: &TableDetail{ForeignKeys: []ForeignKey{{RefTable: "b"}}}},
		{table: Table{Name: "b"}, detail: &TableDetail{ForeignKeys: []ForeignKey{{RefTable: "a"}}}},
	}
	if got := orderByDependency(tables); len(got) != 2 {
		t.Fatalf("a cycle lost a table: %d of 2 kept", len(got))
	}
}

func TestDumpFilenameSanitises(t *testing.T) {
	at := time.Date(2026, 8, 25, 11, 30, 0, 0, time.UTC)
	// A database name is a label in a filename, not an identifier: engines allow
	// hyphens, dots and worse, and a name is not a reason to refuse the backup.
	if got := dumpFilename("my-app.prod/2", "postgres", "sql", at); got != "my-app_prod_2-20260825-113000.sql" {
		t.Errorf("got %q", got)
	}
	if got := dumpFilename("", "clickhouse", "sql", at); got != "clickhouse-20260825-113000.sql" {
		t.Errorf("unnamed database: got %q", got)
	}
	if strings.ContainsAny(filepath.Base(dumpFilename("../../etc/passwd", "x", "sql", at)), "/.\\") &&
		!strings.HasSuffix(dumpFilename("../../etc/passwd", "x", "sql", at), ".sql") {
		t.Error("a traversal attempt reached the filename")
	}
}

func TestValidateDumpDatabaseAcceptsRealNames(t *testing.T) {
	// The old rule was identifierRe, which refused a hyphen — so a database
	// called "my-app" could not be backed up at all.
	for _, name := range []string{"my-app", "café", "jdtest", "MASTER", "db.one"} {
		if err := validateDumpDatabase(name); err != nil {
			t.Errorf("validateDumpDatabase(%q) = %v", name, err)
		}
	}
	for _, name := range []string{"", "   ", "bad\x00name", "line\nbreak"} {
		if err := validateDumpDatabase(name); err == nil {
			t.Errorf("validateDumpDatabase(%q) accepted it", name)
		}
	}
}

func TestParseDSNFillsInWhatTheDriverKnows(t *testing.T) {
	// SQL Server keeps the database in a query parameter, so reading only the
	// path reported every SQL Server connection as having no database — and the
	// dump then refused for want of one the connection string carried.
	info, err := ParseDSN(DriverMSSQL, "sqlserver://sa:pw@127.0.0.1?database=payments")
	if err != nil {
		t.Fatalf("ParseDSN: %v", err)
	}
	if info.Database != "payments" {
		t.Errorf("database = %q, want payments", info.Database)
	}
	if info.Port != "1433" {
		t.Errorf("port = %q, want the default 1433", info.Port)
	}
	for driver, want := range map[Driver]string{
		DriverRedis: "6379", DriverClickHouse: "9000", DriverOracle: "1521",
	} {
		info, err := ParseDSN(driver, string(driver)+"://127.0.0.1/x")
		if err != nil {
			t.Fatalf("ParseDSN(%s): %v", driver, err)
		}
		if info.Port != want {
			t.Errorf("%s port = %q, want %q", driver, info.Port, want)
		}
	}
}

func TestDsnForDatabaseRepoints(t *testing.T) {
	if got := dsnForDatabase(DriverPostgres, "postgres://u:p@h:5432/one?sslmode=disable", "two"); got !=
		"postgres://u:p@h:5432/two?sslmode=disable" {
		t.Errorf("postgres: %s", got)
	}
	if got := dsnForDatabase(DriverMySQL, "u:p@tcp(h:3306)/one?parseTime=true", "two"); got !=
		"u:p@tcp(h:3306)/two?parseTime=true" {
		t.Errorf("mysql: %s", got)
	}
	if got := dsnForDatabase(DriverMSSQL, "sqlserver://u:p@h?database=one", "two"); !strings.Contains(got, "database=two") {
		t.Errorf("mssql: %s", got)
	}
	// SQLite's database is the file the DSN already names; rewriting it would
	// point the dump at a file that does not exist.
	if got := dsnForDatabase(DriverSQLite, "/var/lib/app.db", "two"); got != "/var/lib/app.db" {
		t.Errorf("sqlite: %s", got)
	}
}
