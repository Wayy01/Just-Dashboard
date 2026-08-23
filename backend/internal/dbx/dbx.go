// Package dbx provides database administration across Postgres, MySQL, SQLite
// and MongoDB: browsing schemas and tables, introspecting a table's full
// structure, editing rows through safe generated statements, running arbitrary
// queries, exporting data, generating ORM schemas, and dumping or restoring.
//
// Connection strings are secrets — they carry credentials — so they are held
// encrypted at rest and never returned to a client. Two rules keep the write
// paths safe: identifiers (schema, table and column names) are always validated
// and quoted, never bound, while values are always bound, never interpolated;
// and a query runner is inherently powerful, so destructive statements are
// classified before they run and the handler demands a typed confirmation for
// them. Row edits go one step further and refuse to run without a primary key,
// so an UPDATE or DELETE cannot silently touch more than the one row intended.
package dbx

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	_ "github.com/go-sql-driver/mysql"
	_ "github.com/jackc/pgx/v5/stdlib"
	_ "modernc.org/sqlite"
)

type Driver string

const (
	DriverPostgres Driver = "postgres"
	DriverMySQL    Driver = "mysql"
	DriverMongo    Driver = "mongodb"
	// DriverSQLite treats a single database file as the connection. It shares
	// the SQL query path with Postgres and MySQL, and uses the same pure-Go
	// driver (modernc.org/sqlite) the dashboard already stores its own state in,
	// so it adds no new build dependency and needs no CGO.
	DriverSQLite Driver = "sqlite"
)

var ErrUnsupported = errors.New("unsupported database driver")

func (d Driver) Valid() bool {
	switch d {
	case DriverPostgres, DriverMySQL, DriverMongo, DriverSQLite:
		return true
	}
	return false
}

// IsSQL reports whether the driver goes through database/sql. Mongo is the one
// engine that does not, so callers branch on this rather than listing drivers.
func (d Driver) IsSQL() bool {
	return d == DriverPostgres || d == DriverMySQL || d == DriverSQLite
}

// sqlDriverName maps our driver identity onto the registered database/sql
// driver. pgx registers itself as "pgx" through its stdlib shim.
func (d Driver) sqlDriverName() (string, error) {
	switch d {
	case DriverPostgres:
		return "pgx", nil
	case DriverMySQL:
		return "mysql", nil
	case DriverSQLite:
		return "sqlite", nil
	default:
		return "", fmt.Errorf("%w: %s", ErrUnsupported, d)
	}
}

// placeholder renders the n-th bind marker for the driver. Postgres numbers its
// markers ($1, $2); MySQL and SQLite use a positional '?'. Keeping this in one
// place is what lets the query builders below stay driver-agnostic.
func (d Driver) placeholder(n int) string {
	if d == DriverPostgres {
		return fmt.Sprintf("$%d", n)
	}
	return "?"
}

// Manager owns the connection pools. Pools are opened lazily and reused: a
// query runner that dialled a fresh connection per request would exhaust the
// server's connection limit under any real use.
type Manager struct {
	mu    sync.Mutex
	pools map[int64]*sql.DB
}

func NewManager() *Manager {
	return &Manager{pools: map[int64]*sql.DB{}}
}

func (m *Manager) Pool(ctx context.Context, id int64, driver Driver, dsn string) (*sql.DB, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if db, ok := m.pools[id]; ok {
		return db, nil
	}
	name, err := driver.sqlDriverName()
	if err != nil {
		return nil, err
	}
	if driver == DriverSQLite {
		dsn = sqliteDSN(dsn)
	}
	db, err := sql.Open(name, dsn)
	if err != nil {
		return nil, err
	}
	// A dashboard is not the application: a handful of connections is plenty,
	// and a large pool here would compete with the real workload.
	db.SetMaxOpenConns(5)
	db.SetMaxIdleConns(2)
	db.SetConnMaxLifetime(30 * time.Minute)
	// SQLite serialises writers at the file level, so a pool of five racing
	// connections turns every concurrent write into a "database is locked"
	// error. One connection plus the busy timeout below is the documented way
	// to make a single-file database behave under a web front end.
	if driver == DriverSQLite {
		db.SetMaxOpenConns(1)
	}

	pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := db.PingContext(pingCtx); err != nil {
		db.Close()
		return nil, err
	}
	m.pools[id] = db
	return db, nil
}

