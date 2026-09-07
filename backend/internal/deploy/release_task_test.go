package deploy

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestStoredReleaseTaskUsesExplicitScopeAndRedactsOutput(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "migrations"), 0o700); err != nil {
		t.Fatal(err)
	}
	secret := "release-task-fixture-secret"
	logs := []string{}
	evidence, group, err := runStoredReleaseTask(context.Background(), root, ReleaseTaskConfig{
		Name: "database migration", Command: `printf '%s|%s|%s\n' "$TOKEN" "${RUNTIME_ONLY-unset}" "$PWD"`,
		WorkingDirectory: "migrations", TimeoutSeconds: 5, Env: []string{"TOKEN"},
	}, map[string]string{"TOKEN": secret, "RUNTIME_ONLY": "must-not-be-present"}, func(line BuildLog) error {
		logs = append(logs, line.Text)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(logs, "\n")
	if strings.Contains(joined, secret) || strings.Contains(joined, "must-not-be-present") ||
		!strings.Contains(joined, "[REDACTED]|unset|") || !strings.Contains(joined, filepath.Join(root, "migrations")) {
		t.Fatalf("release task output = %q", joined)
	}
	if evidence.Name != "database migration" || evidence.ExitCode != 0 ||
		len(evidence.VariableNames) != 1 || evidence.VariableNames[0] != "TOKEN" || group.ExitCode != 0 {
		t.Fatalf("release task evidence = %#v / %#v", evidence, group)
	}
	serialized := string(mustJSON(evidence))
	if strings.Contains(serialized, secret) || strings.Contains(serialized, "printf") {
		t.Fatalf("release task evidence leaked command or value: %s", serialized)
	}
}

func TestStoredReleaseTaskCancellationTerminatesProcessGroup(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	evidence, group, err := runStoredReleaseTask(ctx, root, ReleaseTaskConfig{
		Name: "slow migration", Command: "sleep 30 & wait", TimeoutSeconds: 30, Env: []string{},
	}, nil, nil)
	if !errors.Is(err, context.DeadlineExceeded) || !group.TERMSent || evidence.ExitCode == 0 {
		t.Fatalf("cancelled release task = evidence %#v group %#v err %v", evidence, group, err)
	}
}

func TestStoredReleaseTaskRejectsEscapingOrMissingWorkingDirectory(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Fatal(err)
	}
	for _, directory := range []string{"escape", "missing"} {
		_, _, err := runStoredReleaseTask(context.Background(), root, ReleaseTaskConfig{
			Name: "invalid", Command: "true", WorkingDirectory: directory, TimeoutSeconds: 1,
		}, nil, nil)
		if err == nil || !strings.Contains(err.Error(), "working directory is unavailable") {
			t.Fatalf("working directory %q error = %v", directory, err)
		}
	}
}
