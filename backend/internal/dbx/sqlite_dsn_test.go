package dbx

import "testing"

// The three forms the SQLite driver accepts all have to yield the same path,
// or the containment check above this has a documented way round it.
func TestSQLiteDSNPath(t *testing.T) {
	cases := []struct{ dsn, path, rest string }{
		{"/var/lib/app/data.db", "/var/lib/app/data.db", ""},
		{"/var/lib/app/data.db?mode=ro", "/var/lib/app/data.db", "?mode=ro"},
		{"file:/var/lib/app/data.db", "/var/lib/app/data.db", ""},
		{"file:/var/lib/app/data.db?_pragma=foreign_keys(1)", "/var/lib/app/data.db", "?_pragma=foreign_keys(1)"},
		{"../../etc/passwd", "../../etc/passwd", ""},
		{"", "", ""},
	}
	for _, c := range cases {
		path, rest := SQLiteDSNPath(c.dsn)
		if path != c.path || rest != c.rest {
			t.Errorf("SQLiteDSNPath(%q) = %q, %q; want %q, %q", c.dsn, path, rest, c.path, c.rest)
		}
		// Round-tripping the path it just reported must not change the DSN.
		if got := SQLiteDSNWithPath(c.dsn, path); got != c.dsn {
			t.Errorf("SQLiteDSNWithPath(%q, %q) = %q, want unchanged", c.dsn, path, got)
		}
	}
	if got := SQLiteDSNWithPath("file:/tmp/a.db?mode=ro", "/roots/a.db"); got != "file:/roots/a.db?mode=ro" {
		t.Errorf("replacement dropped a form: %q", got)
	}
}
