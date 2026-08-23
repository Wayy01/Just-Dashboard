package dbx

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
)

// Index is one index on a table, with the columns it covers in order.
type Index struct {
	Name    string   `json:"name"`
	Columns []string `json:"columns"`
	Unique  bool     `json:"unique"`
	Primary bool     `json:"primary"`
}

// ForeignKey is one referential constraint. Composite keys keep their columns
// paired with the referenced columns in order, which is what a relationship
// diagram and a "jump to referenced row" action both need.
type ForeignKey struct {
	Name       string   `json:"name"`
	Columns    []string `json:"columns"`
	RefSchema  string   `json:"refSchema,omitempty"`
	RefTable   string   `json:"refTable"`
	RefColumns []string `json:"refColumns"`
	OnUpdate   string   `json:"onUpdate,omitempty"`
	OnDelete   string   `json:"onDelete,omitempty"`
}

// TableDetail is everything the Structure tab shows and everything row editing
// needs: the column list, which columns form the primary key (so an edit can be
// scoped to one row), the indexes, the outgoing foreign keys, and the DDL that
// would recreate the table.
type TableDetail struct {
	Schema      string       `json:"schema"`
	Name        string       `json:"name"`
	Columns     []Column     `json:"columns"`
	PrimaryKey  []string     `json:"primaryKey"`
	Indexes     []Index      `json:"indexes"`
	ForeignKeys []ForeignKey `json:"foreignKeys"`
	CreateSQL   string       `json:"createSql,omitempty"`
}

// Detail assembles a TableDetail with one call per fact rather than one giant
// join, because the failure modes differ: a table with no indexes and a
// permission error reading pg_index must not look the same, and a driver that
// cannot answer one of these should still return the rest.
func Detail(ctx context.Context, db *sql.DB, driver Driver, schema, table string) (*TableDetail, error) {
	cols, err := ListColumns(ctx, db, driver, schema, table)
	if err != nil {
		return nil, err
	}
	d := &TableDetail{Schema: schema, Name: table, Columns: cols, PrimaryKey: []string{}, Indexes: []Index{}, ForeignKeys: []ForeignKey{}}
	if d.PrimaryKey, err = PrimaryKeyColumns(ctx, db, driver, schema, table); err != nil {
		return nil, err
	}
	if d.Indexes, err = listIndexes(ctx, db, driver, schema, table); err != nil {
		return nil, err
	}
	if d.ForeignKeys, err = listForeignKeys(ctx, db, driver, schema, table); err != nil {
		return nil, err
	}
	d.CreateSQL, _ = createSQL(ctx, db, driver, schema, table, d)
	return d, nil
}

// PrimaryKeyColumns returns the ordered primary-key columns. Row editing refuses
// to run without one, so this is a security-relevant answer as much as a
// descriptive one: it is what bounds an UPDATE or DELETE to a single row.
func PrimaryKeyColumns(ctx context.Context, db *sql.DB, driver Driver, schema, table string) ([]string, error) {
	switch driver {
	case DriverSQLite:
		qTable, err := quoteIdent(DriverSQLite, table)
		if err != nil {
			return nil, err
		}
		rows, err := db.QueryContext(ctx, "PRAGMA table_info("+qTable+")")
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		type pkCol struct {
			name string
			pos  int
		}
		var pks []pkCol
		for rows.Next() {
			var (
				cid, notnull, pk int
				name, ctype      string
				dflt             sql.NullString
			)
			if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
				return nil, err
			}
			if pk > 0 {
				pks = append(pks, pkCol{name, pk})
			}
		}
		if err := rows.Err(); err != nil {
			return nil, err
		}
		sort.Slice(pks, func(i, j int) bool { return pks[i].pos < pks[j].pos })
		out := []string{}
		for _, p := range pks {
			out = append(out, p.name)
		}
		return out, nil
	case DriverPostgres, DriverMySQL:
		// information_schema is standard SQL, so one query serves both engines;
		// only the placeholder differs.
		query := `SELECT kcu.column_name
		          FROM information_schema.table_constraints tc
		          JOIN information_schema.key_column_usage kcu
		            ON kcu.constraint_name = tc.constraint_name
		           AND kcu.table_schema = tc.table_schema
		           AND kcu.table_name = tc.table_name
		          WHERE tc.constraint_type = 'PRIMARY KEY'
		            AND tc.table_schema = ` + driver.placeholder(1) + `
		            AND tc.table_name = ` + driver.placeholder(2) + `
		          ORDER BY kcu.ordinal_position`
		rows, err := db.QueryContext(ctx, query, schema, table)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		out := []string{}
		for rows.Next() {
			var name string
			if err := rows.Scan(&name); err != nil {
				return nil, err
			}
			out = append(out, name)
		}
		return out, rows.Err()
	default:
		return nil, fmt.Errorf("%w: %s", ErrUnsupported, driver)
	}
}