// Probe opens a throwaway connection to verify a DSN before it is saved, and
// returns the server version so the operator gets confirmation they reached the
// engine they meant to. It never touches the manager's pool cache: a connection
// being tested may be wrong, and a failed test must not leave a broken pool
// cached under some id.
func Probe(ctx context.Context, driver Driver, dsn string) (string, error) {
	if !driver.IsSQL() {
		return "", fmt.Errorf("%w: %s", ErrUnsupported, driver)
	}
	name, err := driver.sqlDriverName()
	if err != nil {
		return "", err
	}
	if driver == DriverSQLite {
		dsn = sqliteDSN(dsn)
	}
	db, err := sql.Open(name, dsn)
	if err != nil {
		return "", err
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := db.PingContext(pingCtx); err != nil {
		return "", err
	}
	var version string
	switch driver {
	case DriverSQLite:
		_ = db.QueryRowContext(pingCtx, "SELECT sqlite_version()").Scan(&version)
		if version != "" {
			version = "SQLite " + version
		}
	default:
		_ = db.QueryRowContext(pingCtx, "SELECT version()").Scan(&version)
	}
	return version, nil
}

func (m *Manager) Close(id int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if db, ok := m.pools[id]; ok {
		db.Close()
		delete(m.pools, id)
	}
}

func (m *Manager) Shutdown() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, db := range m.pools {
		db.Close()
		delete(m.pools, id)
	}
}

type PoolStats struct {
	Open            int    `json:"open"`
	InUse           int    `json:"inUse"`
	Idle            int    `json:"idle"`
	WaitCount       int64  `json:"waitCount"`
	WaitDuration    string `json:"waitDuration"`
	MaxOpen         int    `json:"maxOpen"`
	MaxIdleClosed   int64  `json:"maxIdleClosed"`
	MaxLifetimeGone int64  `json:"maxLifetimeClosed"`
}

func (m *Manager) Stats(id int64) *PoolStats {
	m.mu.Lock()
	db, ok := m.pools[id]
	m.mu.Unlock()
	if !ok {
		return nil
	}
	s := db.Stats()
	return &PoolStats{
		Open: s.OpenConnections, InUse: s.InUse, Idle: s.Idle,
		WaitCount: s.WaitCount, WaitDuration: s.WaitDuration.String(),
		MaxOpen: s.MaxOpenConnections, MaxIdleClosed: s.MaxIdleClosed,
		MaxLifetimeGone: s.MaxLifetimeClosed,
	}
}

type Database struct {
	Name     string `json:"name"`
	Size     int64  `json:"size,omitempty"`
	Owner    string `json:"owner,omitempty"`
	Encoding string `json:"encoding,omitempty"`
}

// sqliteDSN makes a caller-supplied file path safe to open read-write under a
// web front end: it turns a bare path into a file: URI and appends the busy
// timeout and foreign-key pragmas the dashboard's own store uses, but leaves an
// already-parameterised DSN untouched so an operator can still pass mode=ro.
func sqliteDSN(dsn string) string {
	if strings.Contains(dsn, "?") || strings.HasPrefix(dsn, "file:") {
		return dsn
	}
	return dsn + "?_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)"
}

