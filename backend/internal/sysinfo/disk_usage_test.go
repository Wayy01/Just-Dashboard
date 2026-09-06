package sysinfo

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestDirBreakdownBoundsChildrenAndRecursiveVisits(t *testing.T) {
	root := t.TempDir()
	for _, path := range []string{
		filepath.Join(root, "one"),
		filepath.Join(root, "two"),
		filepath.Join(root, "tree", "three"),
	} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	if _, err := dirBreakdown(context.Background(), root, 10, 2, 100); !errors.Is(err, ErrDirScanLimit) {
		t.Fatalf("child limit error = %v, want ErrDirScanLimit", err)
	}
	if _, err := dirBreakdown(context.Background(), root, 10, 10, 2); !errors.Is(err, ErrDirScanLimit) {
		t.Fatalf("visit limit error = %v, want ErrDirScanLimit", err)
	}
	if entries, err := dirBreakdown(context.Background(), root, 2, 10, 100); err != nil || len(entries) != 2 {
		t.Fatalf("bounded breakdown = %d entries, %v; want 2, nil", len(entries), err)
	}
}

func TestDirBreakdownReportsUnreadableSubtrees(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can read directories regardless of their mode")
	}
	root := t.TempDir()
	locked := filepath.Join(root, "locked")
	if err := os.Mkdir(locked, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(locked, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(locked, 0o700) })

	if _, err := dirBreakdown(context.Background(), root, 10, 10, 100); err == nil {
		t.Fatal("unreadable subtree produced a silently partial result")
	}
}
