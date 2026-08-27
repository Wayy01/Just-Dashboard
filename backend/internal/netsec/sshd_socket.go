package netsec

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/Wayy01/Just-Dashboard/backend/internal/hostexec"
)

// Where sshd listens is not always sshd's decision.
//
// Ubuntu has shipped socket-activated SSH since 22.10, and it is the default on
// 24.04 and later: ssh.service is disabled, ssh.socket holds the listener, and
// the port lives in the unit's ListenStream. sshd never binds anything, so the
// Port directive in sshd_config is read, resolved, reported by `sshd -T` — and
// completely ignored.
//
// That made the port control the one setting on this page that reported success
// and did nothing. `sshd -t` passes, the reload succeeds, `sshd -T` reads back
// the new value, and the machine goes on answering on 22. It is exactly the
// class of failure the rest of this file exists to prevent, so the socket unit
// is read alongside the daemon and written alongside it.

// managedSocketDropIn is where the port override goes. A drop-in rather than an
// edit of the packaged unit: it can be read, diffed and deleted as one thing,
// and a distribution upgrade replacing the unit does not silently take the
// operator's port with it.
const managedSocketDropIn = "10-just-dashboard.conf"

// socketUnits are the names the socket has worn. ssh.socket is Debian and
// Ubuntu; sshd.socket is what a hand-written unit on the RPM side is usually
// called.
var socketUnits = []string{"ssh.socket", "sshd.socket"}

var listenStreamRe = regexp.MustCompile(`^Listen=(.*?) \(Stream\)$`)

// SSHSocket describes a socket unit standing in front of sshd.
type SSHSocket struct {
	// Unit is empty when nothing of the kind is running, which is the case on
	// every host where sshd binds its own port.
	Unit string `json:"unit,omitempty"`
	// Ports are what the socket actually listens on. This is the answer to
	// "which port is SSH on" wherever it is non-empty — sshd_config's Port is
	// not.
	Ports []string `json:"ports,omitempty"`
	// DropIn is the file a port change would be written to.
	DropIn string `json:"dropIn,omitempty"`
}

// Active reports whether a socket owns the listener.
func (s SSHSocket) Active() bool { return s.Unit != "" }

// readSSHSocket asks systemd whether a socket unit is holding SSH's port.
//
// Both halves are needed. `is-active` alone would report a unit that exists but
// is not listening; the Listen properties alone would report a socket that is
// enabled for the next boot but inert now, and only one of those two is the
// reason a port change would be ignored.
func readSSHSocket(ctx context.Context) SSHSocket {
	if !hostexec.AvailableOnHost("systemctl") {
		return SSHSocket{}
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	for _, unit := range socketUnits {
		out, err := hostexec.CommandOnHost(ctx, "systemctl", "is-active", unit).Output()
		if err != nil || strings.TrimSpace(string(out)) != "active" {
			continue
		}
		sock := SSHSocket{Unit: unit, DropIn: socketDropInPath(unit)}
		if props, err := hostexec.CommandOnHost(ctx, "systemctl", "show", unit, "-p", "Listen").Output(); err == nil {
			sock.Ports = parseListenPorts(string(props))
		}
		return sock
	}
	return SSHSocket{}
}

// parseListenPorts reads the addresses out of `systemctl show <unit> -p Listen`:
//
//	Listen=0.0.0.0:22 (Stream)
//	Listen=[::]:22 (Stream)
//
// Only the port is kept, de-duplicated, because a dual-stack socket names the
// same port twice and a page reporting "22, 22" reads as a misconfiguration.
func parseListenPorts(out string) []string {
	seen := map[string]bool{}
	ports := []string{}
	sc := bufio.NewScanner(strings.NewReader(out))
	for sc.Scan() {
		m := listenStreamRe.FindStringSubmatch(strings.TrimSpace(sc.Text()))
		if m == nil {
			continue
		}
		addr := m[1]
		idx := strings.LastIndex(addr, ":")
		if idx < 0 {
			continue
		}
		port := addr[idx+1:]
		if port == "" || seen[port] {
			continue
		}
		seen[port] = true
		ports = append(ports, port)
	}
	return ports
}

func socketDropInPath(unit string) string {
	for _, base := range []string{"/etc/systemd/system", "/host/etc/systemd/system"} {
		if st, err := os.Stat(base); err == nil && st.IsDir() {
			return filepath.Join(base, unit+".d", managedSocketDropIn)
		}
	}
	return ""
}

// socketDropIn is the whole file. ListenStream is a list systemd *appends* to,
// so the empty assignment is not decoration: without it the socket listens on
// the new port and on 22, and an operator who moved SSH to quiet the logs has
// achieved nothing at all.
func socketDropIn(unit, port string) string {
	return fmt.Sprintf(`# Written by Just Dashboard.
#
# This host runs socket-activated SSH: the port belongs to %s, not to
# sshd_config, and sshd never binds a listener of its own. The empty
# ListenStream clears the packaged unit's list first — systemd appends
# otherwise, and the socket would go on answering on 22 as well.
[Socket]
ListenStream=
ListenStream=%s
`, unit, port)
}

// applySocketPort writes the drop-in and restarts the socket.
//
// A restart rather than a reload: systemd rebinds a socket unit's addresses
// only when the unit is restarted, and `daemon-reload` on its own leaves the
// old port bound while reporting that the new configuration was read. Existing
// SSH sessions are separate processes and survive; what does not survive is the
// listener, which is the point.
func applySocketPort(ctx context.Context, sock SSHSocket, port string, out LineWriter) error {
	if sock.DropIn == "" {
		return fmt.Errorf("no writable systemd directory, so %s cannot be moved off its current port", sock.Unit)
	}
	out.Status("Writing %s", sock.DropIn)
	if err := os.MkdirAll(filepath.Dir(sock.DropIn), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(sock.DropIn, []byte(socketDropIn(sock.Unit, port)), 0o644); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	out.Status("Reloading systemd and restarting %s", sock.Unit)
	if b, err := hostexec.CommandOnHost(ctx, "systemctl", "daemon-reload").CombinedOutput(); err != nil {
		return fmt.Errorf("systemctl daemon-reload: %s", strings.TrimSpace(string(b)))
	}
	if b, err := hostexec.CommandOnHost(ctx, "systemctl", "restart", sock.Unit).CombinedOutput(); err != nil {
		return fmt.Errorf("systemctl restart %s: %s", sock.Unit, strings.TrimSpace(string(b)))
	}
	return nil
}
