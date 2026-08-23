package dbx

import (
	"errors"
	"strings"
	"testing"
)

// mustDialect resolves a dialect or fails the test, so the builder tests read
// as being about the SQL they produce rather than about error handling.
func mustDialect(t *testing.T, d Driver) Dialect {
	t.Helper()
	dl, err := DialectFor(d)
	if err != nil {
		t.Fatalf("DialectFor(%s): %v", d, err)
	}
	return dl
}

func TestBuildInsert(t *testing.T) {
	cases := []struct {
		driver    Driver
		cols      []string
		returning bool
		want      string
	}{
		{DriverPostgres, []string{"name", "email"}, true,
			`INSERT INTO "public"."users" ("name", "email") VALUES ($1, $2) RETURNING *`},
		{DriverMySQL, []string{"name", "email"}, true,
			"INSERT INTO `public`.`users` (`name`, `email`) VALUES (?, ?)"},
		{DriverSQLite, []string{"name"}, true,
			`INSERT INTO "users" ("name") VALUES (?) RETURNING *`},
	}
	for _, c := range cases {
		schema := "public"
		if c.driver == DriverSQLite {
			schema = ""
		}
		got, err := buildInsert(mustDialect(t, c.driver), schema, "users", c.cols, c.returning)
		if err != nil {
			t.Fatalf("buildInsert(%s): %v", c.driver, err)
		}
		if got != c.want {
			t.Errorf("buildInsert(%s) = %q, want %q", c.driver, got, c.want)
		}
	}
}

func TestBuildUpdate(t *testing.T) {
	got, err := buildUpdate(mustDialect(t, DriverPostgres), "public", "users",
		[]string{"name", "role"}, []string{"id"}, true)
	if err != nil {
		t.Fatal(err)
	}
	want := `UPDATE "public"."users" SET "name" = $1, "role" = $2 WHERE "id" = $3 RETURNING *`
	if got != want {
		t.Errorf("buildUpdate = %q, want %q", got, want)
	}

	// Placeholders must run continuously across SET and WHERE for MySQL too.
	gotMy, err := buildUpdate(mustDialect(t, DriverMySQL), "app", "users",
		[]string{"name"}, []string{"id", "tenant"}, false)
	if err != nil {
		t.Fatal(err)
	}
	wantMy := "UPDATE `app`.`users` SET `name` = ? WHERE `id` = ? AND `tenant` = ?"
	if gotMy != wantMy {
		t.Errorf("buildUpdate(mysql) = %q, want %q", gotMy, wantMy)
	}
}

func TestBuildUpdateRefusesNoKey(t *testing.T) {
	// An UPDATE with no WHERE columns would touch every row; it must be refused
	// before a statement is ever built.
	if _, err := buildUpdate(mustDialect(t, DriverPostgres), "public", "users", []string{"name"}, nil, false); !errors.Is(err, ErrNoPrimaryKey) {
		t.Errorf("buildUpdate with no key = %v, want ErrNoPrimaryKey", err)
	}
	if _, err := buildDelete(mustDialect(t, DriverPostgres), "public", "users", nil); !errors.Is(err, ErrNoPrimaryKey) {
		t.Errorf("buildDelete with no key = %v, want ErrNoPrimaryKey", err)
	}
}

func TestBuildDelete(t *testing.T) {
	got, err := buildDelete(mustDialect(t, DriverSQLite), "", "sessions", []string{"id"})
	if err != nil {
		t.Fatal(err)
	}
	want := `DELETE FROM "sessions" WHERE "id" = ?`
	if got != want {
		t.Errorf("buildDelete = %q, want %q", got, want)
	}
}

func TestBuildersRejectBadIdentifiers(t *testing.T) {
	// A column name that is not a plain identifier must be rejected rather than
	// interpolated — this is the injection guard for the form-driven edit path.
	if _, err := buildInsert(mustDialect(t, DriverPostgres), "public", "users", []string{"name\x00drop"}, false); err == nil {
		t.Error("buildInsert accepted a non-identifier column name")
	}
	if _, err := buildUpdate(mustDialect(t, DriverPostgres), "public", "users", []string{"ok"}, []string{"id\x00"}, false); err == nil {
		t.Error("buildUpdate accepted a non-identifier key column")
	}
}

func TestSortedKeysDeterministic(t *testing.T) {
	m := map[string]any{"c": 3, "a": 1, "b": 2}
	got := sortedKeys(m)
	want := []string{"a", "b", "c"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("sortedKeys = %v, want %v", got, want)
		}
	}
}

func TestQuotingEscapesAndAllowsRealNames(t *testing.T) {
	// A hyphenated or spaced table name is legal in every engine here and used
	// to be refused outright, which made such a table visible in the list and
	// impossible to open.
	got, err := buildDelete(mustDialect(t, DriverPostgres), "", "user-profiles", []string{"id"})
	if err != nil {
		t.Fatalf("hyphenated table name rejected: %v", err)
	}
	if !strings.Contains(got, `"user-profiles"`) {
		t.Errorf("expected quoted hyphenated name, got %q", got)
	}

	// A name carrying the quote character must have it doubled, not stripped or
	// passed through — that doubling is the whole escape.
	for _, tc := range []struct {
		driver     Driver
		name, want string
	}{
		{DriverPostgres, `we"ird`, `"we""ird"`},
		{DriverMySQL, "we`ird", "`we``ird`"},
		{DriverMSSQL, "we]ird", "[we]]ird]"},
	} {
		q, err := mustDialect(t, tc.driver).QuoteIdent(tc.name)
		if err != nil {
			t.Fatalf("QuoteIdent(%s, %q): %v", tc.driver, tc.name, err)
		}
		if q != tc.want {
			t.Errorf("QuoteIdent(%s, %q) = %q, want %q", tc.driver, tc.name, q, tc.want)
		}
	}
}
