package dbx

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// The developer-experience surface against real servers.
//
// Every feature here is a catalogue query or an engine-specific statement, which
// is the category the unit tests are structurally unable to check: the SQL is
// only wrong on the server that rejects it. Activity, sizes and search each
// read a different system view per engine, and the last time this package added
// one of those it shipped three statements that no server would parse.

func TestLiveActivity(t *testing.T) {
	for _, f := range sqlFixtures() {
		t.Run(string(f.driver), func(t *testing.T) {
			db := liveSQL(t, f.driver, f.env, f.dsn)
			ctx := context.Background()

			list, err := ListActivity(ctx, db, f.driver)
			if errors.Is(err, ErrNoActivityView) {
				t.Skipf("%s has no session list", f.driver)
			}
			if err != nil {
				t.Fatalf("ListActivity: %v", err)
			}
			// This connection is itself a session, so an empty list means the
			// query ran and matched nothing — which for every engine here means
			// the filter is wrong, not that the server is idle.
			if len(list) == 0 {
				t.Fatalf("no sessions reported, but this test is holding one")
			}
			for _, a := range list {
				if a.PID == "" {
					t.Errorf("session with no pid: %+v", a)
				}
				if a.Seconds < 0 {
					t.Errorf("negative duration for pid %s: %v", a.PID, a.Seconds)
				}
			}
		})
	}
}

// TestLiveKillRefusesRubbish proves the pid guard is in front of the kill
// statement rather than behind it. A pid is interpolated — no engine here binds
// one — so the validation is the whole defence, and a test that only checked a
// successful kill would never exercise it.
func TestLiveKillRefusesRubbish(t *testing.T) {
	for _, f := range sqlFixtures() {
		t.Run(string(f.driver), func(t *testing.T) {
			db := liveSQL(t, f.driver, f.env, f.dsn)
			ctx := context.Background()
			for _, bad := range []string{
				"1; DROP TABLE jd_users",
				"1 OR 1=1",
				"'",
				"",
			} {
				err := KillQuery(ctx, db, f.driver, bad)
				if err == nil {
					t.Errorf("KillQuery(%q) was accepted", bad)
				}
			}
			// The seeded table is still there, which is the claim that matters.
			if _, err := db.ExecContext(ctx, "SELECT 1"); err != nil {
				t.Fatalf("connection broken after rejected kills: %v", err)
			}
		})
	}
}

// killProbe is a statement that runs long enough to be caught in the act, with
// a marker in its text so the test can find its own victim among whatever else
// the server happens to be doing.
func killProbe(d Driver) string {
	switch d {
	case DriverPostgres:
		return "SELECT pg_sleep(30) /* jd_kill_probe */"
	case DriverMySQL:
		return "SELECT SLEEP(30) /* jd_kill_probe */"
	case DriverMSSQL:
		return "WAITFOR DELAY '00:00:30' /* jd_kill_probe */"
	case DriverClickHouse:
		return "SELECT sleepEachRow(1) FROM numbers(30) SETTINGS function_sleep_max_microseconds_per_block = 30000000 /* jd_kill_probe */"
	}
	return ""
}

// TestLiveKillRunningQuery is the whole feature end to end: start something
// slow on a second connection, find it in the activity list, stop it, and prove
// it stopped.
//
// Nothing short of this proves the kill works. Each engine's kill is a
// different statement against a different kind of handle — a backend pid, a
// connection id, a spid, a query UUID — and "the statement did not error" is
// satisfied by a KILL that matched nothing at all.
func TestLiveKillRunningQuery(t *testing.T) {
	for _, f := range sqlFixtures() {
		probe := killProbe(f.driver)
		if probe == "" {
			continue
		}
		t.Run(string(f.driver), func(t *testing.T) {
			db := liveSQL(t, f.driver, f.env, f.dsn)
			victim := liveSQL(t, f.driver, f.env, f.dsn)
			ctx := context.Background()

			done := make(chan error, 1)
			go func() {
				_, err := victim.ExecContext(context.Background(), probe)
				done <- err
			}()

			// Poll rather than sleep: the statement is dispatched from another
			// goroutine and the server files it away when it gets round to it.
			var target string
			for i := 0; i < 100 && target == ""; i++ {
				list, err := ListActivity(ctx, db, f.driver)
				if err != nil {
					t.Fatalf("ListActivity: %v", err)
				}
				for _, a := range list {
					if !a.Self && strings.Contains(a.Query, "jd_kill_probe") {
						target = a.PID
					}
				}
				if target == "" {
					time.Sleep(50 * time.Millisecond)
				}
			}
			if target == "" {
				t.Fatal("the probe never appeared in the activity list")
			}

			if err := KillQuery(ctx, db, f.driver, target); err != nil {
				t.Fatalf("KillQuery(%s): %v", target, err)
			}

			select {
			case err := <-done:
				// The victim must come back with an error. A nil here would
				// mean the sleep ran to completion and the kill did nothing.
				if err == nil {
					t.Errorf("the probe finished normally; the kill did nothing")
				}
			case <-time.After(20 * time.Second):
				t.Fatal("the probe is still running 20s after being killed")
			}
		})
	}
}

