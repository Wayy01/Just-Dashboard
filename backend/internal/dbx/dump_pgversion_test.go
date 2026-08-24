package dbx

import (
	"os"
	"path/filepath"
	"testing"
)

// PostgreSQL refuses to dump a server newer than the client — "aborting
// because of server version mismatch" — so which pg_dump is chosen decides
// which servers can be backed up at all. This is the logic that chooses.
func TestPostgresToolPicksForTheServer(t *testing.T) {
	root := t.TempDir()
	for _, major := range []string{"15", "16", "17"} {
		bin := filepath.Join(root, major, "bin")
		if err := os.MkdirAll(bin, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(bin, "pg_dump"), []byte("#!/bin/true\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	// A directory with no pg_dump in it must not be offered.
	if err := os.MkdirAll(filepath.Join(root, "14", "bin"), 0o755); err != nil {
		t.Fatal(err)
	}

	old := pgBinDirs
	pgBinDirs = []string{root}
	t.Cleanup(func() { pgBinDirs = old })

	cases := []struct {
		name   string
		server int
		want   string
	}{
		{"exact match is preferred", 16, filepath.Join(root, "16", "bin", "pg_dump")},
		{"exact match, oldest", 15, filepath.Join(root, "15", "bin", "pg_dump")},
		{"unknown server takes the newest", 0, filepath.Join(root, "17", "bin", "pg_dump")},
		{"a version with no binary falls back to the newest", 14, filepath.Join(root, "17", "bin", "pg_dump")},
		{"a server newer than anything installed takes the newest", 18, filepath.Join(root, "17", "bin", "pg_dump")},
	}
	for _, c := range cases {
		if got := postgresTool("pg_dump", c.server); got != c.want {
			t.Errorf("%s: postgresTool(%d) = %q, want %q", c.name, c.server, got, c.want)
		}
	}
}

// A machine that installs the tools somewhere this does not know about keeps
// working exactly as it did before: the bare name resolves through PATH.
func TestPostgresToolFallsBackToPath(t *testing.T) {
	old := pgBinDirs
	pgBinDirs = []string{t.TempDir()}
	t.Cleanup(func() { pgBinDirs = old })
	if got := postgresTool("pg_dump", 16); got != "pg_dump" {
		t.Errorf("postgresTool with nothing installed = %q, want the bare name", got)
	}
}
