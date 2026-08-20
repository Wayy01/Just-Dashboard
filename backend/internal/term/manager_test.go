package term

import (
	"context"
	"encoding/json"
	"testing"
)

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
