package dbx

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
)

// Row editing is the sharp edge of a data browser: it writes to the database on
// the operator's behalf from a form, not from typed SQL. Two rules make that
// safe and keep it out of the query classifier's hands entirely.
//
//  1. Column and table names are validated identifiers, quoted, never bound —
//     the same choke point BrowseTable uses. Values are always bound.
//  2. Every UPDATE and DELETE is scoped by the table's primary key. A mutation
//     with no key columns is refused rather than run, because an UPDATE or
//     DELETE the caller believes touches one row would otherwise touch all of
//     them. This is why the handlers fetch the primary key before editing and
//     fail when a table has none.

// ErrNoPrimaryKey is returned when an edit is attempted on a table the caller
// could not supply key columns for. The handler turns it into advice to use the
// Query tab, where an explicit WHERE clause is the operator's responsibility.
var ErrNoPrimaryKey = fmt.Errorf("table has no primary key, so a single row cannot be identified for editing")

// buildInsert renders an INSERT for the given (sorted) columns. returning asks
// for the inserted row back, which Postgres and SQLite support and MySQL does
// not.
func buildInsert(driver Driver, schema, table string, cols []string, returning bool) (string, error) {
	rel, err := qualifiedName(driver, schema, table)
	if err != nil {
		return "", err
	}
	qCols := make([]string, len(cols))
	marks := make([]string, len(cols))
	for i, c := range cols {
		q, err := quoteIdent(driver, c)
		if err != nil {
			return "", err
		}
		qCols[i] = q
		marks[i] = driver.placeholder(i + 1)
	}
	q := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)", rel,
		joinComma(qCols), joinComma(marks))
	if returning && driver != DriverMySQL {
		q += " RETURNING *"
	}
	return q, nil
}

// buildUpdate renders an UPDATE that sets setCols and is scoped by whereCols.
// Placeholders run continuously across the SET list and then the WHERE list, so
// the caller appends its args in the same order.
func buildUpdate(driver Driver, schema, table string, setCols, whereCols []string, returning bool) (string, error) {
	if len(whereCols) == 0 {
		return "", ErrNoPrimaryKey
	}
	rel, err := qualifiedName(driver, schema, table)
	if err != nil {
		return "", err
	}
	n := 0
	sets := make([]string, len(setCols))
	for i, c := range setCols {
		q, err := quoteIdent(driver, c)
		if err != nil {
			return "", err
		}
		n++
		sets[i] = q + " = " + driver.placeholder(n)
	}
	wheres := make([]string, len(whereCols))
	for i, c := range whereCols {
		q, err := quoteIdent(driver, c)
		if err != nil {
			return "", err
		}
		n++
		wheres[i] = q + " = " + driver.placeholder(n)
	}
	q := fmt.Sprintf("UPDATE %s SET %s WHERE %s", rel,
		joinComma(sets), joinAnd(wheres))
	if returning && driver != DriverMySQL {
		q += " RETURNING *"
	}
	return q, nil
}

// buildDelete renders a DELETE scoped by whereCols.
func buildDelete(driver Driver, schema, table string, whereCols []string) (string, error) {
	if len(whereCols) == 0 {
		return "", ErrNoPrimaryKey
	}
	rel, err := qualifiedName(driver, schema, table)
	if err != nil {
		return "", err
	}
	wheres := make([]string, len(whereCols))
	for i, c := range whereCols {
		q, err := quoteIdent(driver, c)
		if err != nil {
			return "", err
		}
		wheres[i] = q + " = " + driver.placeholder(i+1)
	}
	return fmt.Sprintf("DELETE FROM %s WHERE %s", rel, joinAnd(wheres)), nil
}

// sortedKeys gives the columns a stable order so the generated SQL and the
// argument slice cannot drift apart, and so a builder is deterministic to test.
func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// InsertRow inserts one row from a column→value map.
func InsertRow(ctx context.Context, db *sql.DB, driver Driver, schema, table string, values map[string]any) (*QueryResult, error) {
	if len(values) == 0 {
		return nil, fmt.Errorf("no column values supplied")
	}
	cols := sortedKeys(values)
	query, err := buildInsert(driver, schema, table, cols, true)
	if err != nil {
		return nil, err
	}
	args := make([]any, len(cols))
	for i, c := range cols {
		args[i] = values[c]
	}
	return RunQuery(ctx, db, query, 1000, args...)
}

// UpdateRow updates the row identified by key with the given values.
func UpdateRow(ctx context.Context, db *sql.DB, driver Driver, schema, table string, values, key map[string]any) (*QueryResult, error) {
	if len(values) == 0 {
		return nil, fmt.Errorf("no column values supplied")
	}
	if len(key) == 0 {
		return nil, ErrNoPrimaryKey
	}
	setCols := sortedKeys(values)
	whereCols := sortedKeys(key)
	query, err := buildUpdate(driver, schema, table, setCols, whereCols, true)
	if err != nil {
		return nil, err
	}
	args := make([]any, 0, len(setCols)+len(whereCols))
	for _, c := range setCols {
		args = append(args, values[c])
	}
	for _, c := range whereCols {
		args = append(args, key[c])
	}
	return RunQuery(ctx, db, query, 1000, args...)
}

// DeleteRow removes the row identified by key.
func DeleteRow(ctx context.Context, db *sql.DB, driver Driver, schema, table string, key map[string]any) (*QueryResult, error) {
	if len(key) == 0 {
		return nil, ErrNoPrimaryKey
	}
	whereCols := sortedKeys(key)
	query, err := buildDelete(driver, schema, table, whereCols)
	if err != nil {
		return nil, err
	}
	args := make([]any, len(whereCols))
	for i, c := range whereCols {
		args[i] = key[c]
	}
	return RunQuery(ctx, db, query, 1000, args...)
}

func joinComma(parts []string) string { return joinWith(parts, ", ") }
func joinAnd(parts []string) string   { return joinWith(parts, " AND ") }

func joinWith(parts []string, sep string) string {
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += sep
		}
		out += p
	}
	return out
}
