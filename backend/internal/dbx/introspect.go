package dbx

import (
	"context"
	"database/sql"
	"fmt"
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
// join, because the failure modes differ and because each engine answers them
// from a different catalogue.
//
// Only the column read is fatal. A table whose indexes cannot be listed because
// the login lacks a grant on the catalogue view is still a table worth showing:
// failing the whole request there would turn a partial answer into no answer,
// and the least-privileged accounts are exactly the ones a dashboard should
// stay usable for.
func Detail(ctx context.Context, db *sql.DB, driver Driver, schema, table string) (*TableDetail, error) {
	d, err := DialectFor(driver)
	if err != nil {
		return nil, err
	}
	cols, err := d.Columns(ctx, db, schema, table)
	if err != nil {
		return nil, err
	}
	detail := &TableDetail{
		Schema: schema, Name: table, Columns: cols,
		PrimaryKey: []string{}, Indexes: []Index{}, ForeignKeys: []ForeignKey{},
	}
	if pk, err := d.PrimaryKey(ctx, db, schema, table); err == nil {
		detail.PrimaryKey = pk
	}
	if ix, err := d.Indexes(ctx, db, schema, table); err == nil {
		detail.Indexes = ix
	}
	if fks, err := d.ForeignKeys(ctx, db, schema, table); err == nil {
		detail.ForeignKeys = fks
	}
	detail.CreateSQL, _ = d.CreateSQL(ctx, db, schema, table, detail)
	return detail, nil
}

// PrimaryKeyColumns returns the ordered primary-key columns. Row editing refuses
// to run without one, so this is a security-relevant answer as much as a
// descriptive one: it is what bounds an UPDATE or DELETE to a single row.
func PrimaryKeyColumns(ctx context.Context, db *sql.DB, driver Driver, schema, table string) ([]string, error) {
	d, err := DialectFor(driver)
	if err != nil {
		return nil, err
	}
	return d.PrimaryKey(ctx, db, schema, table)
}

// Relations returns every foreign key in a schema, which is what the entity
// diagram draws. It reads one table at a time rather than one catalogue-wide
// query because each dialect already knows how to ask for a table's keys, and a
// second per-engine query would be a second thing to get wrong.
func Relations(ctx context.Context, db *sql.DB, driver Driver, schema string) (map[string][]ForeignKey, error) {
	d, err := DialectFor(driver)
	if err != nil {
		return nil, err
	}
	tables, err := d.Tables(ctx, db, schema)
	if err != nil {
		return nil, err
	}
	out := map[string][]ForeignKey{}
	for _, t := range tables {
		if strings.EqualFold(t.Type, "view") {
			continue
		}
		fks, err := d.ForeignKeys(ctx, db, t.Schema, t.Name)
		if err != nil || len(fks) == 0 {
			continue
		}
		out[t.Name] = fks
	}
	return out, nil
}

// synthCreateTable builds a readable CREATE TABLE from an introspected detail,
// for the engines that keep no canonical DDL text of their own. It is a faithful
// summary rather than a byte-exact reproduction — types and defaults come back
// already normalised by the catalogue — so it is labelled as generated where it
// is shown.
func synthCreateTable(d Dialect, schema, table string, detail *TableDetail) string {
	rel, err := qualify(d, schema, table)
	if err != nil {
		rel = table
	}
	var b strings.Builder
	fmt.Fprintf(&b, "CREATE TABLE %s (\n", rel)
	lines := []string{}
	for _, c := range detail.Columns {
		q, err := d.QuoteIdent(c.Name)
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
	if len(detail.PrimaryKey) > 0 {
		cols := make([]string, 0, len(detail.PrimaryKey))
		for _, c := range detail.PrimaryKey {
			q, _ := d.QuoteIdent(c)
			cols = append(cols, q)
		}
		lines = append(lines, "  PRIMARY KEY ("+strings.Join(cols, ", ")+")")
	}
	for _, fk := range detail.ForeignKeys {
		local := make([]string, 0, len(fk.Columns))
		for _, c := range fk.Columns {
			q, _ := d.QuoteIdent(c)
			local = append(local, q)
		}
		refRel, _ := qualify(d, fk.RefSchema, fk.RefTable)
		ref := make([]string, 0, len(fk.RefColumns))
		for _, c := range fk.RefColumns {
			q, _ := d.QuoteIdent(c)
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

// SchemaOutline is the whole schema flattened to names, which is what the SQL
// editor's completion needs and all it needs.
//
// It is one payload rather than a completion endpoint the editor calls per
// keystroke: a round trip per character is unusable over the VPN tunnel these
// dashboards are reached through, and a schema's shape does not change between
// keystrokes. The cost is one request per table on open, which is why the
// result is small — names only, no types, no constraints.
type SchemaOutline struct {
	Schema string              `json:"schema"`
	Tables map[string][]string `json:"tables"`
}

func Outline(ctx context.Context, db *sql.DB, driver Driver, schema string) (*SchemaOutline, error) {
	d, err := DialectFor(driver)
	if err != nil {
		return nil, err
	}
	tables, err := d.Tables(ctx, db, schema)
	if err != nil {
		return nil, err
	}
	out := &SchemaOutline{Schema: schema, Tables: map[string][]string{}}
	for _, t := range tables {
		cols, err := d.Columns(ctx, db, t.Schema, t.Name)
		if err != nil {
			// A table whose columns cannot be read still belongs in the
			// completion list by name; dropping it would make the editor claim
			// it does not exist.
			out.Tables[t.Name] = []string{}
			continue
		}
		names := make([]string, 0, len(cols))
		for _, c := range cols {
			names = append(names, c.Name)
		}
		out.Tables[t.Name] = names
	}
	return out, nil
}
