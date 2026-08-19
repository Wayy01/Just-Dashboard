package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Wayy01/Just-Dashboard/backend/internal/store"
)

// The dashboard was called "VPS Dashboard" and read VPSD_* settings. Both
// fallbacks below exist so that pulling the rename does not lock an operator
// out of their own server, which is the one upgrade failure with no recovery
// short of restoring a backup.

func TestEnvFallsBackToLegacyPrefix(t *testing.T) {
	t.Setenv("VPSD_TERMINAL_SHELL", "/bin/zsh")
	if got := Env("JD_TERMINAL_SHELL"); got != "/bin/zsh" {
		t.Fatalf("legacy VPSD_ name ignored: got %q", got)
	}

	t.Setenv("JD_TERMINAL_SHELL", "/bin/fish")
	if got := Env("JD_TERMINAL_SHELL"); got != "/bin/fish" {
		t.Fatalf("current JD_ name should win: got %q", got)
	}
}

func TestEnvIgnoresLegacyForNonPrefixedKeys(t *testing.T) {
	t.Setenv("VPSD_PATH", "surprise")
	if got := Env("PATH_THAT_IS_NOT_OURS"); got != "" {
		t.Fatalf("unprefixed key should not consult VPSD_: got %q", got)
	}
}

func TestAdoptLegacyDataPrefersTheDirectoryHoldingTheDatabase(t *testing.T) {
	root := t.TempDir()
	current := filepath.Join(root, "just-dashboard")
	legacy := filepath.Join(root, "vps-dashboard")

	// Docker bind-mounts create the configured directory whether or not
	// anything has ever been written to it, so an empty new directory must not
	// count as "already migrated".
	mkdir(t, current)
	mkdir(t, legacy)
	writeDatabase(t, legacy)

	if got := adoptLegacyData(current, legacy); got != legacy {
		t.Fatalf("expected the legacy database to be adopted, got %q", got)
	}

	// Once the new location has its own database, the old one is history.
	writeDatabase(t, current)
	if got := adoptLegacyData(current, legacy); got != current {
		t.Fatalf("expected the configured directory to win, got %q", got)
	}
}

func TestAdoptLegacyDataLeavesFreshInstallsAlone(t *testing.T) {
	root := t.TempDir()
	current := filepath.Join(root, "just-dashboard")
	legacy := filepath.Join(root, "vps-dashboard")
	mkdir(t, current)

	if got := adoptLegacyData(current, legacy); got != current {
		t.Fatalf("no legacy database exists, so nothing should be adopted: got %q", got)
	}
}

func mkdir(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
}

func writeDatabase(t *testing.T, dir string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, store.DatabaseFile), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
}
