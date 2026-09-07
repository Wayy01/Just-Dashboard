package hostexec

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestCommandInDirKeepsLocalWorkingDirectoryAndArgv(t *testing.T) {
	dir := t.TempDir()
	cmd := CommandInDir(context.Background(), dir, "sh", "-c", "printf '%s' \"$1\"", "sh", "a b;$HOME")
	if cmd.Dir != dir {
		t.Fatalf("Dir = %q, want %q", cmd.Dir, dir)
	}
	got, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "a b;$HOME" {
		t.Fatalf("output = %q; argv was not preserved", got)
	}
}

func TestInDirArgsPlacesWorkingDirectoryBeforeOptionTerminator(t *testing.T) {
	got := inDirArgs("/srv/a b")
	want := []string{"--target", "1", "--mount", "--uts", "--ipc", "--net", "--pid", "--wd=/srv/a b", "--"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("inDirArgs() = %#v, want %#v", got, want)
	}
}

func TestAsOwnerPreservesExistingProcessGroupPolicy(t *testing.T) {
	if syscall.Geteuid() != 0 {
		t.Skip("owner dropping is meaningful only as root")
	}
	dir := t.TempDir()
	if err := os.Chown(dir, 65534, 65534); err != nil {
		t.Skipf("cannot prepare non-root-owned directory: %v", err)
	}
	cmd := exec.Command("true")
	cmd.Dir = dir
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	AsOwner(cmd)

	if cmd.SysProcAttr == nil || !cmd.SysProcAttr.Setpgid {
		t.Fatal("AsOwner replaced the existing process-group policy")
	}
	if cmd.SysProcAttr.Credential == nil || cmd.SysProcAttr.Credential.Uid != 65534 {
		t.Fatal("AsOwner did not add the directory owner's credential")
	}
}

func TestRunGroupTerminatesDescendantsAndRecordsForcedCleanup(t *testing.T) {
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "child.pid")
	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, "/bin/sh", "-c",
		`trap '' TERM; /bin/sh -c 'trap "" TERM; while :; do sleep 1; done' & echo $! > "$1"; wait`,
		"sh", pidFile)

	type outcome struct {
		result GroupResult
		err    error
	}
	done := make(chan outcome, 1)
	go func() {
		result, err := RunGroup(ctx, cmd, 75*time.Millisecond)
		done <- outcome{result: result, err: err}
	}()
	childPID := waitForPIDFile(t, pidFile)
	cancel()

	select {
	case got := <-done:
		if !errors.Is(got.err, context.Canceled) {
			t.Fatalf("RunGroup error = %v, want context cancellation", got.err)
		}
		if !got.result.TERMSent || !got.result.KILLSent {
			t.Fatalf("cleanup evidence = %#v, want TERM then KILL", got.result)
		}
		var groupErr *GroupError
		if !errors.As(got.err, &groupErr) || groupErr.Result != got.result {
			t.Fatalf("group error did not retain cleanup evidence: %v", got.err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("RunGroup did not complete after cancellation")
	}
	waitForProcessGone(t, childPID)
}

func waitForPIDFile(t *testing.T, path string) int {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(path)
		if err == nil {
			pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
			if err != nil {
				t.Fatalf("parse child pid: %v", err)
			}
			return pid
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("child pid was not written")
	return 0
}

func waitForProcessGone(t *testing.T, pid int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if err := syscall.Kill(pid, 0); errors.Is(err, syscall.ESRCH) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("descendant process %d survived process-group cancellation", pid)
}
