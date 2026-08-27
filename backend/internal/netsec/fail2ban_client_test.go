package netsec

import (
	"context"
	"strings"
	"testing"
)

// Every fixture is `fail2ban-client` output copied from Fail2Ban v1.0.2 — the
// build on Debian 12 and Ubuntu 24.04, which is to say the one nearly every
// install of this dashboard is talking to. It does not print a list. It draws
// a tree, and when there is nothing to draw it answers in a sentence.

func TestParseClientListReadsTheTreeFail2banDraws(t *testing.T) {
	got := parseClientList(`These IP addresses/networks are ignored:
|- 127.0.0.0/8
|- 10.0.0.0/8
` + "`- 192.0.2.5")
	want := "127.0.0.0/8,10.0.0.0/8,192.0.2.5"
	if strings.Join(got, ",") != want {
		t.Fatalf("got %v, want %v", got, want)
	}
}

// The empty answer is prose. Read as data it became five allowlist entries
// beginning with "No", on every jail that had never been given one — which is
// most of them.
func TestParseClientListReadsAnEmptyAnswerAsEmpty(t *testing.T) {
	for _, sentence := range []string{
		"No IP address/network is ignored",
		"No file is currently monitored",
	} {
		if got := parseClientList(sentence); len(got) != 0 {
			t.Errorf("parseClientList(%q) = %v, want none", sentence, got)
		}
	}
}

// A single-valued answer is a heading and a bare line, and the heading is not
// one of the values.
func TestParseClientListDropsTheHeading(t *testing.T) {
	got := parseClientList("The jail sshd has the following actions:\niptables-multiport")
	if strings.Join(got, ",") != "iptables-multiport" {
		t.Fatalf("got %v", got)
	}
}

// The shapes older builds print still work: this runs on whatever the host has.
func TestParseClientListStillReadsTheOlderShapes(t *testing.T) {
	if got := parseClientList(`['127.0.0.1/8', '::1']`); strings.Join(got, ",") != "127.0.0.1/8,::1" {
		t.Errorf("python list = %v", got)
	}
	if got := parseClientList("127.0.0.1/8 ::1"); strings.Join(got, ",") != "127.0.0.1/8,::1" {
		t.Errorf("bare line = %v", got)
	}
}

// The jail report, against the real thing. The values are behind tab-separated
// labels inside a tree, and the label is what identifies them — the indent
// changes with the nesting and the tree characters change with the position.
func TestJailStatusParsesTheRealReport(t *testing.T) {
	previous := run
	run = func(_ context.Context, name string, args ...string) (string, error) {
		return "Status for the jail: sshd\n" +
			"|- Filter\n" +
			"|  |- Currently failed:\t3\n" +
			"|  |- Total failed:\t812\n" +
			"|  `- File list:\t/var/log/auth.log\n" +
			"`- Actions\n" +
			"   |- Currently banned:\t2\n" +
			"   |- Total banned:\t57\n" +
			"   `- Banned IP list:\t192.0.2.5 198.51.100.7\n", nil
	}
	t.Cleanup(func() { run = previous })

	j, err := New().jailStatus(t.Context(), "sshd")
	if err != nil {
		t.Fatal(err)
	}
	if j.Currently != 3 || j.TotalFailed != 812 || j.CurrentlyBan != 2 || j.TotalBanned != 57 {
		t.Fatalf("counters misread: %+v", j)
	}
	if strings.Join(j.BannedIPs, ",") != "192.0.2.5,198.51.100.7" {
		t.Errorf("banned = %v", j.BannedIPs)
	}
	if strings.Join(j.FileList, ",") != "/var/log/auth.log" {
		t.Errorf("files = %v", j.FileList)
	}
}

// `fail2ban-client status` names the running jails on one line.
func TestParseJailListReadsTheRealStatus(t *testing.T) {
	got := parseJailList("Status\n|- Number of jail:\t2\n`- Jail list:\tsshd, nginx-http-auth\n")
	if strings.Join(got, ",") != "sshd,nginx-http-auth" {
		t.Fatalf("got %v", got)
	}
}
