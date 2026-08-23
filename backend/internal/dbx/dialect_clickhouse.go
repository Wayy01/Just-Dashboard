package dbx

import (
	"context"
	"database/sql"
	"strings"
)

type clickhouseDialect struct{}

func (clickhouseDialect) Driver() Driver                      { return DriverClickHouse }
func (clickhouseDialect) SQLDriverName() string               { return "clickhouse" }
func (clickhouseDialect) NormaliseDSN(d string) string        { return d }
func (clickhouseDialect) VersionQuery() string                { return "SELECT 'ClickHouse ' || version()" }
func (clickhouseDialect) TunePool(*sql.DB)                    {}
func (clickhouseDialect) Placeholder(int) string              { return "?" }
func (clickhouseDialect) DefaultSchema() string               { return "default" }
func (clickhouseDialect) SupportsReturning() bool             { return false }
func (clickhouseDialect) QuoteIdent(s string) (string, error) { return quoteBacktick(s) }

// ClickHouse's CREATE TABLE requires a table engine and, for the MergeTree
// family that anyone actually uses, a sorting key. A generic column-list form
// would emit statements the server rejects, so the DDL builder declines to
// offer itself here rather than producing plausible-looking SQL that never
// runs. Its own SQL console remains available through the Query tab.
func (clickhouseDialect) SupportsDDL() bool { return false }

func (d clickhouseDialect) Paginate(limit, offset, argStart int) (string, []any) {
	return standardLimit(d, limit, offset, argStart)
}

func (clickhouseDialect) ColumnTypes() []string {
	return []string{
		"String", "FixedString(16)", "UUID",
		"Int8", "Int16", "Int32", "Int64", "UInt8", "UInt32", "UInt64",
		"Float32", "Float64", "Decimal(18,4)",
		"Date", "DateTime", "DateTime64(3)",
		"Bool", "JSON", "Array(String)",
	}
}

func (clickhouseDialect) Databases(ctx context.Context, db *sql.DB) ([]Database, error) {
	rows, err := db.QueryContext(ctx, `
	  SELECT d.name,
	         toInt64(ifNull((SELECT sum(bytes_on_disk) FROM system.parts p
	                          WHERE p.database = d.name AND p.active), 0)),
	         ifNull(d.engine, ''),
	         ''
	  FROM system.databases d
	  ORDER BY d.name`)
	if err != nil {
		return nil, err
	}
	return scanDatabases(rows)
}

func (clickhouseDialect) Tables(ctx context.Context, db *sql.DB, schema string) ([]Table, error) {
	rows, err := db.QueryContext(ctx, `
	  SELECT database, name,
	         if(engine LIKE '%View', 'view', 'table'),
	         toInt64(ifNull(total_rows, 0)),
	         toInt64(ifNull(total_bytes, 0)),
	         ifNull(comment, '')
	  FROM system.tables
	  WHERE database NOT IN ('system','INFORMATION_SCHEMA','information_schema')
	    AND (? = '' OR database = ?)
	  ORDER BY database, name`, schema, schema)
	if err != nil {
		return nil, err
	}
	return scanTables(rows)
}

// Columns reads system.columns rather than information_schema: only the former
// reports whether a column is part of the sorting key, and ClickHouse spells
// optionality as a Nullable(...) wrapper on the type rather than a flag.
func (clickhouseDialect) Columns(ctx context.Context, db *sql.DB, schema, table string) ([]Column, error) {
	rows, err := db.QueryContext(ctx, `
	  SELECT name, type, ifNull(default_expression, ''), position, is_in_primary_key
	  FROM system.columns
	  WHERE database = ? AND table = ?
	  ORDER BY position`, schema, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Column{}
	for rows.Next() {
		var c Column
		var inPK uint8
		if err := rows.Scan(&c.Name, &c.Type, &c.Default, &c.Position, &inPK); err != nil {
			return nil, err
		}
		c.Nullable = strings.HasPrefix(c.Type, "Nullable(")
		if inPK == 1 {
			c.Key = "PRI"
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// PrimaryKey reports the sorting key columns. ClickHouse has no primary-key
// constraint in the relational sense — the key orders parts and is not unique —
// so a row edit keyed on it could match more than one row. That is why the
// mutation path checks uniqueness separately rather than trusting this.
func (clickhouseDialect) PrimaryKey(ctx context.Context, db *sql.DB, schema, table string) ([]string, error) {
	rows, err := db.QueryContext(ctx, `
	  SELECT name FROM system.columns
	  WHERE database = ? AND table = ? AND is_in_primary_key = 1
	  ORDER BY position`, schema, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// Indexes reports the data-skipping indices, which are the only named index
// objects ClickHouse has. The sorting key is reported as a synthetic primary
// entry so the Structure tab can show what the table is actually ordered by.
func (clickhouseDialect) Indexes(ctx context.Context, db *sql.DB, schema, table string) ([]Index, error) {
	out := []Index{}
	var sortingKey string
	if err := db.QueryRowContext(ctx,
		`SELECT ifNull(sorting_key, '') FROM system.tables WHERE database = ? AND name = ?`,
		schema, table).Scan(&sortingKey); err == nil && sortingKey != "" {
		cols := []string{}
		for _, c := range strings.Split(sortingKey, ",") {
			if c = strings.TrimSpace(c); c != "" {
				cols = append(cols, c)
			}
		}
		out = append(out, Index{Name: "sorting key", Columns: cols, Primary: true})
	}
	rows, err := db.QueryContext(ctx,
		`SELECT name, expr FROM system.data_skipping_indices WHERE database = ? AND table = ?`,
		schema, table)
	if err != nil {
		// A server too old for this table still has a usable sorting key above.
		return out, nil
	}
	defer rows.Close()
	for rows.Next() {
		var name, expr string
		if err := rows.Scan(&name, &expr); err != nil {
			return out, nil
		}
		out = append(out, Index{Name: name, Columns: []string{expr}})
	}
	return out, nil
}

// ClickHouse has no referential constraints at all.
func (clickhouseDialect) ForeignKeys(ctx context.Context, db *sql.DB, schema, table string) ([]ForeignKey, error) {
	return noForeignKeys(ctx, db, schema, table)
}

func (clickhouseDialect) CreateSQL(ctx context.Context, db *sql.DB, schema, table string, _ *TableDetail) (string, error) {
	rel, err := qualify(clickhouseDialect{}, schema, table)
	if err != nil {
		return "", err
	}
	var ddl string
	if err := db.QueryRowContext(ctx, "SHOW CREATE TABLE "+rel).Scan(&ddl); err != nil {
		return "", err
	}
	// ClickHouse returns the statement with literal \n escapes in some client
	// protocols; rendering those as newlines is the difference between readable
	// DDL and one very long line.
	return strings.ReplaceAll(ddl, `\n`, "\n"), nil
}

func (clickhouseDialect) CastText(e string) string { return "toString(" + e + ")" }
