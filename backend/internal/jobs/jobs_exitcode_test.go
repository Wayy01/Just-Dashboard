package jobs

import (
	"context"
	"log/slog"
	"testing"
	"time"
)

func awaitJob(t *testing.T, m *Manager, id string) Job {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		job, _, ok := m.Get(id)
		if ok && job.Status != StatusRunning {
			return job
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("job did not finish")
	return Job{}
}

// ExitCode is a field the API has always carried and nothing ever wrote, so
// every failed job reported "exit 0" — which next to "failed" is a
// contradiction the reader has to resolve by ignoring one of them.
func TestJobRecordsWhatTheCommandExitedWith(t *testing.T) {
	m := New(slog.Default())
	t.Cleanup(m.Shutdown)
	job := m.Start(Spec{Kind: "test", Title: "exit 3", Timeout: 10 * time.Second},
		func(ctx context.Context, out Emitter) error {
			_, err := out.Run(ctx, "sh", "-c", "exit 3")
			return err
		})
	done := awaitJob(t, m, job.ID)
	if done.ExitCode != 3 {
		t.Fatalf("ExitCode = %d, want 3", done.ExitCode)
	}
}

func TestJobRecordsZeroForACommandThatSucceeded(t *testing.T) {
	m := New(slog.Default())
	t.Cleanup(m.Shutdown)
	job := m.Start(Spec{Kind: "test", Title: "true", Timeout: 10 * time.Second},
		func(ctx context.Context, out Emitter) error {
			_, err := out.Run(ctx, "sh", "-c", "true")
			return err
		})
	done := awaitJob(t, m, job.ID)
	if done.ExitCode != 0 || done.Status != StatusSucceeded {
		t.Fatalf("job = %+v", done)
	}
}

// A job that runs several commands is reported by the one that decided the
// outcome, which is the last to run.
func TestJobRecordsTheLastCommandsCode(t *testing.T) {
	m := New(slog.Default())
	t.Cleanup(m.Shutdown)
	job := m.Start(Spec{Kind: "test", Title: "two", Timeout: 10 * time.Second},
		func(ctx context.Context, out Emitter) error {
			if _, err := out.Run(ctx, "sh", "-c", "exit 7"); err != nil {
				return err
			}
			_, err := out.Run(ctx, "sh", "-c", "exit 2")
			return err
		})
	done := awaitJob(t, m, job.ID)
	if done.ExitCode != 2 {
		t.Fatalf("ExitCode = %d, want the last command's 2", done.ExitCode)
	}
}

// A command that never started must not be reported with whatever the previous
// one exited with.
func TestJobDoesNotInheritACodeWhenTheCommandIsMissing(t *testing.T) {
	m := New(slog.Default())
	t.Cleanup(m.Shutdown)
	job := m.Start(Spec{Kind: "test", Title: "missing", Timeout: 10 * time.Second},
		func(ctx context.Context, out Emitter) error {
			if _, err := out.Run(ctx, "sh", "-c", "exit 5"); err != nil {
				return err
			}
			_, err := out.Run(ctx, "jd-no-such-binary-anywhere")
			return err
		})
	done := awaitJob(t, m, job.ID)
	if done.ExitCode == 5 {
		t.Fatal("a command that never ran inherited the previous exit code")
	}
	if done.Status != StatusFailed {
		t.Fatalf("status = %q", done.Status)
	}
}

// A job that runs no command at all has nothing to report and stays at zero.
func TestJobWithNoCommandKeepsAZeroCode(t *testing.T) {
	m := New(slog.Default())
	t.Cleanup(m.Shutdown)
	job := m.Start(Spec{Kind: "test", Title: "none", Timeout: 10 * time.Second},
		func(ctx context.Context, out Emitter) error {
			out.Status("nothing to run")
			return nil
		})
	done := awaitJob(t, m, job.ID)
	if done.ExitCode != 0 || done.Status != StatusSucceeded {
		t.Fatalf("job = %+v", done)
	}
}
