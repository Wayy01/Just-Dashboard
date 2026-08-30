package files

import (
	"os"
	"path/filepath"
	"testing"
)

// Where the page opens is the first thing anybody notices, and the rule has
// to survive the two hosts this runs on: one where $HOME is a real directory
// inside the roots, and one where it is not reachable at all.
func TestHomeFallsBackInsideTheRoots(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home", "deploy")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	s := New([]string{root})

	t.Setenv("HOME", home)
	if got := s.Home(); got != home {
		t.Fatalf("Home() = %q, want %q", got, home)
	}

	// $HOME pointing somewhere the roots refuse must not produce a start
	// location that answers "outside the permitted roots" on first paint.
	t.Setenv("HOME", t.TempDir())
	if got := s.Home(); got != root {
		t.Fatalf("Home() = %q, want the configured root %q", got, root)
	}
}

func TestPlacesOnlyOffersWhatIsReachable(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "etc"), 0o755); err != nil {
		t.Fatal(err)
	}
	s := New([]string{root})
	t.Setenv("HOME", root)

	for _, p := range s.Places() {
		if _, err := s.Resolve(p.Path); err != nil {
			t.Fatalf("Places offered %q, which Resolve refuses: %v", p.Path, err)
		}
		if _, err := os.Stat(p.Path); err != nil {
			t.Fatalf("Places offered %q, which does not exist", p.Path)
		}
	}
	// /etc exists on the host but not under this root, so the notable entry
	// must not be offered — a shortcut that lands on a refusal is worse than
	// no shortcut.
	for _, p := range s.Places() {
		if p.Path == "/etc" {
			t.Fatal("a notable place outside the roots was offered")
		}
	}
}

// Complete is what makes typing a path a real alternative to clicking, and it
// has the same containment obligation as everything else here.
func TestCompleteFiltersAndContains(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"var", "vault", "etc"} {
		if err := os.Mkdir(filepath.Join(root, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, ".hidden"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "va.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := New([]string{root})

	names := func(entries []Entry) []string {
		out := []string{}
		for _, e := range entries {
			out = append(out, e.Name)
		}
		return out
	}

	got := names(s.Complete(filepath.Join(root, "va"), 0))
	if len(got) != 3 || got[0] != "var" || got[1] != "vault" || got[2] != "va.txt" {
		t.Fatalf("Complete(va) = %v, want the two directories then the file", got)
	}
	if got := names(s.Complete(root+string(os.PathSeparator), 0)); len(got) != 4 {
		t.Fatalf("a trailing separator means the whole directory, got %v", got)
	}
	// A dotfile appears once it has been asked for, and not before — the same
	// rule the listing's "show hidden" switch applies.
	if got := names(s.Complete(filepath.Join(root, ".h"), 0)); len(got) != 1 || got[0] != ".hidden" {
		t.Fatalf("Complete(.h) = %v, want .hidden", got)
	}
	if got := s.Complete(filepath.Join(t.TempDir(), "x"), 0); len(got) != 0 {
		t.Fatalf("Complete outside the roots must answer with nothing, got %v", names(got))
	}
}
