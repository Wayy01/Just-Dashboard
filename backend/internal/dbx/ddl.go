package dbx

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"strings"
)

// DDL is the one place in this package that assembles a statement whose
// *structure* the operator chose, rather than only its values. A CREATE TABLE
// cannot bind anything: the table name, the column names, the type names and
// the defaults are all syntax. So every fragment is validated before it is
// concatenated, and the validation is by pattern rather than by escaping —
// there is nothing to escape a type name into.
//
// The rules below are deliberately narrow. They accept what a create-table form
// can legitimately produce and nothing else; anything more exotic belongs in
// the Query tab, where the statement is classified, confirmed and audited as
// the arbitrary SQL it is. Refusing an unusual-but-valid type is a nuisance;
// accepting a crafted one is a shell on the database host.

// typeRe accepts a SQL type name with an optional parenthesised argument list:
// varchar(255), decimal(10,2), int unsigned, timestamp with time zone,
// enum('a','b'). It admits no semicolon, no comment introducer and no nested
// parenthesis, so a type cannot carry a second statement.
var typeRe = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_ ]{0,40}(\([A-Za-z0-9_,'. ]{0,120}\))?$`)

// defaultRe accepts a literal default: a number, a single-quoted string with no
// embedded quote, or one of the bare keywords engines share.
var defaultRe = regexp.MustCompile(`^(-?[0-9]+(\.[0-9]+)?|'[^']*'|NULL|TRUE|FALSE|CURRENT_TIMESTAMP|CURRENT_DATE|CURRENT_TIME|NOW\(\))$`)

// NewColumn is one column in a create-table or add-column request.
type NewColumn struct {
	Name       string `json:"name"`
	Type       string `json:"type"`
	NotNull    bool   `json:"notNull"`
	PrimaryKey bool   `json:"primaryKey"`
	Default    string `json:"default,omitempty"`
}

func validateType(t string) error {
	t = strings.TrimSpace(t)
	if t == "" {
		return fmt.Errorf("a column type is required")
	}
	if !typeRe.MatchString(t) {
		return fmt.Errorf("column type %q is not one this form can build; use the Query tab for it", t)
	}
	return nil
}

func validateDefault(v string) error {
	v = strings.TrimSpace(v)
	if v == "" {
		return nil
	}
	if !defaultRe.MatchString(strings.ToUpper(v)) && !defaultRe.MatchString(v) {
		return fmt.Errorf("default %q is not a plain literal; use the Query tab for an expression default", v)
	}
	return nil
}

// renderColumn produces one column definition line.
func renderColumn(d Dialect, c NewColumn, inline bool) (string, error) {
	q, err := d.QuoteIdent(c.Name)
	if err != nil {
		return "", err
	}
	if err := validateType(c.Type); err != nil {
		return "", err
	}
	if err := validateDefault(c.Default); err != nil {
		return "", err
	}
	line := q + " " + strings.TrimSpace(c.Type)
	if c.Default != "" {
		line += " DEFAULT " + strings.TrimSpace(c.Default)
	}
	if c.NotNull {
		line += " NOT NULL"
	}
	// A single-column primary key is written inline; a composite one becomes a
	// table constraint, which is why CreateTable collects them instead.
	if inline && c.PrimaryKey {
		line += " PRIMARY KEY"
	}
	return line, nil
}

// CreateTableSQL renders the statement without running it, which is what lets
// the UI show the operator exactly what they are about to execute.
func CreateTableSQL(driver Driver, schema, table string, cols []NewColumn) (string, error) {
	d, err := DialectFor(driver)
	if err != nil {
		return "", err
	}
	if !d.SupportsDDL() {
		return "", fmt.Errorf("%s tables need an engine and sorting key that this form cannot choose for you; create them from the Query tab", driver)
	}
	if len(cols) == 0 {
		return "", fmt.Errorf("a table needs at least one column")
	}
	rel, err := qualify(d, schema, table)
	if err != nil {
		return "", err
	}
	pks := []string{}
	for _, c := range cols {
		if c.PrimaryKey {
			q, err := d.QuoteIdent(c.Name)
			if err != nil {
				return "", err
			}
			pks = append(pks, q)
		}
	}
	lines := make([]string, 0, len(cols)+1)
	for _, c := range cols {
		line, err := renderColumn(d, c, len(pks) == 1)
		if err != nil {
			return "", err
		}
		lines = append(lines, "  "+line)
	}
	if len(pks) > 1 {
		lines = append(lines, "  PRIMARY KEY ("+strings.Join(pks, ", ")+")")
	}
	return fmt.Sprintf("CREATE TABLE %s (\n%s\n)", rel, strings.Join(lines, ",\n")), nil
}

// CreateTable renders and executes.
func CreateTable(ctx context.Context, db *sql.DB, driver Driver, schema, table string, cols []NewColumn) (string, error) {
	stmt, err := CreateTableSQL(driver, schema, table, cols)
	if err != nil {
		return "", err
	}
	if _, err := db.ExecContext(ctx, stmt); err != nil {
		return stmt, err
	}
	return stmt, nil
}

// DropTable removes a table. The handler in front of this demands the
// destructive capability and a typed confirmation naming the table, in the same
// way DROP through the query runner does — the route being different must not
// make the guard weaker.
func DropTable(ctx context.Context, db *sql.DB, driver Driver, schema, table string) (string, error) {
	return execRelStatement(ctx, db, driver, schema, table, "DROP TABLE %s")
}

// TruncateTable empties a table. SQLite has no TRUNCATE and optimises an
// unqualified DELETE into the same thing, so it gets that instead of an error.
func TruncateTable(ctx context.Context, db *sql.DB, driver Driver, schema, table string) (string, error) {
	form := "TRUNCATE TABLE %s"
	if driver == DriverSQLite {
		form = "DELETE FROM %s"
	}
	return execRelStatement(ctx, db, driver, schema, table, form)
}

func execRelStatement(ctx context.Context, db *sql.DB, driver Driver, schema, table, form string) (string, error) {
	d, err := DialectFor(driver)
	if err != nil {
		return "", err
	}
	rel, err := qualify(d, schema, table)
	if err != nil {
		return "", err
	}
	stmt := fmt.Sprintf(form, rel)
	if _, err := db.ExecContext(ctx, stmt); err != nil {
		return stmt, err
	}
	return stmt, nil
}

// AddColumn appends a column to an existing table.
func AddColumn(ctx context.Context, db *sql.DB, driver Driver, schema, table string, col NewColumn) (string, error) {
	d, err := DialectFor(driver)
	if err != nil {
		return "", err
	}
	rel, err := qualify(d, schema, table)
	if err != nil {
		return "", err
	}
	// A column added to a table that already has rows cannot be NOT NULL without
	// a default — every existing row would violate it. Saying so here is more
	// use than relaying whichever way the engine phrases that.
	if col.NotNull && col.Default == "" {
		return "", fmt.Errorf("a NOT NULL column added to an existing table needs a default, or every existing row would violate it")
	}
	def, err := renderColumn(d, col, false)
	if err != nil {
		return "", err
	}
	stmt := fmt.Sprintf("ALTER TABLE %s %s %s", rel, d.AddColumnKeyword(), def)
	if _, err := db.ExecContext(ctx, stmt); err != nil {
		return stmt, err
	}
	return stmt, nil
}

// DropColumn removes a column and everything in it.
func DropColumn(ctx context.Context, db *sql.DB, driver Driver, schema, table, column string) (string, error) {
	d, err := DialectFor(driver)
	if err != nil {
		return "", err
	}
	rel, err := qualify(d, schema, table)
	if err != nil {
		return "", err
	}
	col, err := d.QuoteIdent(column)
	if err != nil {
		return "", err
	}
	// Some engines hold the column hostage behind an object they created
	// themselves; clearing that is part of the same action, not a separate one.
	if err := d.BeforeDropColumn(ctx, db, schema, table, column); err != nil {
		return "", fmt.Errorf("could not clear what depends on %s first: %w", column, err)
	}
	stmt := fmt.Sprintf("ALTER TABLE %s DROP COLUMN %s", rel, col)
	if _, err := db.ExecContext(ctx, stmt); err != nil {
		return stmt, err
	}
	return stmt, nil
}

// RenameColumn renames a column. Every engine here spells this the same way
// except SQL Server, which has no ALTER ... RENAME COLUMN at all and does it
// through a system stored procedure.
func RenameColumn(ctx context.Context, db *sql.DB, driver Driver, schema, table, from, to string) (string, error) {
	d, err := DialectFor(driver)
	if err != nil {
		return "", err
	}
	rel, err := qualify(d, schema, table)
	if err != nil {
		return "", err
	}
	qFrom, err := d.QuoteIdent(from)
	if err != nil {
		return "", err
	}
	qTo, err := d.QuoteIdent(to)
	if err != nil {
		return "", err
	}
	var stmt string
	var args []any
	if driver == DriverMSSQL {
		// sp_rename takes its arguments as bound strings, so the identifiers go
		// as parameters here rather than into the statement text.
		stmt = "EXEC sp_rename @p1, @p2, 'COLUMN'"
		args = []any{fmt.Sprintf("%s.%s", table, from), to}
		if schema != "" {
			args[0] = fmt.Sprintf("%s.%s.%s", schema, table, from)
		}
	} else {
		stmt = fmt.Sprintf("ALTER TABLE %s RENAME COLUMN %s TO %s", rel, qFrom, qTo)
	}
	if _, err := db.ExecContext(ctx, stmt, args...); err != nil {
		return stmt, err
	}
	return stmt, nil
}

// RenameTable renames a table.
func RenameTable(ctx context.Context, db *sql.DB, driver Driver, schema, table, to string) (string, error) {
	d, err := DialectFor(driver)
	if err != nil {
		return "", err
	}
	rel, err := qualify(d, schema, table)
	if err != nil {
		return "", err
	}
	qTo, err := d.QuoteIdent(to)
	if err != nil {
		return "", err
	}
	var stmt string
	var args []any
	switch driver {
	case DriverMSSQL:
		stmt = "EXEC sp_rename @p1, @p2"
		src := table
		if schema != "" {
			src = schema + "." + table
		}
		args = []any{src, to}
	case DriverMySQL:
		stmt = fmt.Sprintf("RENAME TABLE %s TO %s", rel, qTo)
	default:
		stmt = fmt.Sprintf("ALTER TABLE %s RENAME TO %s", rel, qTo)
	}
	if _, err := db.ExecContext(ctx, stmt, args...); err != nil {
		return stmt, err
	}
	return stmt, nil
}

// CreateIndex builds an index over one or more columns.
func CreateIndex(ctx context.Context, db *sql.DB, driver Driver, schema, table, name string, columns []string, unique bool) (string, error) {
	d, err := DialectFor(driver)
	if err != nil {
		return "", err
	}
	if len(columns) == 0 {
		return "", fmt.Errorf("an index needs at least one column")
	}
	rel, err := qualify(d, schema, table)
	if err != nil {
		return "", err
	}
	qName, err := d.QuoteIdent(name)
	if err != nil {
		return "", err
	}
	cols := make([]string, len(columns))
	for i, c := range columns {
		q, err := d.QuoteIdent(c)
		if err != nil {
			return "", err
		}
		cols[i] = q
	}
	kind := "INDEX"
	if unique {
		kind = "UNIQUE INDEX"
	}
	stmt := fmt.Sprintf("CREATE %s %s ON %s (%s)", kind, qName, rel, strings.Join(cols, ", "))
	if _, err := db.ExecContext(ctx, stmt); err != nil {
		return stmt, err
	}
	return stmt, nil
}

// DropIndex removes an index. MySQL and SQL Server scope the name to a table;
// Postgres, SQLite and Oracle treat it as a schema-level object.
func DropIndex(ctx context.Context, db *sql.DB, driver Driver, schema, table, name string) (string, error) {
	d, err := DialectFor(driver)
	if err != nil {
		return "", err
	}
	qName, err := d.QuoteIdent(name)
	if err != nil {
		return "", err
	}
	var stmt string
	switch driver {
	case DriverMySQL, DriverMSSQL:
		rel, err := qualify(d, schema, table)
		if err != nil {
			return "", err
		}
		if driver == DriverMySQL {
			stmt = fmt.Sprintf("DROP INDEX %s ON %s", qName, rel)
		} else {
			stmt = fmt.Sprintf("DROP INDEX %s ON %s", qName, rel)
		}
	default:
		rel := qName
		if schema != "" && driver != DriverSQLite {
			qSchema, err := d.QuoteIdent(schema)
			if err != nil {
				return "", err
			}
			rel = qSchema + "." + qName
		}
		stmt = "DROP INDEX " + rel
	}
	if _, err := db.ExecContext(ctx, stmt); err != nil {
		return stmt, err
	}
	return stmt, nil
}
