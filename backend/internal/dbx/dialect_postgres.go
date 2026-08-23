package dbx

import (
	"context"
	"database/sql"
)

type postgresDialect struct{}

func (postgresDialect) Driver() Driver               { return DriverPostgres }
func (postgresDialect) SQLDriverName() string        { return "pgx" }
func (postgresDialect) NormaliseDSN(d string) string { return d }
func (postgresDialect) VersionQuery() string         { return "SELECT version()" }
func (postgresDialect) TunePool(*sql.DB)             {}
func (postgresDialect) SupportsReturning() bool      { return true }
func (postgresDialect) DefaultSchema() string        { return "public" }
func (postgresDialect) SupportsDDL() bool            { return true }

func (postgresDialect) QuoteIdent(name string) (string, error) { return quoteDouble(name) }

// Postgres numbers its bind markers, so the same value used twice must be
// passed twice unless the caller reuses the number deliberately.
func (postgresDialect) Placeholder(n int) string { return "$" + itoa(n) }

func (d postgresDialect) Paginate(limit, offset, argStart int) (string, []any) {
	return standardLimit(d, limit, offset, argStart)
}

func (postgresDialect) ColumnTypes() []string {
	return []string{
		"text", "varchar(255)", "char(1)", "boolean",
		"smallint", "integer", "bigint", "serial", "bigserial",
		"numeric(10,2)", "real", "double precision",
		"date", "time", "timestamp", "timestamptz", "interval",
		"json", "jsonb", "uuid", "bytea", "inet",
	}
}

func (postgresDialect) Databases(ctx context.Context, db *sql.DB) ([]Database, error) {
	rows, err := db.QueryContext(ctx, `SELECT d.datname,
	                pg_database_size(d.datname),
	                COALESCE(pg_get_userbyid(d.datdba), ''),
	                pg_encoding_to_char(d.encoding)
	         FROM pg_database d
	         WHERE NOT d.datistemplate
	         ORDER BY 1`)
	if err != nil {
		return nil, err
	}
	return scanDatabases(rows)
}

func (postgresDialect) Tables(ctx context.Context, db *sql.DB, schema string) ([]Table, error) {
	rows, err := db.QueryContext(ctx, `SELECT n.nspname, c.relname,
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
	         ORDER BY 1, 2`, schema)
	if err != nil {
		return nil, err
	}
	return scanTables(rows)
}

func (d postgresDialect) Columns(ctx context.Context, db *sql.DB, schema, table string) ([]Column, error) {
	return infoSchemaColumns(ctx, db, d, schema, table)
}

func (d postgresDialect) PrimaryKey(ctx context.Context, db *sql.DB, schema, table string) ([]string, error) {
	return infoSchemaPrimaryKey(ctx, db, d, schema, table)
}

// Indexes reads pg_index rather than information_schema, which has no index
// view at all. unnest ... WITH ORDINALITY is what keeps a composite index's
// columns in their declared order rather than alphabetical.
func (postgresDialect) Indexes(ctx context.Context, db *sql.DB, schema, table string) ([]Index, error) {
	rows, err := db.QueryContext(ctx, `SELECT i.relname, ix.indisunique, ix.indisprimary, a.attname
	          FROM pg_class t
	          JOIN pg_namespace n ON n.oid = t.relnamespace
	          JOIN pg_index ix ON t.oid = ix.indrelid
	          JOIN pg_class i ON i.oid = ix.indexrelid
	          JOIN unnest(ix.indkey) WITH ORDINALITY AS k(attnum, ord) ON true
	          JOIN pg_attribute a ON a.attrelid = t.oid AND a.attnum = k.attnum
	          WHERE n.nspname = $1 AND t.relname = $2
	          ORDER BY i.relname, k.ord`, schema, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	acc := newIndexAcc()
	for rows.Next() {
		var name, col string
		var unique, primary bool
		if err := rows.Scan(&name, &unique, &primary, &col); err != nil {
			return nil, err
		}
		acc.add(name, col, unique, primary)
	}
	return acc.slice(), rows.Err()
}

func (postgresDialect) ForeignKeys(ctx context.Context, db *sql.DB, schema, table string) ([]ForeignKey, error) {
	rows, err := db.QueryContext(ctx, `SELECT con.conname, att.attname, nsp.nspname, cl.relname, att2.attname,
	                 con.confupdtype, con.confdeltype
	          FROM pg_constraint con
	          JOIN pg_class rel ON rel.oid = con.conrelid
	          JOIN pg_namespace rn ON rn.oid = rel.relnamespace
	          JOIN pg_class cl ON cl.oid = con.confrelid
	          JOIN pg_namespace nsp ON nsp.oid = cl.relnamespace
	          JOIN unnest(con.conkey) WITH ORDINALITY AS k(attnum, ord) ON true
	          JOIN pg_attribute att ON att.attrelid = con.conrelid AND att.attnum = k.attnum
	          JOIN unnest(con.confkey) WITH ORDINALITY AS fk(attnum, ord) ON fk.ord = k.ord
	          JOIN pg_attribute att2 ON att2.attrelid = con.confrelid AND att2.attnum = fk.attnum
	          WHERE con.contype = 'f' AND rn.nspname = $1 AND rel.relname = $2
	          ORDER BY con.conname, k.ord`, schema, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	acc := newFKAcc()
	for rows.Next() {
		var name, col, refSchema, refTable, refCol, upd, del string
		if err := rows.Scan(&name, &col, &refSchema, &refTable, &refCol, &upd, &del); err != nil {
			return nil, err
		}
		fk := acc.get(name)
		fk.Columns = append(fk.Columns, col)
		fk.RefSchema, fk.RefTable = refSchema, refTable
		fk.RefColumns = append(fk.RefColumns, refCol)
		fk.OnUpdate, fk.OnDelete = pgFKAction(upd), pgFKAction(del)
	}
	return acc.slice(), rows.Err()
}

// pgFKAction maps pg_constraint's single-char action codes to words.
func pgFKAction(c string) string {
	switch c {
	case "a":
		return "NO ACTION"
	case "r":
		return "RESTRICT"
	case "c":
		return "CASCADE"
	case "n":
		return "SET NULL"
	case "d":
		return "SET DEFAULT"
	default:
		return ""
	}
}

// Postgres keeps no canonical DDL text — pg_dump reconstructs it — so the
// statement is synthesised from what was introspected and labelled as generated
// where it is shown.
func (d postgresDialect) CreateSQL(_ context.Context, _ *sql.DB, schema, table string, detail *TableDetail) (string, error) {
	return synthCreateTable(d, schema, table, detail), nil
}

func (postgresDialect) CastText(e string) string { return "CAST(" + e + " AS TEXT)" }

func (postgresDialect) AddColumnKeyword() string { return "ADD COLUMN" }

func (postgresDialect) BeforeDropColumn(context.Context, *sql.DB, string, string, string) error {
	return nil
}
