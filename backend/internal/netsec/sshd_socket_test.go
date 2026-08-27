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

func TestParseListenAddressesReadsSystemdsFormat(t *testing.T) {
	addrs := parseListenAddresses(listenProperties)
	if strings.Join(addrs, "|") != "0.0.0.0:22|[::]:22" {
		t.Fatalf("addresses = %v", addrs)
	}
	// A dual-stack socket names the same port twice, and "22, 22" on the page
	// reads as a misconfiguration rather than as one listener.
	if strings.Join(portsOf(addrs), ",") != "22" {
		t.Fatalf("ports = %v, want one 22", portsOf(addrs))
	}
}

// A unit may also carry a unix socket, which has no port and is not what
// "which port is SSH on" is asking about.
func TestParseListenAddressesIgnoresNonPortSockets(t *testing.T) {
	addrs := parseListenAddresses("Listen=[::]:2222 (Stream)\nListen=/run/sshd.sock (Sequential)\n")
	if strings.Join(portsOf(addrs), ",") != "2222" {
		t.Fatalf("ports = %v", portsOf(addrs))
	}
}

// The port changes and nothing else does. A socket bound to one interface must
// not become one bound to every interface because somebody moved the port.
func TestRewritePortKeepsTheAddresses(t *testing.T) {
	got := rewritePort([]string{"0.0.0.0:22", "[::]:22", "192.0.2.10:22"}, "2222")
	want := "0.0.0.0:2222|[::]:2222|192.0.2.10:2222"
	if strings.Join(got, "|") != want {
		t.Fatalf("got %v, want %v", got, want)
	}
}

// With nothing readable, both families are named — because a bare
// `ListenStream=<port>` binds the IPv6 wildcard, and under the
// BindIPv6Only=ipv6-only that ssh.socket sets, that takes IPv4 SSH off the
// machine.
func TestRewritePortNamesBothFamiliesWhenNothingWasRead(t *testing.T) {
	got := rewritePort(nil, "2222")
	if strings.Join(got, "|") != "0.0.0.0:2222|[::]:2222" {
		t.Fatalf("got %v", got)
	}
}

// ListenStream is a list systemd appends to. Without the empty assignment
// first, a socket moved to 2222 goes on answering on 22 as well — a port move
// that leaves the old port open.
func TestSocketDropInClearsTheInheritedListenList(t *testing.T) {
	file := socketDropIn("ssh.socket", "2222", []string{"0.0.0.0:22", "[::]:22"})
	lines := []string{}
	for _, l := range strings.Split(file, "\n") {
		if strings.HasPrefix(l, "ListenStream=") {
			lines = append(lines, l)
		}
	}
	want := []string{"ListenStream=", "ListenStream=0.0.0.0:2222", "ListenStream=[::]:2222"}
	if strings.Join(lines, "|") != strings.Join(want, "|") {
		t.Fatalf("ListenStream lines = %v, want %v", lines, want)
	}
	// Naming both families is only legal with this set, and the base unit may
	// not have set it.
	if !strings.Contains(file, "BindIPv6Only=ipv6-only") {
		t.Error("the two addresses will collide and the socket will fail to start")
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
