package safepath

import (
	"os"
	"path/filepath"
	"testing"
)

// The case that matters: filepath.Join drops the leading separator of an
// absolute second element, so "/etc/cron.d" as a symlink target collapsed to
// "etc/cron.d", landed inside the destination and passed the old lexical
// guard. The link was then created with the raw absolute target, and the next
// archive entry was written through it — as root, from a role holding nothing
// but file.write.
func TestCheckLinkTargetRefusesAbsolute(t *testing.T) {
	dest := t.TempDir()
	if err := CheckLinkTarget(dest, "link", "/etc/cron.d"); err == nil {
		t.Fatal("an absolute symlink target must be refused")
	}
	if err := CheckLinkTarget(dest, "a/link", "../../../etc"); err == nil {
		t.Fatal("a relative target escaping the destination must be refused")
	}
	if err := CheckLinkTarget(dest, "a/link", "../b"); err != nil {
		t.Fatalf("a target inside the destination must be allowed: %v", err)
	}
	if err := CheckLinkTarget(dest, "link", ""); err == nil {
		t.Fatal("an empty target must be refused")
	}
}

func TestJoinRefusesTraversal(t *testing.T) {
	dest := t.TempDir()
	for _, name := range []string{"../evil", "a/../../evil", "../../etc/passwd"} {
		if _, err := Join(dest, name); err == nil {
			t.Fatalf("entry %q must not resolve inside the destination", name)
		}
	}
	// An entry named with a leading separator is relocated under the
	// destination, which is what tar itself does with an absolute member name.
	abs, err := Join(dest, "/etc/passwd")
	if err != nil {
		t.Fatalf("an absolute entry name should be relocated, not refused: %v", err)
	}
	if want := filepath.Join(dest, "etc/passwd"); abs != want {
		t.Fatalf("Join = %q, want %q", abs, want)
	}
	got, err := Join(dest, "a/b/c")
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(dest, "a/b/c"); got != want {
		t.Fatalf("Join = %q, want %q", got, want)
	}
}

// The second half of the escape: even with an absolute target refused, an
// archive can plant a link to a directory inside the roots and write through
// it. Nothing is written through a symlink that already exists.
func TestJoinRefusesWritingThroughAnExistingSymlink(t *testing.T) {
	dest := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(dest, "link")); err != nil {
		t.Fatal(err)
	}
	if _, err := Join(dest, "link/evil"); err == nil {
		t.Fatal("writing through a pre-existing symlink must be refused")
	}
	// The link itself is still addressable, so re-extracting an archive that
	// contains it keeps working.
	if _, err := Join(dest, "link"); err != nil {
		t.Fatalf("the link itself must remain addressable: %v", err)
	}
}

func TestCreateDoesNotFollowASymlink(t *testing.T) {
	dest := t.TempDir()
	outside := filepath.Join(t.TempDir(), "target")
	if err := os.WriteFile(outside, []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dest, "entry")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	f, err := Create(link, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	b, err := os.ReadFile(outside)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "original" {
		t.Fatalf("the symlink's target was overwritten: %q", b)
	}
}

func TestMkdirReplacesASymlink(t *testing.T) {
	dest := t.TempDir()
	outside := t.TempDir()
	path := filepath.Join(dest, "dir")
	if err := os.Symlink(outside, path); err != nil {
		t.Fatal(err)
	}
	if err := Mkdir(path, 0o755); err != nil {
		t.Fatal(err)
	}
	st, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode()&os.ModeSymlink != 0 {
		t.Fatal("the entry is still a symlink; a later write would follow it")
	}
}
