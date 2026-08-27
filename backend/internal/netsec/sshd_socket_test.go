package netsec

import (
	"strings"
	"testing"
)

// Copied from `systemctl show ssh.socket -p Listen` on Ubuntu 25.04, where
// socket activation is the default and ssh.service is disabled.
const listenProperties = `Listen=0.0.0.0:22 (Stream)
Listen=[::]:22 (Stream)
`

func TestParseListenPortsReadsSystemdsFormat(t *testing.T) {
	ports := parseListenPorts(listenProperties)
	// A dual-stack socket names the same port twice, and "22, 22" on the page
	// reads as a misconfiguration rather than as one listener.
	if strings.Join(ports, ",") != "22" {
		t.Fatalf("ports = %v, want one 22", ports)
	}
}

func TestParseListenPortsHandlesABareIPv6Address(t *testing.T) {
	ports := parseListenPorts("Listen=[::]:2222 (Stream)\nListen=/run/sshd.sock (Sequential)\n")
	if strings.Join(ports, ",") != "2222" {
		t.Fatalf("ports = %v, want 2222 and nothing from the unix socket", ports)
	}
}

func TestParseListenPortsIgnoresEverythingElse(t *testing.T) {
	if got := parseListenPorts("Listen=\n"); len(got) != 0 {
		t.Fatalf("ports = %v, want none", got)
	}
}

// ListenStream is a list systemd appends to. Without the empty assignment
// first, a socket moved to 2222 goes on answering on 22 as well — which is a
// port move that leaves the old port open, the one outcome the change was made
// to avoid.
func TestSocketDropInClearsTheInheritedListenList(t *testing.T) {
	file := socketDropIn("ssh.socket", "2222")
	lines := []string{}
	for _, l := range strings.Split(file, "\n") {
		if strings.HasPrefix(l, "ListenStream=") {
			lines = append(lines, l)
		}
	}
	want := []string{"ListenStream=", "ListenStream=2222"}
	if strings.Join(lines, "|") != strings.Join(want, "|") {
		t.Fatalf("ListenStream lines = %v, want %v", lines, want)
	}
	if !strings.Contains(file, "[Socket]") {
		t.Error("no [Socket] section, so systemd would refuse the drop-in")
	}
	if !strings.Contains(file, "ssh.socket") {
		t.Error("the drop-in does not name the unit it belongs to")
	}
}

func TestSSHSocketActiveOnlyWhenAUnitWasFound(t *testing.T) {
	if (SSHSocket{}).Active() {
		t.Error("a host with no socket unit reported as socket-activated")
	}
	if !(SSHSocket{Unit: "ssh.socket"}).Active() {
		t.Error("a named unit reported as absent")
	}
}