func listIndexes(ctx context.Context, db *sql.DB, driver Driver, schema, table string) ([]Index, error) {
	switch driver {
	case DriverSQLite:
		return sqliteIndexes(ctx, db, table)
	case DriverMySQL:
		return mysqlIndexes(ctx, db, schema, table)
	case DriverPostgres:
		return postgresIndexes(ctx, db, schema, table)
	default:
		return nil, fmt.Errorf("%w: %s", ErrUnsupported, driver)
	}
}

// grouped accumulates rows that arrive one column at a time into ordered
// per-name groups, preserving first-seen order of the groups themselves.
type indexAcc struct {
	order []string
	byKey map[string]*Index
}

func newIndexAcc() *indexAcc { return &indexAcc{byKey: map[string]*Index{}} }

func (a *indexAcc) add(name, col string, unique, primary bool) {
	ix, ok := a.byKey[name]
	if !ok {
		ix = &Index{Name: name, Unique: unique, Primary: primary, Columns: []string{}}
		a.byKey[name] = ix
		a.order = append(a.order, name)
	}
	if col != "" {
		ix.Columns = append(ix.Columns, col)
	}
}

func (a *indexAcc) slice() []Index {
	out := make([]Index, 0, len(a.order))
	for _, n := range a.order {
		out = append(out, *a.byKey[n])
	}
	return out
}

