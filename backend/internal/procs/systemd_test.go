package procs

import (
	"testing"
	"time"
)

func TestActiveSinceCombinesBootUptimeWithMonotonicTimestamp(t *testing.T) {
	now := time.Date(2026, 9, 1, 22, 0, 0, 0, time.UTC)
	// Host booted two hours ago and the service became active half an hour
	// after boot, so it has been active for ninety minutes.
	got := activeSinceUnix(uint64((30*time.Minute)/time.Microsecond), uint64((2*time.Hour)/time.Second), now)
	want := now.Add(-90 * time.Minute)
	if !got.Equal(want) {
		t.Fatalf("active since = %s, want %s", got, want)
	}
}

func TestParseSystemdTimestampFallback(t *testing.T) {
	got, ok := parseSystemdTimestamp("Tue 2026-09-01 20:30:00 UTC")
	if !ok || got.Unix() != time.Date(2026, 9, 1, 20, 30, 0, 0, time.UTC).Unix() {
		t.Fatalf("parsed timestamp = %s, %v", got, ok)
	}
}
