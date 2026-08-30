package files

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The fuzzy finder is a ranking, and a ranking is the part that is easy to get
// subtly wrong and impossible to notice: every one of these queries returns
// results whichever way the scoring goes, and only the order says whether the
// feature works. So the tests pin the order rather than the presence.
func findFixture(t *testing.T) *Service {
	t.Helper()
	root := t.TempDir()
	for _, dir := range []string{
		"src/app/files", "src/components", "node_modules/left-pad",
		"etc/nginx/sites-available", ".git/objects", ".config",
	} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, f := range []string{
		"src/app/files/page.tsx",
		"src/components/file-row.tsx",
		"src/components/a-packaged-application.ts",
		"etc/nginx/nginx.conf",
		"etc/nginx/sites-available/example.conf",
		"node_modules/left-pad/index.js",
		".git/objects/deadbeef",
		".config/deploy-secrets.env",
		"README.md",
	} {
		if err := os.WriteFile(filepath.Join(root, f), []byte("x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return New([]string{root})
}

func find(t *testing.T, s *Service, query string, opts ...func(*FindOptions)) *FindResult {
	t.Helper()
	o := FindOptions{Root: s.Roots()[0], Query: query}
	for _, fn := range opts {
		fn(&o)
	}
	res, err := s.Find(context.Background(), o)
	if err != nil {
		t.Fatalf("Find(%q): %v", query, err)
	}
	return res
}

func relNames(res *FindResult) []string {
	out := make([]string, 0, len(res.Hits))
	for _, h := range res.Hits {
		out = append(out, h.Rel)
	}
	return out
}

func TestFindRanksTheNameAboveThePath(t *testing.T) {
	s := findFixture(t)
	res := find(t, s, "nginx")

	// nginx.conf is named for what was typed; sites-available/example.conf
	// merely lives under a directory that is. A finder that cannot tell those
	// apart is a finder people stop using after two queries. (The directory
	// called exactly "nginx" outranks both, which is also right: an exact name
	// is the strongest signal there is.)
	if rankOf(res, "etc/nginx/nginx.conf") >= rankOf(res, "etc/nginx/sites-available/example.conf") {
		t.Fatalf("a name match must outrank a path-only match, got %v", relNames(res))
	}
	if len(res.Hits) == 0 || res.Hits[0].Rel != "etc/nginx" {
		t.Fatalf("expected the exactly-named directory first, got %v", relNames(res))
	}
	if !containsRel(res, "etc/nginx/sites-available/example.conf") {
		t.Fatalf("a match on the directory part should still be a result: %v", relNames(res))
	}
}

func TestFindPrefersTheTightestRun(t *testing.T) {
	s := findFixture(t)
	res := find(t, s, "app")

	// "app" appears as a run in src/app and scattered through
	// a-packaged-application. The backward tightening pass is what makes the
	// run win; without it the greedy forward match takes the first three
	// scattered letters and the wrong file is top of the list.
	if len(res.Hits) == 0 {
		t.Fatal("expected matches for app")
	}
	first := res.Hits[0].Rel
	if first != "src/app" && first != "src/app/files" {
		t.Fatalf("expected the src/app run first, got %v", relNames(res))
	}
}

func TestFindTermsAreAnded(t *testing.T) {
	s := findFixture(t)
	res := find(t, s, "file tsx")
	for _, h := range res.Hits {
		if !strings.Contains(h.Rel, "file") || !strings.HasSuffix(h.Rel, ".tsx") {
			t.Fatalf("every hit must satisfy both terms, got %q", h.Rel)
		}
	}
	if !containsRel(res, "src/components/file-row.tsx") {
		t.Fatalf("expected file-row.tsx, got %v", relNames(res))
	}
	if containsRel(res, "etc/nginx/nginx.conf") {
		t.Fatalf("a hit satisfying only one term leaked in: %v", relNames(res))
	}
}

// The noise directories are the whole reason a search over a project is
// usable: node_modules alone outnumbers everything an operator wrote.
func TestFindSkipsNoiseAndHiddenDirectories(t *testing.T) {
	s := findFixture(t)
	if res := find(t, s, "index"); containsRel(res, "node_modules/left-pad/index.js") {
		t.Fatalf("node_modules must be skipped: %v", relNames(res))
	}
	// .git stays out even when hidden files are asked for: it is noise rather
	// than a dotfile somebody is looking for, which is the same reason
	// node_modules is on the list.
	for _, hidden := range []bool{false, true} {
		res := find(t, s, "deadbeef", func(o *FindOptions) { o.Hidden = hidden })
		if len(res.Hits) != 0 {
			t.Fatalf(".git must be skipped (hidden=%v): %v", hidden, relNames(res))
		}
	}
	if res := find(t, s, "secrets"); len(res.Hits) != 0 {
		t.Fatalf("a dotfile directory is out of the way by default: %v", relNames(res))
	}
	res := find(t, s, "secrets", func(o *FindOptions) { o.Hidden = true })
	if !containsRel(res, ".config/deploy-secrets.env") {
		t.Fatalf("hidden:true must reach a dotfile directory, got %v", relNames(res))
	}
}

// rankOf is the position in the ranking, or a number past the end for a
// result that is missing entirely.
func rankOf(res *FindResult, rel string) int {
	for i, h := range res.Hits {
		if h.Rel == rel {
			return i
		}
	}
	return len(res.Hits) + 1
}

// Containment is the invariant every route in this package shares, and a fuzzy
// walk is the one most likely to wander: it descends on its own rather than
// following a path the caller named.
func TestFindStaysInsideTheRoots(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.env"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Fatal(err)
	}
	s := New([]string{root})

	if _, err := s.Find(context.Background(), FindOptions{Root: outside, Query: "secret"}); err == nil {
		t.Fatal("a search rooted outside the permitted roots must be refused")
	}
	res := find(t, s, "secret")
	for _, h := range res.Hits {
		if strings.Contains(h.Path, outside) {
			t.Fatalf("the walk followed a symlink out of the roots: %q", h.Path)
		}
	}
}

// The highlight the UI draws is only as good as these offsets, and the browser
// counts UTF-16 code units where Go counts bytes.
func TestFindReportsMatchPositionsInTheName(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "café-report.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := New([]string{root})
	res := find(t, s, "report")
	if len(res.Hits) != 1 {
		t.Fatalf("expected one hit, got %v", relNames(res))
	}
	got := res.Hits[0].Matches
	// "café-" is five characters and five UTF-16 units, however many bytes it
	// takes; the match starts at the sixth.
	if len(got) != 6 || got[0] != 5 {
		t.Fatalf("matches = %v, want six positions starting at 5", got)
	}
}

func TestFindEmptyQueryFindsNothing(t *testing.T) {
	s := findFixture(t)
	if res := find(t, s, "   "); len(res.Hits) != 0 {
		t.Fatalf("a blank query must not list the disk: %v", relNames(res))
	}
}

func containsRel(res *FindResult, rel string) bool {
	for _, h := range res.Hits {
		if h.Rel == rel {
			return true
		}
	}
	return false
}
