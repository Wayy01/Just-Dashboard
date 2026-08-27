package dbx

import (
	"context"
	"database/sql"
	"sort"
)

// Storage is what a schema costs on disk, table by table.
//
// This answers the question that arrives at the worst possible moment: the disk
// is filling and nobody knows which table did it. Every engine here can be
// asked, and none of them can be asked the same way — so this is the question
// in the dashboard's own shape, with each dialect supplying its catalogue query.
//
// Row counts are the engine's own estimate, not COUNT(*). That is deliberate:
// counting forty tables exactly is a full scan of the database to answer a
// question about relative size, and the estimate is off by a percent on the
// only thing this view is for — which table is the big one.
type TableSize struct {
	Schema string `json:"schema"`
	Table  string `json:"table"`
	// Rows is an estimate. The UI says so, because a figure that is nearly
	// right and presented as exact is worse than one presented as nearly right.
	Rows int64 `json:"rows"`
	// Bytes is everything the table costs — data, indexes, toast, the lot.
	Bytes      int64 `json:"bytes"`
	DataBytes  int64 `json:"dataBytes"`
	IndexBytes int64 `json:"indexBytes"`
}

// Overview is the whole storage-and-health answer in one request, because it is
// read as one screen and three round trips to draw one panel is three chances
// for it to be half drawn.
type Overview struct {
	Schema     string      `json:"schema"`
	Tables     []TableSize `json:"tables"`
	TotalBytes int64       `json:"totalBytes"`
	TotalRows  int64       `json:"totalRows"`
	TableCount int         `json:"tableCount"`
	// SizesKnown is false where the engine cannot report per-table bytes at
	// all. The UI then shows row counts and says the sizes are unavailable,
	// rather than drawing a bar chart of zeroes that reads as an empty database.
	SizesKnown bool      `json:"sizesKnown"`
	Pool       PoolStats `json:"pool"`
}

// StorageOverview reports per-table size for a schema plus the pool's state.
func StorageOverview(ctx context.Context, db *sql.DB, driver Driver, schema string) (*Overview, error) {
	d, err := DialectFor(driver)
	if err != nil {
		return nil, err
	}
	if schema == "" {
		schema = d.DefaultSchema()
	}
	sizes, err := d.TableSizes(ctx, db, schema)
	if err != nil {
		return nil, err
	}
	out := &Overview{Schema: schema, Tables: sizes, TableCount: len(sizes)}
	for _, t := range sizes {
		out.TotalBytes += t.Bytes
		out.TotalRows += t.Rows
		if t.Bytes > 0 {
			out.SizesKnown = true
		}
	}
	// Biggest first: the list exists to be read from the top and abandoned
	// after three rows.
	sort.SliceStable(out.Tables, func(i, j int) bool {
		if out.Tables[i].Bytes != out.Tables[j].Bytes {
			return out.Tables[i].Bytes > out.Tables[j].Bytes
		}
		return out.Tables[i].Rows > out.Tables[j].Rows
	})
	// The pool's state belongs next to the size breakdown because the two
	// failures look identical from the application's side — "the database is
	// slow" — and have nothing to do with each other. Waiting on a connection
	// is a pool that is too small; the server itself being busy is what the
	// activity list is for.
	st := db.Stats()
	out.Pool = PoolStats{
		Open: st.OpenConnections, InUse: st.InUse, Idle: st.Idle,
		WaitCount: st.WaitCount, WaitDuration: st.WaitDuration.String(),
		MaxOpen: st.MaxOpenConnections, MaxIdleClosed: st.MaxIdleClosed,
		MaxLifetimeGone: st.MaxLifetimeClosed,
	}
	return out, nil
}

// scanSizes reads the six columns every dialect's size query selects, in the
// one order, so adding an engine is a query and not a scan loop.
func scanSizes(rows *sql.Rows, schema string) ([]TableSize, error) {
	defer rows.Close()
	out := []TableSize{}
	for rows.Next() {
		var t TableSize
		if err := rows.Scan(nullText{&t.Schema}, &t.Table, &t.Rows, &t.Bytes, &t.DataBytes, &t.IndexBytes); err != nil {
			return nil, err
		}
		if t.Schema == "" {
			t.Schema = schema
		}
		out = append(out, t)
	}
	return out, rows.Err()
}
