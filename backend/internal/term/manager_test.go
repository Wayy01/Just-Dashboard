package term

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"testing"
)

// TestMain gives this package's tests a tmux server of their own.
//
// Two reasons, and both are the difference between a test suite that passes
// and one that means anything. The obvious one is the developer's machine: the
// terminal's whole promise is that a session outlives the process that made
// it, so a test that opened one against the real tmux server would leave it
// there, and a test that listed sessions would find the operator's. The other
// is that `go test ./...` runs packages concurrently — the sessions this
// package creates and the ones another package creates would land on the same
// server and appear in each other's listings, which is exactly the sort of
// cross-talk the feature must never have and a test must never invent.
//
// tmux takes its socket directory from TMUX_TMPDIR, and every `tmux` this
// package runs is a child of this process, so setting it here is enough.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "jdtmux")
	if err == nil {
		// Short, because a unix socket path has about a hundred characters to
		// play with and a nested temp directory can spend them all.
		os.Setenv("TMUX_TMPDIR", dir)
		defer os.RemoveAll(dir)
	}
	code := m.Run()
	// The server outlives the tests otherwise: that is the property under
	// test, and a stray tmux server per run is not a legacy worth keeping.
	exec.Command("tmux", "kill-server").Run()
	if err == nil {
		os.RemoveAll(dir)
	}
	os.Exit(code)
}


// A list endpoint that answers JSON null instead of [] crashes any client that
// iterates the result, so the contract is worth pinning down.
func TestTmuxSessionsMarshalsAsArray(t *testing.T) {
	m := NewManager(true, "/bin/sh", "")
	got := m.TmuxSessions(context.Background())
	if got == nil {
		t.Fatal("TmuxSessions returned a nil slice")
	}
	encoded, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	if string(encoded) == "null" {
		t.Fatalf("TmuxSessions marshalled to null, want an array")
	}
}

func TestRingBufferKeepsMostRecentBytes(t *testing.T) {
	r := newRingBuffer(8)
	r.Write([]byte("abcdef"))
	r.Write([]byte("ghij"))
	if got := string(r.Bytes()); got != "cdefghij" {
		t.Fatalf("Bytes() = %q, want %q", got, "cdefghij")
	}

	// A single write larger than the buffer keeps only its tail.
	r.Write([]byte("0123456789"))
	if got := string(r.Bytes()); got != "23456789" {
		t.Fatalf("Bytes() = %q, want %q", got, "23456789")
	}
}
