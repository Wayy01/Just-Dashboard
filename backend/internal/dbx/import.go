package dbx

import (
	"context"
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// Importing is the mirror of exporting, and the asymmetry between them is the
// interesting part: an export cannot fail halfway in a way that matters, and an
// import can. Half-loaded data is worse than none, because the operator cannot
// tell by looking which half arrived.
//
// So the whole load runs inside one transaction and either commits or rolls
// back. The cost is that a very large import holds a transaction open; the
// alternative — committing in batches — turns a failed import into a
// reconciliation job, which is not a trade a dashboard should make on the
// operator's behalf.

// ImportOptions describes how to read the incoming data and where to put it.
type ImportOptions struct {
	Schema string
	Table  string
	// Columns names the target columns in file order. Empty means "use the
	// CSV header", which is the common case; a JSON import ignores it and uses
	// each object's own keys.
	Columns []string
	// HasHeader treats the first CSV record as names rather than data.
	HasHeader bool
	// Truncate empties the table first. It is a separate, explicitly requested
	// step rather than an implied part of "import", because replacing a table's
	// contents and adding to them are different intentions.
	Truncate bool
	// StopOnError aborts at the first bad row. With it off, bad rows are
	// counted and reported and the good ones still land — but only if the
	// whole transaction commits, so "skipped" never means "partially applied".
	StopOnError bool
	// NullAs is the literal that means SQL NULL rather than an empty string.
	// CSV cannot distinguish the two, so the caller says which it meant.
	NullAs string
}

// ImportResult reports what happened, with a bounded sample of the failures —
// a file with fifty thousand bad rows should not answer with fifty thousand
// error strings.
type ImportResult struct {
	Inserted  int      `json:"inserted"`
	Failed    int      `json:"failed"`
	Errors    []string `json:"errors"`
	Truncated bool     `json:"errorsTruncated"`
	Statement string   `json:"statement"`
}

const maxImportErrors = 20

// ImportCSV loads delimited data into a table.
func ImportCSV(ctx context.Context, db *sql.DB, driver Driver, r io.Reader, opts ImportOptions) (*ImportResult, error) {
	cr := csv.NewReader(r)
	// Rows in real exports are ragged; deciding per row is more useful than
	// refusing the file outright, and a short row is reported as a row error.
	cr.FieldsPerRecord = -1

	cols := opts.Columns
	if opts.HasHeader {
		header, err := cr.Read()
		if err != nil {
			return nil, fmt.Errorf("could not read the header row: %w", err)
		}
		if len(cols) == 0 {
			cols = make([]string, 0, len(header))
			for _, h := range header {
				cols = append(cols, strings.TrimSpace(strings.TrimPrefix(h, "\ufeff")))
			}
		}
	}
	if len(cols) == 0 {
		return nil, fmt.Errorf("no target columns: either include a header row or name the columns")
	}

	rows := func(yield func([]any, error) bool) {
		for {
			rec, err := cr.Read()
			if err == io.EOF {
				return
			}
			if err != nil {
				if !yield(nil, err) {
					return
				}
				continue
			}
			if len(rec) != len(cols) {
				if !yield(nil, fmt.Errorf("row has %d fields, expected %d", len(rec), len(cols))) {
					return
				}
				continue
			}
			vals := make([]any, len(rec))
			for i, f := range rec {
				if opts.NullAs != "" && f == opts.NullAs {
					vals[i] = nil
					continue
				}
				vals[i] = f
			}
			if !yield(vals, nil) {
				return
			}
		}
	}
	return runImport(ctx, db, driver, cols, rows, opts)
}

// ImportJSON loads an array of objects. Each object's keys are matched against
// the column list, so a file whose objects carry extra keys still loads and the
// extras are ignored rather than failing the row.
func ImportJSON(ctx context.Context, db *sql.DB, driver Driver, r io.Reader, opts ImportOptions) (*ImportResult, error) {
	var docs []map[string]any
	if err := json.NewDecoder(r).Decode(&docs); err != nil {
		return nil, fmt.Errorf("could not parse JSON: expected an array of objects (%w)", err)
	}
	cols := opts.Columns
	if len(cols) == 0 {
		// The union of keys across every object, so a sparse file does not lose
		// the columns that only later rows carry.
		seen := map[string]bool{}
		for _, d := range docs {
			for k := range d {
				if !seen[k] {
					seen[k] = true
					cols = append(cols, k)
				}
			}
		}
		sortStrings(cols)
	}
	if len(cols) == 0 {
		return nil, fmt.Errorf("the file contained no objects to import")
	}

	rows := func(yield func([]any, error) bool) {
		for _, doc := range docs {
			vals := make([]any, len(cols))
			for i, c := range cols {
				v, ok := doc[c]
				if !ok {
					vals[i] = nil
					continue
				}
				// Nested structures cannot be bound as a scalar, so they go in
				// as their JSON text — which is what a json/jsonb column wants
				// anyway and what a text column can at least hold.
				switch v.(type) {
				case map[string]any, []any:
					b, _ := json.Marshal(v)
					vals[i] = string(b)
				default:
					vals[i] = v
				}
			}
			if !yield(vals, nil) {
				return
			}
		}
	}
	return runImport(ctx, db, driver, cols, rows, opts)
}

// runImport is the shared transactional load. The row source is a push
// iterator so CSV can stream a file of any size while JSON hands over a slice
// it has already parsed, without either one growing a second copy of the load.
func runImport(
	ctx context.Context,
	db *sql.DB,
	driver Driver,
	cols []string,
	rows func(func([]any, error) bool),
	opts ImportOptions,
) (*ImportResult, error) {
	d, err := DialectFor(driver)
	if err != nil {
		return nil, err
	}
	stmt, err := buildInsert(d, opts.Schema, opts.Table, cols, false)
	if err != nil {
		return nil, err
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	// Rollback on any path that does not reach Commit. It is a no-op after a
	// successful commit, so this is safe to defer unconditionally.
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	if opts.Truncate {
		rel, err := qualify(d, opts.Schema, opts.Table)
		if err != nil {
			return nil, err
		}
		form := "TRUNCATE TABLE " + rel
		if driver == DriverSQLite {
			form = "DELETE FROM " + rel
		}
		if _, err := tx.ExecContext(ctx, form); err != nil {
			return nil, fmt.Errorf("could not empty the table first: %w", err)
		}
	}

	prepared, err := tx.PrepareContext(ctx, stmt)
	if err != nil {
		return nil, fmt.Errorf("could not prepare the insert: %w", err)
	}
	defer prepared.Close()

	res := &ImportResult{Errors: []string{}, Statement: stmt}
	line := 0
	var fatal error

	rows(func(vals []any, rowErr error) bool {
		line++
		if rowErr == nil {
			if _, err := prepared.ExecContext(ctx, vals...); err != nil {
				rowErr = err
			}
		}
		if rowErr != nil {
			res.Failed++
			if len(res.Errors) < maxImportErrors {
				res.Errors = append(res.Errors, fmt.Sprintf("row %d: %v", line, rowErr))
			} else {
				res.Truncated = true
			}
			if opts.StopOnError {
				fatal = fmt.Errorf("row %d: %w", line, rowErr)
				return false
			}
			return true
		}
		res.Inserted++
		return true
	})

	if fatal != nil {
		return nil, fatal
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	committed = true
	return res, nil
}

// sortStrings is sort.Strings without pulling the import into this file's
// dependency set twice over.
func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

// ParseImportPreview reads the first few records of a CSV so the UI can show
// the operator what it thinks the columns are before anything is written.
func ParseImportPreview(r io.Reader, hasHeader bool, limit int) ([]string, [][]string, error) {
	if limit <= 0 || limit > 50 {
		limit = 10
	}
	cr := csv.NewReader(r)
	cr.FieldsPerRecord = -1
	var header []string
	if hasHeader {
		rec, err := cr.Read()
		if err != nil {
			return nil, nil, err
		}
		for _, h := range rec {
			header = append(header, strings.TrimSpace(strings.TrimPrefix(h, "\ufeff")))
		}
	}
	sample := [][]string{}
	for len(sample) < limit {
		rec, err := cr.Read()
		if err != nil {
			break
		}
		sample = append(sample, rec)
	}
	if header == nil && len(sample) > 0 {
		// With no header the columns are positional; naming them by index gives
		// the mapping UI something to point at.
		for i := range sample[0] {
			header = append(header, "column "+strconv.Itoa(i+1))
		}
	}
	return header, sample, nil
}
