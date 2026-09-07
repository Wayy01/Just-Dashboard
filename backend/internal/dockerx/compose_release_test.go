package dockerx

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestComposeReleaseEnvironmentIsPrivateSortedAndEscaped(t *testing.T) {
	directory := t.TempDir()
	path, err := writeComposeReleaseEnv(directory, map[string]string{
		"TOKEN": "line one\nline two", "ALPHA": "quoted \" value",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(path)
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("env file mode = %v, error=%v", info.Mode().Perm(), err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	if strings.Index(text, "ALPHA=") > strings.Index(text, "TOKEN=") ||
		!strings.Contains(text, `TOKEN="line one\nline two"`) ||
		!strings.Contains(text, `ALPHA="quoted \" value"`) {
		t.Fatalf("env file is not deterministic/escaped: %q", text)
	}
	if !containedComposeFile(directory, filepath.Join(directory, "compose.yml")) ||
		containedComposeFile(directory, filepath.Join(directory, "..", "compose.yml")) {
		t.Fatal("Compose release containment check is incorrect")
	}
}

func TestComposeReleaseProjectNamesAreClosed(t *testing.T) {
	for _, valid := range []string{"jd-e1", "app_2", "a"} {
		if !validComposeProjectName(valid) {
			t.Fatalf("valid project %q rejected", valid)
		}
	}
	for _, invalid := range []string{"", "UPPER", "-prefix", "../escape", "has space"} {
		if validComposeProjectName(invalid) {
			t.Fatalf("invalid project %q accepted", invalid)
		}
	}
}
