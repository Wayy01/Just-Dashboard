package gitx

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A ref reaches an argument vector, so anything that could be read as an
// option or escape the repository has to be refused before it gets there.
func TestValidateRef(t *testing.T) {
	ok := []string{
		"main", "feature/login", "release-1.2.3", "v2.0", "user@host",
		"a_b.c-d", "origin/main", "HEAD", "abc123def",
	}
	for _, r := range ok {
		if err := ValidateRef(r); err != nil {
			t.Errorf("ValidateRef(%q) rejected a valid ref: %v", r, err)
		}
	}
	bad := []string{
		"", "--upload-pack=/bin/sh", "--exec=rm -rf /", "-x",
		"../../etc/passwd", "main..dev", "branch.lock",
		"has space", "semi;colon", "pipe|char", "dollar$sub", "back`tick",
		"quote'x", `dquote"x`, "new\nline", strings.Repeat("a", 256),
	}
	for _, r := range bad {
		if err := ValidateRef(r); err == nil {
			t.Errorf("ValidateRef(%q) accepted a ref it should refuse", r)
		}
	}
}

// The roots are the boundary an operator configured; a path outside them must
// be refused even when it really is a repository.
func TestResolveRejectsOutsideRoots(t *testing.T) {
	dir := t.TempDir()
	repo := filepath.Join(dir, "inside")
	mkRepo(t, repo)

	outside := t.TempDir()
	mkRepo(t, filepath.Join(outside, "other"))

	s := New([]string{dir})

	if _, err := s.Resolve(repo); err != nil {
		t.Errorf("repo inside a root was rejected: %v", err)
	}
	if _, err := s.Resolve(filepath.Join(outside, "other")); err == nil {
		t.Error("repo outside every root was accepted")
	}
	// A prefix match must not be a substring match: /tmp/xyz-evil is not
	// inside /tmp/xyz.
	sibling := dir + "-evil"
	if _, err := s.Resolve(sibling); err == nil {
		t.Error("a sibling directory sharing a name prefix was accepted")
	}
}

func TestResolveRejectsNonRepo(t *testing.T) {
	dir := t.TempDir()
	s := New([]string{dir})
	if _, err := s.Resolve(dir); err == nil {
		t.Error("a plain directory was accepted as a repository")
	}
}

// A remote may carry a token; it is rendered on a list page, so it is scrubbed.
func TestScrubRemote(t *testing.T) {
	cases := map[string]string{
		"https://user:ghp_secrettoken@github.com/a/b.git": "https://***@github.com/a/b.git",
		"https://github.com/a/b.git":                      "https://github.com/a/b.git",
		"git@github.com:a/b.git":                          "git@github.com:a/b.git",
		"":                                                "",
	}
	for in, want := range cases {
		if got := scrubRemote(in); got != want {
			t.Errorf("scrubRemote(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestTrackCount(t *testing.T) {
	cases := []struct {
		in, key string
		want    int
	}{
		{"[ahead 2, behind 1]", "ahead ", 2},
		{"[ahead 2, behind 1]", "behind ", 1},
		{"[ahead 12]", "ahead ", 12},
		{"[behind 3]", "ahead ", 0},
		{"", "ahead ", 0},
	}
	for _, c := range cases {
		if got := trackCount(c.in, c.key); got != c.want {
			t.Errorf("trackCount(%q,%q) = %d, want %d", c.in, c.key, got, c.want)
		}
	}
}

func mkRepo(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(path, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
}

// A local branch may contain a slash, so only the full refname distinguishes
// local from remote.
func TestBranchRemoteDetection(t *testing.T) {
	cases := map[string]bool{
		"refs/heads/main":               false,
		"refs/heads/fix/audit-findings": false,
		"refs/heads/feature/a/b":        false,
		"refs/remotes/origin/main":      true,
		"refs/remotes/upstream/dev":     true,
	}
	for refname, want := range cases {
		if got := strings.HasPrefix(refname, "refs/remotes/"); got != want {
			t.Errorf("%q classified remote=%v, want %v", refname, got, want)
		}
	}
}
