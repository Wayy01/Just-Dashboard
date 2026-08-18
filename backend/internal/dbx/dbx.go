// Package dbx provides read-mostly database administration: browsing schemas,
// running queries, and dumping or restoring data.
//
// Connection strings are secrets — they carry credentials — so they are held
// encrypted at rest and never returned to a client. A query runner is
// inherently powerful, so destructive statements are classified before they
// run and the handler demands a typed confirmation for them.
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
)

type Driver string

const (
	DriverPostgres Driver = "postgres"
	DriverMySQL    Driver = "mysql"
	DriverMongo    Driver = "mongodb"
)

var ErrUnsupported = errors.New("unsupported database driver")

func (d Driver) Valid() bool {
	switch d {
	case DriverPostgres, DriverMySQL, DriverMongo:
		return true
	}
	return false
}

// sqlDriverName maps our driver identity onto the registered database/sql
// driver. pgx registers itself as "pgx" through its stdlib shim.
func (d Driver) sqlDriverName() (string, error) {
	switch d {
	case DriverPostgres:
		return "pgx", nil
	case DriverMySQL:
		return "mysql", nil
	default:
		return "", fmt.Errorf("%w: %s", ErrUnsupported, d)
	}
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
	db, err := sql.Open(name, dsn)
	if err != nil {
		return nil, err
	}
	// A dashboard is not the application: a handful of connections is plenty,
	// and a large pool here would compete with the real workload.
	db.SetMaxOpenConns(5)
	db.SetMaxIdleConns(2)
	db.SetConnMaxLifetime(30 * time.Minute)

	pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := db.PingContext(pingCtx); err != nil {
		db.Close()
		return nil, err
	}
	m.pools[id] = db
	return db, nil
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
	Name  string `json:"name"`
	Size  int64  `json:"size,omitempty"`
	Owner string `json:"owner,omitempty"`
	Encoding string `json:"encoding,omitempty"`
}

func ListDatabases(ctx context.Context, db *sql.DB, driver Driver) ([]Database, error) {
	var query string
	switch driver {
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
	qSchema, err := quoteIdent(driver, schema)
	if err != nil {
		return nil, err
	}
	qTable, err := quoteIdent(driver, table)
	if err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}
	var query string
	if driver == DriverPostgres {
		query = fmt.Sprintf(`SELECT * FROM %s.%s LIMIT $1 OFFSET $2`, qSchema, qTable)
	} else {
		query = fmt.Sprintf(`SELECT * FROM %s.%s LIMIT ? OFFSET ?`, qSchema, qTable)
	}
	return RunQuery(ctx, db, query, limit, limit, offset)
}

type QueryResult struct {
	Columns   []string         `json:"columns"`
	Types     []string         `json:"types"`
	Rows      [][]any          `json:"rows"`
	RowCount  int              `json:"rowCount"`
	Affected  int64            `json:"rowsAffected"`
	Duration  string           `json:"duration"`
	Truncated bool             `json:"truncated"`
	Statement string           `json:"statement"`
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
	trimmed := strings.ToLower(strings.TrimSpace(stripLeadingComments(query)))
	for _, prefix := range []string{"select", "with", "show", "describe", "desc", "explain", "table", "values", "pragma"} {
		if strings.HasPrefix(trimmed, prefix) {
			return true
		}
	}
	// A statement with RETURNING produces rows even though it mutates.
	return strings.Contains(trimmed, " returning ")
}

func stripLeadingComments(q string) string {
	for {
		q = strings.TrimSpace(q)
		if strings.HasPrefix(q, "--") {
			if idx := strings.Index(q, "\n"); idx >= 0 {
				q = q[idx+1:]
				continue
			}
			return ""
		}
		if strings.HasPrefix(q, "/*") {
			if idx := strings.Index(q, "*/"); idx >= 0 {
				q = q[idx+2:]
				continue
			}
			return ""
		}
		return q
	}
}

// Risk classifies a statement so the UI can warn before it runs.
type Risk struct {
	Destructive bool     `json:"destructive"`
	Level       string   `json:"level"`
	Reasons     []string `json:"reasons"`
}

var riskPatterns = []struct {
	re     *regexp.Regexp
	level  string
	reason string
}{
	{regexp.MustCompile(`(?is)\bdrop\s+(database|schema|table|view|index|column)\b`), "critical", "drops a database object"},
	{regexp.MustCompile(`(?is)\btruncate\b`), "critical", "truncates a table"},
	{regexp.MustCompile(`(?is)\bdelete\s+from\b(?![\s\S]*\bwhere\b)`), "critical", "deletes every row (no WHERE clause)"},
	{regexp.MustCompile(`(?is)\bupdate\s+\S+\s+set\b(?![\s\S]*\bwhere\b)`), "critical", "updates every row (no WHERE clause)"},
	{regexp.MustCompile(`(?is)\bdelete\s+from\b`), "high", "deletes rows"},
	{regexp.MustCompile(`(?is)\bupdate\b`), "high", "updates rows"},
	{regexp.MustCompile(`(?is)\balter\s+table\b`), "high", "alters a table definition"},
	{regexp.MustCompile(`(?is)\bgrant\b|\brevoke\b`), "high", "changes permissions"},
	{regexp.MustCompile(`(?is)\binsert\s+into\b|\breplace\s+into\b`), "medium", "inserts rows"},
	{regexp.MustCompile(`(?is)\bcreate\b`), "medium", "creates a database object"},
}

func Classify(query string) Risk {
	risk := Risk{Level: "read", Reasons: []string{}}
	for _, p := range riskPatterns {
		if p.re.MatchString(query) {
			risk.Reasons = append(risk.Reasons, p.reason)
			if rank(p.level) > rank(risk.Level) {
				risk.Level = p.level
			}
		}
	}
	risk.Destructive = risk.Level == "critical" || risk.Level == "high"
	return risk
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
