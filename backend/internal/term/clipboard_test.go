package term

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var clipboardFixtures = map[string][]byte{
	"image/png":  {0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 0, 0, 0, 0},
	"image/jpeg": {0xff, 0xd8, 0xff, 0xe0, 0, 0x10, 'J', 'F', 'I', 'F', 0},
	"image/webp": {'R', 'I', 'F', 'F', 4, 0, 0, 0, 'W', 'E', 'B', 'P', 'V', 'P', '8', ' '},
}

func TestClipboardStoreAcceptsOnlySupportedImageBytes(t *testing.T) {
	extensions := map[string]string{"image/png": ".png", "image/jpeg": ".jpg", "image/webp": ".webp"}
	for mimeType, body := range clipboardFixtures {
		t.Run(mimeType, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "clipboard")
			store := newClipboardStore(root)
			file, err := store.save("0123456789abcdef", mimeType, bytes.NewReader(body), os.Getuid(), os.Getgid())
			if err != nil {
				t.Fatal(err)
			}
			if !strings.HasSuffix(file.Path, extensions[mimeType]) {
				t.Errorf("path = %q, want %s extension", file.Path, extensions[mimeType])
			}
			if filepath.Dir(file.Path) != filepath.Join(root, "0123456789abcdef") {
				t.Errorf("path escaped its session directory: %q", file.Path)
			}
			if !clipboardName.MatchString(filepath.Base(file.Path)) {
				t.Errorf("filename is not randomized in the expected shape: %q", file.Path)
			}
			got, err := os.ReadFile(file.Path)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, body) {
				t.Errorf("saved bytes = %x, want %x", got, body)
			}
			info, err := os.Stat(file.Path)
			if err != nil {
				t.Fatal(err)
			}
			if info.Mode().Perm() != 0o600 {
				t.Errorf("file mode = %04o, want 0600", info.Mode().Perm())
			}
			dirInfo, err := os.Stat(filepath.Dir(file.Path))
			if err != nil {
				t.Fatal(err)
			}
			if dirInfo.Mode().Perm() != 0o700 {
				t.Errorf("directory mode = %04o, want 0700", dirInfo.Mode().Perm())
			}
		})
	}
}

func TestClipboardStoreRejectsUnsupportedAndMismatchedTypes(t *testing.T) {
	store := newClipboardStore(filepath.Join(t.TempDir(), "clipboard"))
	if _, err := store.save("0123456789abcdef", "image/gif", bytes.NewReader([]byte("GIF89a")), os.Getuid(), os.Getgid()); !errors.Is(err, ErrClipboardType) {
		t.Fatalf("GIF error = %v, want ErrClipboardType", err)
	}
	if _, err := store.save("0123456789abcdef", "image/png", bytes.NewReader(clipboardFixtures["image/jpeg"]), os.Getuid(), os.Getgid()); !errors.Is(err, ErrClipboardType) {
		t.Fatalf("mismatched bytes error = %v, want ErrClipboardType", err)
	}
}

type zeroReader struct{}

func (zeroReader) Read(p []byte) (int, error) {
	clear(p)
	return len(p), nil
}

func TestClipboardStoreRejectsOversizedImagesWithoutLeavingAPartialFile(t *testing.T) {
	root := filepath.Join(t.TempDir(), "clipboard")
	store := newClipboardStore(root)
	body := io.MultiReader(
		bytes.NewReader(clipboardFixtures["image/png"]),
		io.LimitReader(zeroReader{}, MaxClipboardImageBytes),
	)
	_, err := store.save("0123456789abcdef", "image/png", body, os.Getuid(), os.Getgid())
	if !errors.Is(err, ErrClipboardTooLarge) {
		t.Fatalf("error = %v, want ErrClipboardTooLarge", err)
	}
	entries, readErr := os.ReadDir(filepath.Join(root, "0123456789abcdef"))
	if readErr != nil {
		t.Fatal(readErr)
	}
	if len(entries) != 0 {
		t.Fatalf("oversized upload left %d entries behind", len(entries))
	}
}

func TestClipboardStoreRejectsSessionPathTraversalAndSymlinkRoots(t *testing.T) {
	parent := t.TempDir()
	store := newClipboardStore(filepath.Join(parent, "clipboard"))
	_, err := store.save("../../outside", "image/png", bytes.NewReader(clipboardFixtures["image/png"]), os.Getuid(), os.Getgid())
	if !errors.Is(err, ErrClipboardSessionID) {
		t.Fatalf("traversal error = %v, want ErrClipboardSessionID", err)
	}
	if _, err := os.Stat(filepath.Join(parent, "outside")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("path traversal created an outside entry: %v", err)
	}

	target := filepath.Join(parent, "target")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(parent, "linked-root")
	if err := os.Symlink(target, root); err != nil {
		t.Fatal(err)
	}
	linked := newClipboardStore(root)
	if _, err := linked.save("0123456789abcdef", "image/png", bytes.NewReader(clipboardFixtures["image/png"]), os.Getuid(), os.Getgid()); err == nil {
		t.Fatal("a symlink clipboard root was accepted")
	}
	if entries, err := os.ReadDir(target); err != nil || len(entries) != 0 {
		t.Fatalf("symlink target was written: entries=%v err=%v", entries, err)
	}
}

func TestClipboardCleanupRemovesExpiredFilesAndKeepsRecentOnes(t *testing.T) {
	root := filepath.Join(t.TempDir(), "clipboard")
	store := newClipboardStore(root)
	old, err := store.save("0123456789abcdef", "image/png", bytes.NewReader(clipboardFixtures["image/png"]), os.Getuid(), os.Getgid())
	if err != nil {
		t.Fatal(err)
	}
	recent, err := store.save("fedcba9876543210", "image/jpeg", bytes.NewReader(clipboardFixtures["image/jpeg"]), os.Getuid(), os.Getgid())
	if err != nil {
		t.Fatal(err)
	}
	live, err := store.save("aaaaaaaaaaaaaaaa", "image/webp", bytes.NewReader(clipboardFixtures["image/webp"]), os.Getuid(), os.Getgid())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if err := os.Chtimes(old.Path, now.Add(-2*time.Hour), now.Add(-2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(live.Path, now.Add(-2*time.Hour), now.Add(-2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	store.cleanupExpired(now.Add(-time.Hour), map[string]bool{"aaaaaaaaaaaaaaaa": true})

	if _, err := os.Stat(old.Path); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("expired file still exists: %v", err)
	}
	if _, err := os.Stat(recent.Path); err != nil {
		t.Errorf("recent file was removed: %v", err)
	}
	if _, err := os.Stat(live.Path); err != nil {
		t.Errorf("expired file belonging to a live PTY was removed: %v", err)
	}
	if _, err := os.Stat(filepath.Dir(old.Path)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("empty expired session directory still exists: %v", err)
	}
}
