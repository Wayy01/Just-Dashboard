package netsec

import (
	"testing"
	"time"
)

// Real `last -F -w` output from a util-linux host. The columns are padded
// rather than delimited and the "from" column is empty for a console login,
// which is what makes a fixed field count the wrong way to read this.
const lastOutput = `root     pts/1        203.0.113.9      Wed Aug 20 03:15:22 2026 - Wed Aug 20 04:02:11 2026  (00:46)
deploy   pts/0        10.8.0.4         Wed Aug 20 02:59:00 2026   still logged in
root     tty1                          Tue Aug 19 22:04:03 2026 - down                      (01:55)
ubuntu   pts/3        198.51.100.7     Tue Aug 19 20:00:00 2026 - crash                     (02:04)
reboot   system boot  6.1.0-18-amd64   Tue Aug 19 10:00:01 2026   still running

wtmp begins Tue Feb 17 02:02:53 2026
`

func TestParseLastReadsEveryShapeOfRecord(t *testing.T) {
	got := parseLast(lastOutput, 100)
	if len(got) != 5 {
		t.Fatalf("parsed %d records, want 5:\n%+v", len(got), got)
	}

	closed := got[0]
	if closed.User != "root" || closed.TTY != "pts/1" || closed.From != "203.0.113.9" {
		t.Errorf("closed session parsed as %+v", closed)
	}
	if closed.LoginTime == nil || closed.LoginTime.Format("2006-01-02 15:04:05") != "2026-08-20 03:15:22" {
		t.Errorf("login time = %v", closed.LoginTime)
	}
	if closed.EndTime == nil || closed.EndTime.Format("15:04:05") != "04:02:11" {
		t.Errorf("end time = %v", closed.EndTime)
	}
	if closed.Duration != "00:46" || closed.Active {
		t.Errorf("duration = %q active = %v", closed.Duration, closed.Active)
	}

	open := got[1]
	if !open.Active || open.EndTime != nil {
		t.Errorf("a session still logged in parsed as %+v", open)
	}

	// A console login has no "from" at all; reading the columns positionally
	// shifts the timestamp into it.
	console := got[2]
	if console.TTY != "tty1" || console.From != "" {
		t.Errorf("console login parsed as %+v", console)
	}
	if console.Ended != "down" {
		t.Errorf("ended = %q, want down", console.Ended)
	}

	if got[3].Ended != "crash" {
		t.Errorf("ended = %q, want crash", got[3].Ended)
	}

	boot := got[4]
	if boot.Kind != "boot" || boot.TTY != "" {
		t.Errorf("reboot parsed as %+v", boot)
	}
	if boot.From != "6.1.0-18-amd64" {
		t.Errorf("reboot from = %q, want the kernel version", boot.From)
	}
}

// A busybox `last` refuses -F, and its output carries no year. It still has to
// parse: the alternative is an empty table on those hosts.
func TestParseLastWithoutTheYear(t *testing.T) {
	got := parseLast("root     pts/0        10.0.0.2         Wed Aug 20 03:15 - 04:02  (00:46)\n", 10)
	if len(got) != 1 {
		t.Fatalf("parsed %d records, want 1", len(got))
	}
	if got[0].LoginTime == nil {
		t.Fatal("no login time")
	}
	if got[0].LoginTime.Year() != time.Now().Year() {
		t.Errorf("year = %d, want the current one", got[0].LoginTime.Year())
	}
	if got[0].LoginTime.Format("01-02 15:04") != "08-20 03:15" {
		t.Errorf("login time = %v", got[0].LoginTime)
	}
}

// Headers, footers and anything else without a timestamp are skipped rather
// than guessed at — a half-parsed row of a security log is worse than no row.
func TestParseLastSkipsWhatItCannotRead(t *testing.T) {
	got := parseLast("wtmp begins Tue Feb 17 02:02:53 2026\n\nnonsense\nroot\n", 10)
	if len(got) != 0 {
		t.Fatalf("invented %d records from junk: %+v", len(got), got)
	}
}

func TestParseLastHonoursTheLimit(t *testing.T) {
	if got := parseLast(lastOutput, 2); len(got) != 2 {
		t.Fatalf("returned %d records for a limit of 2", len(got))
	}
}