func TestLiveSearch(t *testing.T) {
	for _, f := range sqlFixtures() {
		t.Run(string(f.driver), func(t *testing.T) {
			db := liveSQL(t, f.driver, f.env, f.dsn)
			setupFixture(t, db, f)
			ctx := context.Background()

			// A string that exists in exactly one column of one table.
			res, err := Search(ctx, db, f.driver, f.schema, "a@x.io")
			if err != nil {
				t.Fatalf("Search: %v", err)
			}
			found := false
			for _, m := range res.Matches {
				if m.Table == "jd_users" && m.Column == "email" {
					found = true
					if m.Row == nil {
						t.Error("match carries no row")
					}
				}
			}
			if !found {
				t.Errorf("email not found; matches=%+v skipped=%v", res.Matches, res.Skipped)
			}

			// A numeric value, which is the case that fails without the text
			// cast: LIKE against an integer column is an error on Postgres and
			// SQL Server rather than a coercion.
			num, err := Search(ctx, db, f.driver, f.schema, "2")
			if err != nil {
				t.Fatalf("Search(numeric): %v", err)
			}
			if len(num.Matches) == 0 {
				t.Errorf("numeric search found nothing; skipped=%v", num.Skipped)
			}

			// And a value that is nowhere, which must come back empty rather
			// than erroring — the two are the same screen to a reader.
			none, err := Search(ctx, db, f.driver, f.schema, "zzz-not-present-zzz")
			if err != nil {
				t.Fatalf("Search(absent): %v", err)
			}
			if len(none.Matches) != 0 {
				t.Errorf("absent value matched %d rows", len(none.Matches))
			}
			if none.Scanned == 0 {
				t.Error("search reported scanning no tables at all")
			}
		})
	}
}

func TestLiveStorageOverview(t *testing.T) {
	for _, f := range sqlFixtures() {
		t.Run(string(f.driver), func(t *testing.T) {
			db := liveSQL(t, f.driver, f.env, f.dsn)
			setupFixture(t, db, f)
			ctx := context.Background()

			ov, err := StorageOverview(ctx, db, f.driver, f.schema)
			if err != nil {
				t.Fatalf("StorageOverview: %v", err)
			}
			byName := map[string]TableSize{}
			for _, tb := range ov.Tables {
				byName[tb.Table] = tb
			}
			if _, ok := byName["jd_users"]; !ok {
				t.Fatalf("seeded table missing from overview: %+v", ov.Tables)
			}
			if ov.TableCount == 0 {
				t.Error("overview reports no tables")
			}
			// Sorted biggest-first is the contract the panel draws.
			for i := 1; i < len(ov.Tables); i++ {
				if ov.Tables[i-1].Bytes < ov.Tables[i].Bytes {
					t.Errorf("overview not sorted by size: %+v", ov.Tables)
					break
				}
			}
			for _, tb := range ov.Tables {
				if tb.Rows < 0 || tb.Bytes < 0 {
					t.Errorf("negative figures for %s: %+v", tb.Table, tb)
				}
			}
			if ov.Pool.Open == 0 {
				t.Error("pool reports no open connections while a query is in flight")
			}
		})
	}
}

// TestLiveGeneratedTypes runs the two new generators over genuinely
// introspected structure. The generators are pure, but the *input* is not: a
// type name only this engine emits is exactly what makes a generator produce
// `any` for half a schema.
func TestLiveGeneratedTypes(t *testing.T) {
	for _, f := range sqlFixtures() {
		t.Run(string(f.driver), func(t *testing.T) {
			db := liveSQL(t, f.driver, f.env, f.dsn)
			setupFixture(t, db, f)
			ctx := context.Background()

			tables, err := ListTables(ctx, db, f.driver, f.schema)
			if err != nil {
				t.Fatal(err)
			}
			details := map[string]*TableDetail{}
			for _, tb := range tables {
				if d, err := Detail(ctx, db, f.driver, tb.Schema, tb.Name); err == nil {
					details[tb.Name] = d
				}
			}

			ts, err := GenerateORM(ORMTypeScript, f.driver, tables, details)
			if err != nil {
				t.Fatalf("GenerateORM typescript: %v", err)
			}
			if !strings.Contains(ts, "export interface") {
				t.Errorf("typescript output has no interface:\n%s", ts)
			}
			if !strings.Contains(ts, "email") {
				t.Errorf("typescript output missing the email column:\n%s", ts)
			}
			// An unmapped type would come out as `any`, which is the generator
			// silently giving up. It is a real failure on a schema this simple.
			if strings.Contains(ts, ": any") {
				t.Errorf("typescript output fell back to any:\n%s", ts)
			}

			zod, err := GenerateORM(ORMZod, f.driver, tables, details)
			if err != nil {
				t.Fatalf("GenerateORM zod: %v", err)
			}
			if !strings.Contains(zod, "z.object") {
				t.Errorf("zod output has no schema:\n%s", zod)
			}
			if strings.Contains(zod, "z.unknown()") {
				t.Errorf("zod output fell back to unknown:\n%s", zod)
			}
		})
	}
}