func ListDatabases(ctx context.Context, db *sql.DB, driver Driver) ([]Database, error) {
	var query string
	switch driver {
	case DriverSQLite:
		// A SQLite connection is a single file, but ATTACHed databases show up
		// as extra rows; "main" is always present. The file path is reported so
		// the operator can see which file they are browsing.
		rows, err := db.QueryContext(ctx, `PRAGMA database_list`)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		out := []Database{}
		for rows.Next() {
			var seq int
			var name, file string
			if err := rows.Scan(&seq, &name, &file); err != nil {
				return nil, err
			}
			d := Database{Name: name, Owner: file}
			out = append(out, d)
		}
		return out, rows.Err()
	case DriverPostgres:
		query = `SELECT d.datname,
		                pg_database_size(d.datname),
		                COALESCE(pg_get_userbyid(d.datdba), ''),
		                pg_encoding_to_char(d.encoding)
		         FROM pg_database d
		         WHERE NOT d.datistemplate
		         ORDER BY 1`
	case DriverMySQL:
		query = `SELECT s.SCHEMA_NAME,
		                COALESCE(SUM(t.DATA_LENGTH + t.INDEX_LENGTH), 0),
		                '',
		                s.DEFAULT_CHARACTER_SET_NAME
		         FROM information_schema.SCHEMATA s
		         LEFT JOIN information_schema.TABLES t ON t.TABLE_SCHEMA = s.SCHEMA_NAME
		         GROUP BY s.SCHEMA_NAME, s.DEFAULT_CHARACTER_SET_NAME
		         ORDER BY 1`
	default:
		return nil, fmt.Errorf("%w: %s", ErrUnsupported, driver)
	}
	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Database{}
	for rows.Next() {
		var d Database
		if err := rows.Scan(&d.Name, &d.Size, &d.Owner, &d.Encoding); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

type Table struct {
	Schema  string `json:"schema"`
	Name    string `json:"name"`
	Type    string `json:"type"`
	Rows    int64  `json:"estimatedRows"`
	Size    int64  `json:"size,omitempty"`
	Comment string `json:"comment,omitempty"`
}

func ListTables(ctx context.Context, db *sql.DB, driver Driver, schema string) ([]Table, error) {
	var (
		query string
		args  []any
	)
	switch driver {
	case DriverSQLite:
		// SQLite has one schema per file. sqlite_master carries tables and
		// views; the internal sqlite_* tables are hidden. Row and size figures
		// are not free here, so they are left at zero rather than run a COUNT
		// per table on every listing.
		query = `SELECT 'main', name,
		                CASE type WHEN 'table' THEN 'table' ELSE type END,
		                0, 0, ''
		         FROM sqlite_master
		         WHERE type IN ('table','view')
		           AND name NOT LIKE 'sqlite_%'
		         ORDER BY name`
	case DriverPostgres:
		query = `SELECT n.nspname, c.relname,
		                CASE c.relkind WHEN 'r' THEN 'table' WHEN 'v' THEN 'view'
		                               WHEN 'm' THEN 'materialized view' ELSE c.relkind::text END,
		                COALESCE(c.reltuples::bigint, 0),
		                pg_total_relation_size(c.oid),
		                COALESCE(obj_description(c.oid), '')
		         FROM pg_class c
		         JOIN pg_namespace n ON n.oid = c.relnamespace
		         WHERE c.relkind IN ('r','v','m','p')
		           AND n.nspname NOT IN ('pg_catalog','information_schema')
		           AND ($1 = '' OR n.nspname = $1)
		         ORDER BY 1, 2`
		args = []any{schema}
	case DriverMySQL:
		query = `SELECT TABLE_SCHEMA, TABLE_NAME,
		                CASE TABLE_TYPE WHEN 'BASE TABLE' THEN 'table' ELSE LOWER(TABLE_TYPE) END,
		                COALESCE(TABLE_ROWS, 0),
		                COALESCE(DATA_LENGTH + INDEX_LENGTH, 0),
		                COALESCE(TABLE_COMMENT, '')
		         FROM information_schema.TABLES
		         WHERE TABLE_SCHEMA NOT IN ('mysql','information_schema','performance_schema','sys')
		           AND (? = '' OR TABLE_SCHEMA = ?)
		         ORDER BY 1, 2`
		args = []any{schema, schema}
	default:
		return nil, fmt.Errorf("%w: %s", ErrUnsupported, driver)
	}
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Table{}
	for rows.Next() {
		var t Table
		if err := rows.Scan(&t.Schema, &t.Name, &t.Type, &t.Rows, &t.Size, &t.Comment); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

type Column struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Nullable bool   `json:"nullable"`
	Default  string `json:"default,omitempty"`
	Key      string `json:"key,omitempty"`
	Position int    `json:"position"`
}

func ListColumns(ctx context.Context, db *sql.DB, driver Driver, schema, table string) ([]Column, error) {
	if driver == DriverSQLite {
		return sqliteColumns(ctx, db, table)
	}
	var (
		query string
		args  []any
	)
	switch driver {
	case DriverPostgres:
		query = `SELECT column_name, data_type, is_nullable = 'YES',
		                COALESCE(column_default, ''), ordinal_position
		         FROM information_schema.columns
		         WHERE table_schema = $1 AND table_name = $2
		         ORDER BY ordinal_position`
		args = []any{schema, table}
	case DriverMySQL:
		query = `SELECT COLUMN_NAME, COLUMN_TYPE, IS_NULLABLE = 'YES',
		                COALESCE(COLUMN_DEFAULT, ''), ORDINAL_POSITION
		         FROM information_schema.COLUMNS
		         WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ?
		         ORDER BY ORDINAL_POSITION`
		args = []any{schema, table}
	default:
		return nil, fmt.Errorf("%w: %s", ErrUnsupported, driver)
	}
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Column{}
	for rows.Next() {
		var c Column
		if err := rows.Scan(&c.Name, &c.Type, &c.Nullable, &c.Default, &c.Position); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// sqliteColumns reads columns from PRAGMA table_info. The pragma cannot bind a
// parameter for the table name, so the identifier is validated and quoted the
// same way every other generated statement in this package is.
func sqliteColumns(ctx context.Context, db *sql.DB, table string) ([]Column, error) {
	qTable, err := quoteIdent(DriverSQLite, table)
	if err != nil {
		return nil, err
	}
	rows, err := db.QueryContext(ctx, "PRAGMA table_info("+qTable+")")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Column{}
	for rows.Next() {
		var (
			cid, notnull, pk int
			name, ctype      string
			dflt             sql.NullString
		)
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			return nil, err
		}
		c := Column{
			Name: name, Type: ctype, Nullable: notnull == 0,
			Default: dflt.String, Position: cid + 1,
		}
		if pk > 0 {
			c.Key = "PRI"
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// identifierRe restricts what may be interpolated into a generated query.
// Table and schema names cannot be bound as parameters, so the only safe
// option is to reject anything that is not a plain identifier and then quote it.
var identifierRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_$]{0,62}$`)

func quoteIdent(driver Driver, name string) (string, error) {
	if !identifierRe.MatchString(name) {
		return "", fmt.Errorf("invalid identifier %q", name)
	}
	if driver == DriverMySQL {
		return "`" + name + "`", nil
	}
	return `"` + name + `"`, nil
}

// BrowseTable pages through a table's rows. It builds the statement from
// validated, quoted identifiers and binds limit and offset as parameters.
func BrowseTable(ctx context.Context, db *sql.DB, driver Driver, schema, table string, limit, offset int) (*QueryResult, error) {
	rel, err := qualifiedName(driver, schema, table)
	if err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}
	// No ORDER BY: adding one would force a sort over an arbitrary column on a
	// table that may have millions of rows and no index on it, turning a cheap
	// page fetch into a full scan. Ordered access is what the Query tab is for.
	query := fmt.Sprintf(`SELECT * FROM %s LIMIT %s OFFSET %s`,
		rel, driver.placeholder(1), driver.placeholder(2))
	return RunQuery(ctx, db, query, limit, limit, offset)
}

// qualifiedName renders a validated, quoted schema.table reference. SQLite has
// no per-schema namespacing worth qualifying, and an empty schema (the common
// case for it) yields a bare table name rather than a dangling dot.
func qualifiedName(driver Driver, schema, table string) (string, error) {
	qTable, err := quoteIdent(driver, table)
	if err != nil {
		return "", err
	}
	if schema == "" || driver == DriverSQLite {
		return qTable, nil
	}
	qSchema, err := quoteIdent(driver, schema)
	if err != nil {
		return "", err
	}
	return qSchema + "." + qTable, nil
}

type QueryResult struct {
	Columns   []string `json:"columns"`
	Types     []string `json:"types"`
	Rows      [][]any  `json:"rows"`
	RowCount  int      `json:"rowCount"`
	Affected  int64    `json:"rowsAffected"`
	Duration  string   `json:"duration"`
	Truncated bool     `json:"truncated"`
	Statement string   `json:"statement"`
}

// RunQuery executes a statement and materialises the result set. Rows beyond
// maxRows are dropped and flagged rather than streamed: a browser table is not
// where a million-row result belongs.
func RunQuery(ctx context.Context, db *sql.DB, query string, maxRows int, args ...any) (*QueryResult, error) {
	if maxRows <= 0 || maxRows > 5000 {
		maxRows = 500
	}
	start := time.Now()
	res := &QueryResult{Columns: []string{}, Types: []string{}, Rows: [][]any{}, Statement: query}

	if !returnsRows(query) {
		exec, err := db.ExecContext(ctx, query, args...)
		if err != nil {
			return nil, err
		}
		res.Affected, _ = exec.RowsAffected()
		res.Duration = time.Since(start).Round(time.Microsecond).String()
		return res, nil
	}

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	res.Columns = cols
	if types, err := rows.ColumnTypes(); err == nil {
		for _, t := range types {
			res.Types = append(res.Types, t.DatabaseTypeName())
		}
	}
	for rows.Next() {
		if len(res.Rows) >= maxRows {
			res.Truncated = true
			break
		}
		holders := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range holders {
			ptrs[i] = &holders[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, err
		}
		row := make([]any, len(cols))
		for i, v := range holders {
			row[i] = normaliseValue(v)
		}
		res.Rows = append(res.Rows, row)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	res.RowCount = len(res.Rows)
	res.Duration = time.Since(start).Round(time.Microsecond).String()
	return res, nil
}

// normaliseValue converts driver values into something JSON can carry.
// []byte in particular would otherwise be base64-encoded and unreadable for
// what is almost always text.
func normaliseValue(v any) any {
	switch t := v.(type) {
	case nil:
		return nil
	case []byte:
		return string(t)
	case time.Time:
		return t.UTC().Format(time.RFC3339Nano)
	default:
		return v
	}
}

func returnsRows(query string) bool {
	trimmed := strings.ToLower(normaliseSQL(query))
	for _, prefix := range []string{"select", "with", "show", "describe", "desc", "explain", "table", "values", "pragma"} {
		if strings.HasPrefix(trimmed, prefix) {
			return true
		}
	}
	// A statement with RETURNING produces rows even though it mutates.
	return strings.Contains(trimmed, " returning ")
}

// normaliseSQL strips comments and collapses whitespace so that a statement
// cannot hide its verb between the words the patterns below look for. The
// classification is what decides whether the caller needs the destructive
// capability, so a gap here is an authorisation gap: "DELETE/**/FROM users"
// used to classify as a read and ran with no capability check and no typed
// confirmation.
//
// String literals are copied through verbatim rather than removed. Removing
// them would let `SELECT 'x--'` swallow the statement that follows it, and
// keeping them can only over-report — a SELECT whose text contains the word
// "delete" costs the operator one extra confirmation, which is the direction
// this function is allowed to be wrong in.
func normaliseSQL(q string) string {
	var b strings.Builder
	b.Grow(len(q))
	for i := 0; i < len(q); i++ {
		c := q[i]
		switch {
		case c == '\'' || c == '"' || c == '`':
			quote := c
			b.WriteByte(c)
			for i++; i < len(q); i++ {
				b.WriteByte(q[i])
				if q[i] == '\\' && quote != '`' && i+1 < len(q) {
					i++
					b.WriteByte(q[i])
					continue
				}
				if q[i] == quote {
					break
				}
			}
		case c == '#', c == '-' && i+1 < len(q) && q[i+1] == '-':
			for i < len(q) && q[i] != '\n' {
				i++
			}
			b.WriteByte(' ')
		case c == '/' && i+1 < len(q) && q[i+1] == '*':
			i += 2
			for i+1 < len(q) && !(q[i] == '*' && q[i+1] == '/') {
				i++
			}
			i++
			b.WriteByte(' ')
		default:
			b.WriteByte(c)
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

// Risk classifies a statement so the UI can warn before it runs.
type Risk struct {
	Destructive bool     `json:"destructive"`
	Level       string   `json:"level"`
	Reasons     []string `json:"reasons"`
}

// Go's regexp package is RE2, which has no negative lookahead, so "mutates
// every row" is expressed as a positive match plus an absence check rather
// than one pattern.
var (
	deleteRe    = regexp.MustCompile(`(?is)\bdelete\b`)
	updateSetRe = regexp.MustCompile(`(?is)\bupdate\s+\S+\s+set\b`)
	whereRe     = regexp.MustCompile(`(?is)\bwhere\b`)
)

func matches(re *regexp.Regexp) func(string) bool {
	return re.MatchString
}

// unscoped reports a statement that mutates without a WHERE clause — the
// difference between "deletes rows" and "empties the table".
func unscoped(re *regexp.Regexp) func(string) bool {
	return func(q string) bool { return re.MatchString(q) && !whereRe.MatchString(q) }
}

var riskPatterns = []struct {
	match  func(string) bool
	level  string
	reason string
}{
	// Deliberately "any DROP" rather than a list of object types: the list
	// omitted ROLE, OWNED, FUNCTION and everything a future dialect adds, and
	// each omission was a statement that ran without confirmation.
	{matches(regexp.MustCompile(`(?is)\bdrop\b`)), "critical", "drops a database object"},
	{matches(regexp.MustCompile(`(?is)\btruncate\b`)), "critical", "truncates a table"},
	{matches(regexp.MustCompile(`(?is)\bcopy\b.*\bfrom\s+program\b`)), "critical", "runs a shell command on the database host"},
	{unscoped(deleteRe), "critical", "deletes every row (no WHERE clause)"},
	{unscoped(updateSetRe), "critical", "updates every row (no WHERE clause)"},
	{matches(deleteRe), "high", "deletes rows"},
	{matches(regexp.MustCompile(`(?is)\bupdate\b`)), "high", "updates rows"},
	{matches(regexp.MustCompile(`(?is)\balter\b`)), "high", "alters a database object"},
	{matches(regexp.MustCompile(`(?is)\bgrant\b|\brevoke\b`)), "high", "changes permissions"},
	{matches(regexp.MustCompile(`(?is)\binsert\s+into\b|\breplace\s+into\b`)), "medium", "inserts rows"},
	{matches(regexp.MustCompile(`(?is)\bcreate\b`)), "medium", "creates a database object"},
}

// readOnlyLeaders are the statement forms that cannot change anything. PRAGMA
// is absent on purpose — `PRAGMA journal_mode=WAL` writes.
var readOnlyLeaders = map[string]bool{
	"select": true, "with": true, "show": true, "describe": true,
	"desc": true, "explain": true, "table": true, "values": true,
}

func Classify(query string) Risk {
	q := normaliseSQL(query)
	risk := Risk{Level: "read", Reasons: []string{}}
	for _, p := range riskPatterns {
		if p.match(q) {
			risk.Reasons = append(risk.Reasons, p.reason)
			if rank(p.level) > rank(risk.Level) {
				risk.Level = p.level
			}
		}
	}
	// Fail closed. An unrecognised statement — DO, CALL, VACUUM, whatever the
	// next dialect adds — used to be indistinguishable from a SELECT, and the
	// query runner derives its capability check from this verdict. Costing the
	// operator a confirmation for a statement nobody enumerated is the right
	// side to be wrong on.
	if risk.Level == "read" && !readOnly(q) {
		risk.Level = "high"
		risk.Reasons = append(risk.Reasons, "statement is not a recognised read")
	}
	risk.Destructive = risk.Level == "critical" || risk.Level == "high"
	return risk
}

// readOnly reports whether every statement in q leads with a read-only verb.
func readOnly(q string) bool {
	for _, stmt := range strings.Split(q, ";") {
		stmt = strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(stmt), "("))
		if stmt == "" {
			continue
		}
		word := strings.ToLower(stmt)
		if i := strings.IndexAny(word, " \t(\""); i >= 0 {
			word = word[:i]
		}
		if !readOnlyLeaders[word] {
			return false
		}
	}
	return true
}

func rank(level string) int {
	switch level {
	case "critical":
		return 3
	case "high":
		return 2
	case "medium":
		return 1
	default:
		return 0
	}
}
