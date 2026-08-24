package dbx

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// Searching a whole schema for a value is the query a developer types by hand
// most often and enjoys least: "this order id appears somewhere — which table?"
// Done properly it is a dozen ad-hoc SELECTs; done here it is one request.
//
// The cost of getting it wrong is a full scan of every table in the database at
// once, so this is bounded in three directions at the same time: how many
// tables are visited, how many columns of each are compared, and how many
// matches come back per table. Those bounds are not tuning knobs — they are
// what makes the feature safe to offer on a production server at all.

// SearchMatch is one row somewhere that contains the value.
type SearchMatch struct {
	Schema string `json:"schema"`
	Table  string `json:"table"`
	Column string `json:"column"`
	// Value is the matched cell, truncated for display.
	Value string `json:"value"`
	// Row is the whole matching row, so the result is useful without a second
	// trip to go and look at it.
	Row map[string]any `json:"row"`
}

type SearchResult struct {
	Matches []SearchMatch `json:"matches"`
	// Scanned and Skipped say how much of the schema was actually looked at, so
	// "no matches" can be told apart from "gave up before reaching it".
	Scanned   int      `json:"tablesScanned"`
	Skipped   []string `json:"tablesSkipped,omitempty"`
	Truncated bool     `json:"truncated"`
}

const (
	searchMaxTables       = 60
	searchMaxColumns      = 40
	searchMaxPerTable     = 5
	searchMaxTotalMatches = 200
)

// Search looks for a value across every table in a schema.
//
// Comparison is a case-insensitive substring against the text form of each
// column, which is why every column is cast first: an integer id and a uuid are
// exactly the values people search for, and `LIKE` against them is an error on
// the stricter engines rather than a coercion.
func Search(ctx context.Context, db *sql.DB, driver Driver, schema, needle string) (*SearchResult, error) {
	if strings.TrimSpace(needle) == "" {
		return nil, fmt.Errorf("a value to search for is required")
	}
	d, err := DialectFor(driver)
	if err != nil {
		return nil, err
	}
	tables, err := d.Tables(ctx, db, schema)
	if err != nil {
		return nil, err
	}

	out := &SearchResult{Matches: []SearchMatch{}, Skipped: []string{}}
	pattern := "%" + needle + "%"

	for _, t := range tables {
		if out.Scanned >= searchMaxTables || len(out.Matches) >= searchMaxTotalMatches {
			out.Truncated = true
			break
		}
		// Views are skipped: they can be arbitrarily expensive to materialise,
		// and the rows behind them are in the tables being searched anyway.
		if strings.EqualFold(t.Type, "view") {
			continue
		}
		cols, err := d.Columns(ctx, db, t.Schema, t.Name)
		if err != nil || len(cols) == 0 {
			out.Skipped = append(out.Skipped, t.Name)
			continue
		}
		matches, err := searchTable(ctx, db, d, t, cols, pattern)
		if err != nil {
			// One unreadable table must not end the search — a permission error
			// on an audit table is normal and the other forty still matter.
			out.Skipped = append(out.Skipped, t.Name)
			continue
		}
		out.Scanned++
		out.Matches = append(out.Matches, matches...)
	}
	if len(out.Matches) > searchMaxTotalMatches {
		out.Matches = out.Matches[:searchMaxTotalMatches]
		out.Truncated = true
	}
	return out, nil
}

func searchTable(ctx context.Context, db *sql.DB, d Dialect, t Table, cols []Column, pattern string) ([]SearchMatch, error) {
	rel, err := qualify(d, t.Schema, t.Name)
	if err != nil {
		return nil, err
	}
	if len(cols) > searchMaxColumns {
		cols = cols[:searchMaxColumns]
	}

	preds := make([]string, 0, len(cols))
	args := make([]any, 0, len(cols))
	for i, c := range cols {
		q, err := d.QuoteIdent(c.Name)
		if err != nil {
			continue
		}
		preds = append(preds, fmt.Sprintf("%s LIKE %s", d.CastText(q), d.Placeholder(i+1)))
		args = append(args, pattern)
	}
	if len(preds) == 0 {
		return nil, fmt.Errorf("no comparable columns")
	}

	tail, tailArgs := d.Paginate(searchMaxPerTable, 0, len(args)+1)
	args = append(args, tailArgs...)
	query := fmt.Sprintf("SELECT * FROM %s WHERE %s %s", rel, strings.Join(preds, " OR "), tail)

	res, err := RunQuery(ctx, db, query, searchMaxPerTable, args...)
	if err != nil {
		return nil, err
	}

	out := make([]SearchMatch, 0, res.RowCount)
	lower := strings.ToLower(strings.Trim(pattern, "%"))
	for _, row := range res.Rows {
		rec := map[string]any{}
		for i, c := range res.Columns {
			rec[c] = row[i]
		}
		// Report which column actually matched rather than just the row: the
		// question was "where does this value live", and naming the table
		// without naming the column only half answers it.
		column, value := "", ""
		for i, c := range res.Columns {
			s := cellText(row[i])
			if s != "" && strings.Contains(strings.ToLower(s), lower) {
				column, value = c, s
				break
			}
		}
		if len(value) > 200 {
			value = value[:200] + "…"
		}
		out = append(out, SearchMatch{
			Schema: t.Schema, Table: t.Name, Column: column, Value: value, Row: rec,
		})
	}
	return out, nil
}

func cellText(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	default:
		return fmt.Sprint(t)
	}
}
