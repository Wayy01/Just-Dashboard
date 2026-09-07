package dockerx

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateComposeFilesUsesOrderedTemporaryInputsWithoutTouchingSource(t *testing.T) {
	bin := t.TempDir()
	capture := filepath.Join(t.TempDir(), "capture")
	paths := filepath.Join(t.TempDir(), "paths")
	script := `#!/bin/sh
set -eu
printf '%s\n' "$*" >> "$COMPOSE_ARGV_CAPTURE"
previous=''
for argument in "$@"; do
  if [ "$previous" = '-f' ]; then
    printf '%s\n' "$argument" >> "$COMPOSE_PATH_CAPTURE"
    cat "$argument" >> "$COMPOSE_ARGV_CAPTURE"
  fi
  previous="$argument"
done
case "$*" in
  *'config --services') printf 'db\nweb\n' ;;
  *'config --quiet') ;;
  *'config') printf 'services:\n  db: {}\n  web: {}\n' ;;
esac
`
	command := filepath.Join(bin, "docker")
	if err := os.WriteFile(command, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("COMPOSE_ARGV_CAPTURE", capture)
	t.Setenv("COMPOSE_PATH_CAPTURE", paths)
	source := t.TempDir()
	if err := os.WriteFile(filepath.Join(source, "sentinel"), []byte("unchanged"), 0o600); err != nil {
		t.Fatal(err)
	}

	client := New("")
	result, err := client.ValidateComposeFiles(context.Background(), source, []ComposeInput{
		{Path: "compose.yml", Content: "services:\n  db:\n    image: postgres:17\n"},
		{Path: "compose.override.yml", Content: "services:\n  web:\n    image: nginx:1.27\n"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Valid || !strings.Contains(result.Normalised, "services:") ||
		strings.Join(result.Services, ",") != "db,web" {
		t.Fatalf("validation result = %#v", result)
	}
	captured, err := os.ReadFile(capture)
	if err != nil {
		t.Fatal(err)
	}
	first := strings.Index(string(captured), "image: postgres:17")
	second := strings.Index(string(captured), "image: nginx:1.27")
	if first < 0 || second < 0 || first > second {
		t.Fatalf("Compose input order was not preserved: %s", captured)
	}
	temporaryPaths, err := os.ReadFile(paths)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range strings.Fields(string(temporaryPaths)) {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("temporary Compose file still exists at %q: %v", path, err)
		}
	}
	entries, err := os.ReadDir(source)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "sentinel" {
		t.Fatalf("validation changed the source directory: %#v", entries)
	}
}

func TestValidateComposePlanUsesIsolatedPlaceholderEnvironment(t *testing.T) {
	bin := t.TempDir()
	capture := filepath.Join(bin, "docker.capture")
	script := `#!/bin/sh
set -eu
printf 'argv=%s\n' "$*" >> "$0.capture"
printf 'app=%s\n' "${APP_IMAGE:-missing}" >> "$0.capture"
printf 'ambient=%s\n' "${BACKEND_SECRET-unset}" >> "$0.capture"
case "$*" in
  *'config --services') printf 'app\n' ;;
  *'config --quiet') ;;
esac
`
	command := filepath.Join(bin, "docker")
	if err := os.WriteFile(command, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("APP_IMAGE", "ambient-image-secret")
	t.Setenv("BACKEND_SECRET", "ambient-backend-secret")
	source := t.TempDir()
	if err := os.WriteFile(filepath.Join(source, ".env"), []byte("APP_IMAGE=dotenv-secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	result, err := New("").ValidateComposePlan(context.Background(), source, []ComposeInput{{
		Path: "compose.yml", Content: "services:\n  app:\n    image: ${APP_IMAGE}\n",
	}}, []string{"APP_IMAGE"})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Valid || strings.Join(result.Services, ",") != "app" {
		t.Fatalf("planning validation result = %#v", result)
	}
	captured, err := os.ReadFile(capture)
	if err != nil {
		t.Fatal(err)
	}
	text := string(captured)
	if !strings.Contains(text, "app=just-dashboard-planning-placeholder") ||
		!strings.Contains(text, "ambient=unset") || strings.Contains(text, "ambient-image-secret") ||
		strings.Contains(text, "ambient-backend-secret") || strings.Contains(text, "dotenv-secret") {
		t.Fatalf("planning Compose inherited ambient material: %s", text)
	}
	for _, line := range strings.Split(text, "\n") {
		if !strings.HasPrefix(line, "argv=") {
			continue
		}
		fields := strings.Fields(strings.TrimPrefix(line, "argv="))
		for index, field := range fields {
			if (field == "--env-file" || field == "-f") && index+1 < len(fields) {
				if _, err := os.Stat(fields[index+1]); !os.IsNotExist(err) {
					t.Fatalf("temporary planning input still exists at %q: %v", fields[index+1], err)
				}
			}
		}
	}
}