func postgresIndexes(ctx context.Context, db *sql.DB, schema, table string) ([]Index, error) {
	query := `SELECT i.relname, ix.indisunique, ix.indisprimary, a.attname
	          FROM pg_class t
	          JOIN pg_namespace n ON n.oid = t.relnamespace
	          JOIN pg_index ix ON t.oid = ix.indrelid
	          JOIN pg_class i ON i.oid = ix.indexrelid
	          JOIN unnest(ix.indkey) WITH ORDINALITY AS k(attnum, ord) ON true
	          JOIN pg_attribute a ON a.attrelid = t.oid AND a.attnum = k.attnum
	          WHERE n.nspname = $1 AND t.relname = $2
	          ORDER BY i.relname, k.ord`
	rows, err := db.QueryContext(ctx, query, schema, table)
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

func mysqlIndexes(ctx context.Context, db *sql.DB, schema, table string) ([]Index, error) {
	query := `SELECT INDEX_NAME, NON_UNIQUE, COLUMN_NAME
	          FROM information_schema.STATISTICS
	          WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ?
	          ORDER BY INDEX_NAME, SEQ_IN_INDEX`
	rows, err := db.QueryContext(ctx, query, schema, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	acc := newIndexAcc()
	for rows.Next() {
		var name, col string
		var nonUnique int
		if err := rows.Scan(&name, &nonUnique, &col); err != nil {
			return nil, err
		}
		acc.add(name, col, nonUnique == 0, name == "PRIMARY")
	}
	return acc.slice(), rows.Err()
}

func sqliteIndexes(ctx context.Context, db *sql.DB, table string) ([]Index, error) {
	qTable, err := quoteIdent(DriverSQLite, table)
	if err != nil {
		return nil, err
	}
	rows, err := db.QueryContext(ctx, "PRAGMA index_list("+qTable+")")
	if err != nil {
		return nil, err
	}
	type meta struct {
		name    string
		unique  bool
		primary bool
	}
	var metas []meta
	for rows.Next() {
		var (
			seq     int
			name    string
			unique  int
			origin  string
			partial int
		)
		if err := rows.Scan(&seq, &name, &unique, &origin, &partial); err != nil {
			rows.Close()
			return nil, err
		}
		metas = append(metas, meta{name: name, unique: unique == 1, primary: origin == "pk"})
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()

	out := []Index{}
	for _, m := range metas {
		qName, err := quoteIdent(DriverSQLite, m.name)
		if err != nil {
			continue
		}
		ir, err := db.QueryContext(ctx, "PRAGMA index_info("+qName+")")
		if err != nil {
			return nil, err
		}
		ix := Index{Name: m.name, Unique: m.unique, Primary: m.primary, Columns: []string{}}
		for ir.Next() {
			var seqno, cid int
			var col sql.NullString
			if err := ir.Scan(&seqno, &cid, &col); err != nil {
				ir.Close()
				return nil, err
			}
			if col.Valid {
				ix.Columns = append(ix.Columns, col.String)
			}
		}
		ir.Close()
		out = append(out, ix)
	}
	return out, nil
}

func listForeignKeys(ctx context.Context, db *sql.DB, driver Driver, schema, table string) ([]ForeignKey, error) {
	switch driver {
	case DriverSQLite:
		return sqliteForeignKeys(ctx, db, table)
	case DriverMySQL:
		return mysqlForeignKeys(ctx, db, schema, table)
	case DriverPostgres:
		return postgresForeignKeys(ctx, db, schema, table)
	default:
		return nil, fmt.Errorf("%w: %s", ErrUnsupported, driver)
	}
}

type fkAcc struct {
	order []string
	byKey map[string]*ForeignKey
}

func newFKAcc() *fkAcc { return &fkAcc{byKey: map[string]*ForeignKey{}} }

func (a *fkAcc) get(name string) *ForeignKey {
	fk, ok := a.byKey[name]
	if !ok {
		fk = &ForeignKey{Name: name, Columns: []string{}, RefColumns: []string{}}
		a.byKey[name] = fk
		a.order = append(a.order, name)
	}
	return fk
}

func (a *fkAcc) slice() []ForeignKey {
	out := make([]ForeignKey, 0, len(a.order))
	for _, n := range a.order {
		out = append(out, *a.byKey[n])
	}
	return out
}

func postgresForeignKeys(ctx context.Context, db *sql.DB, schema, table string) ([]ForeignKey, error) {
	query := `SELECT con.conname, att.attname, nsp.nspname, cl.relname, att2.attname,
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
	          ORDER BY con.conname, k.ord`
	rows, err := db.QueryContext(ctx, query, schema, table)
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

func mysqlForeignKeys(ctx context.Context, db *sql.DB, schema, table string) ([]ForeignKey, error) {
	query := `SELECT k.CONSTRAINT_NAME, k.COLUMN_NAME, k.REFERENCED_TABLE_SCHEMA,
	                 k.REFERENCED_TABLE_NAME, k.REFERENCED_COLUMN_NAME,
	                 COALESCE(r.UPDATE_RULE, ''), COALESCE(r.DELETE_RULE, '')
	          FROM information_schema.KEY_COLUMN_USAGE k
	          LEFT JOIN information_schema.REFERENTIAL_CONSTRAINTS r
	            ON r.CONSTRAINT_NAME = k.CONSTRAINT_NAME
	           AND r.CONSTRAINT_SCHEMA = k.TABLE_SCHEMA
	          WHERE k.TABLE_SCHEMA = ? AND k.TABLE_NAME = ?
	            AND k.REFERENCED_TABLE_NAME IS NOT NULL
	          ORDER BY k.CONSTRAINT_NAME, k.ORDINAL_POSITION`
	rows, err := db.QueryContext(ctx, query, schema, table)
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
		fk.OnUpdate, fk.OnDelete = upd, del
	}
	return acc.slice(), rows.Err()
}

func sqliteForeignKeys(ctx context.Context, db *sql.DB, table string) ([]ForeignKey, error) {
	qTable, err := quoteIdent(DriverSQLite, table)
	if err != nil {
		return nil, err
	}
	rows, err := db.QueryContext(ctx, "PRAGMA foreign_key_list("+qTable+")")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	acc := newFKAcc()
	for rows.Next() {
		var (
			id, seq                     int
			refTable, from, to          string
			onUpdate, onDelete, matchSp string
		)
		if err := rows.Scan(&id, &seq, &refTable, &from, &to, &onUpdate, &onDelete, &matchSp); err != nil {
			return nil, err
		}
		// SQLite has no names for foreign keys, so the constraint id doubles as
		// one; a from/to of "" means the FK references the referenced table's PK.
		fk := acc.get(fmt.Sprintf("fk_%d", id))
		fk.Columns = append(fk.Columns, from)
		fk.RefTable = refTable
		fk.RefColumns = append(fk.RefColumns, to)
		fk.OnUpdate, fk.OnDelete = onUpdate, onDelete
	}
	return acc.slice(), rows.Err()
}

// createSQL returns DDL that would recreate the table. SQLite and MySQL keep
// the original text and hand it back verbatim; Postgres has no such command, so
// the statement is synthesised from the introspected structure.
func createSQL(ctx context.Context, db *sql.DB, driver Driver, schema, table string, d *TableDetail) (string, error) {
	switch driver {
	case DriverSQLite:
		var ddl sql.NullString
		err := db.QueryRowContext(ctx,
			`SELECT sql FROM sqlite_master WHERE name = ? AND type IN ('table','view')`, table).Scan(&ddl)
		if err != nil {
			return "", err
		}
		return ddl.String, nil
	case DriverMySQL:
		rel, err := qualifiedName(DriverMySQL, schema, table)
		if err != nil {
			return "", err
		}
		var name, ddl string
		// SHOW CREATE TABLE returns two columns; a view returns a differently
		// named second column, so the values are scanned positionally.
		if err := db.QueryRowContext(ctx, "SHOW CREATE TABLE "+rel).Scan(&name, &ddl); err != nil {
			return "", err
		}
		return ddl, nil
	case DriverPostgres:
		return synthCreateTable(DriverPostgres, schema, table, d), nil
	default:
		return "", fmt.Errorf("%w: %s", ErrUnsupported, driver)
	}
}

// synthCreateTable builds a readable CREATE TABLE from an introspected detail.
// It is a faithful summary rather than a byte-exact reproduction — Postgres
// keeps no canonical DDL text, and column defaults and types come back already
// normalised — so it is labelled as generated where it is shown.
func synthCreateTable(driver Driver, schema, table string, d *TableDetail) string {
	rel, err := qualifiedName(driver, schema, table)
	if err != nil {
		rel = table
	}
	var b strings.Builder
	fmt.Fprintf(&b, "CREATE TABLE %s (\n", rel)
	lines := []string{}
	for _, c := range d.Columns {
		q, err := quoteIdent(driver, c.Name)
		if err != nil {
			q = c.Name
		}
		line := "  " + q + " " + c.Type
		if !c.Nullable {
			line += " NOT NULL"
		}
		if c.Default != "" {
			line += " DEFAULT " + c.Default
		}
		lines = append(lines, line)
	}
	if len(d.PrimaryKey) > 0 {
		cols := make([]string, 0, len(d.PrimaryKey))
		for _, c := range d.PrimaryKey {
			q, _ := quoteIdent(driver, c)
			cols = append(cols, q)
		}
		lines = append(lines, "  PRIMARY KEY ("+strings.Join(cols, ", ")+")")
	}
	for _, fk := range d.ForeignKeys {
		local := make([]string, 0, len(fk.Columns))
		for _, c := range fk.Columns {
			q, _ := quoteIdent(driver, c)
			local = append(local, q)
		}
		refRel, _ := qualifiedName(driver, fk.RefSchema, fk.RefTable)
		ref := make([]string, 0, len(fk.RefColumns))
		for _, c := range fk.RefColumns {
			q, _ := quoteIdent(driver, c)
			ref = append(ref, q)
		}
		line := fmt.Sprintf("  FOREIGN KEY (%s) REFERENCES %s (%s)",
			strings.Join(local, ", "), refRel, strings.Join(ref, ", "))
		if fk.OnDelete != "" && fk.OnDelete != "NO ACTION" {
			line += " ON DELETE " + fk.OnDelete
		}
		if fk.OnUpdate != "" && fk.OnUpdate != "NO ACTION" {
			line += " ON UPDATE " + fk.OnUpdate
		}
		lines = append(lines, line)
	}
	b.WriteString(strings.Join(lines, ",\n"))
	b.WriteString("\n);")
	return b.String()
}