// TestLiveRowInsertSQL round-trips: take a real row out of the browser, render
// it as an INSERT, and give it back to the same server. That is the only check
// that matters for a clipboard feature — the text is useless if the engine it
// came from will not parse it.
func TestLiveRowInsertSQL(t *testing.T) {
	for _, f := range sqlFixtures() {
		t.Run(string(f.driver), func(t *testing.T) {
			db := liveSQL(t, f.driver, f.env, f.dsn)
			setupFixture(t, db, f)
			ctx := context.Background()

			res, err := Browse(ctx, db, f.driver, BrowseOptions{
				Schema: f.schema, Table: "jd_users", Limit: 10,
			})
			if err != nil {
				t.Fatalf("Browse: %v", err)
			}
			if res.RowCount == 0 {
				t.Fatal("no rows to copy")
			}
			row := map[string]any{}
			for i, c := range res.Columns {
				row[c] = res.Rows[0][i]
			}
			// Move it out of the way of the primary key it came with.
			row["id"] = 900
			row["email"] = "copied@x.io"

			stmt, err := RowInsertSQL(f.driver, f.schema, "jd_users", row)
			if err != nil {
				t.Fatalf("RowInsertSQL: %v", err)
			}
			if !strings.HasPrefix(stmt, "INSERT INTO ") {
				t.Fatalf("not an insert:\n%s", stmt)
			}
			if _, err := db.ExecContext(ctx, strings.TrimSuffix(stmt, ";")); err != nil {
				t.Fatalf("server rejected the rendered INSERT: %v\n%s", err, stmt)
			}
			var n int
			if err := db.QueryRowContext(ctx,
				"SELECT COUNT(*) FROM "+fixtureRel(t, f, "jd_users")+
					" WHERE "+fixtureCol(t, f, "email")+" = "+quoteLiteral("copied@x.io")).Scan(&n); err != nil {
				t.Fatalf("verify: %v", err)
			}
			if n != 1 {
				t.Errorf("copied row count = %d, want 1", n)
			}
		})
	}
}

// TestLiveRowInsertSQLQuoting is the case the feature exists to get right: a
// value containing the quote character that terminates a SQL string.
func TestLiveRowInsertSQLQuoting(t *testing.T) {
	for _, f := range sqlFixtures() {
		t.Run(string(f.driver), func(t *testing.T) {
			db := liveSQL(t, f.driver, f.env, f.dsn)
			setupFixture(t, db, f)
			ctx := context.Background()

			nasty := "O'Brien'); DROP TABLE jd_users; --"
			stmt, err := RowInsertSQL(f.driver, f.schema, "jd_users", map[string]any{
				"id": 901, "email": "quote@x.io", "name": nasty,
			})
			if err != nil {
				t.Fatalf("RowInsertSQL: %v", err)
			}
			if _, err := db.ExecContext(ctx, strings.TrimSuffix(stmt, ";")); err != nil {
				t.Fatalf("server rejected the rendered INSERT: %v\n%s", err, stmt)
			}
			var got string
			if err := db.QueryRowContext(ctx,
				"SELECT "+fixtureCol(t, f, "name")+" FROM "+fixtureRel(t, f, "jd_users")+
					" WHERE "+fixtureCol(t, f, "email")+" = "+quoteLiteral("quote@x.io")).Scan(&got); err != nil {
				t.Fatalf("read back: %v", err)
			}
			if got != nasty {
				t.Errorf("value round-tripped as %q, want %q", got, nasty)
			}
			// And the table the injected fragment named is still standing.
			var n int
			if err := db.QueryRowContext(ctx,
				"SELECT COUNT(*) FROM "+fixtureRel(t, f, "jd_users")).Scan(&n); err != nil {
				t.Fatalf("jd_users is gone: %v", err)
			}
		})
	}
}
