package proxysvc

import (
	"context"
	"fmt"
	"sort"

	"github.com/shirou/gopsutil/v4/net"
	"github.com/shirou/gopsutil/v4/process"
)

// Listener is one socket the host is accepting on, joined to the process that
// owns it. "What is listening on 8080 and who started it" is the question this
// answers, and it is the first question during an incident.
type Listener struct {
	Protocol string `json:"protocol"`
	Address  string `json:"address"`
	Port     uint32 `json:"port"`
	PID      int32  `json:"pid"`
	Process  string `json:"process"`
	Cmdline  string `json:"cmdline,omitempty"`
	User     string `json:"user,omitempty"`
	Exposed  bool   `json:"exposed"`
}

// ListListeners enumerates listening sockets. Exposed marks a socket bound to
// a wildcard address rather than loopback — the distinction between "reachable
// from the internet" and "reachable from this machine only".
func ListListeners(ctx context.Context) ([]Listener, error) {
	conns, err := net.ConnectionsWithContext(ctx, "inet")
	if err != nil {
		return nil, err
	}
	cache := map[int32]*process.Process{}
	out := []Listener{}
	seen := map[string]bool{}
	for _, c := range conns {
		// Type 2 is SOCK_DGRAM: UDP has no LISTEN state, so a bound socket is
		// the closest thing to a listener there is. A *connected* one is not —
		// every DNS lookup on the host opens one, and listing those turned a
		// page about what the server is accepting on into a page of the
		// machine's own outbound traffic.
		if c.Status != "LISTEN" && !(c.Type == 2 && c.Raddr.Port == 0) {
			continue
		}
		proto := "tcp"
		if c.Type == 2 {
			proto = "udp"
		}
		key := fmt.Sprintf("%s|%s|%d|%d", proto, c.Laddr.IP, c.Laddr.Port, c.Pid)
		if seen[key] {
			continue
		}
		seen[key] = true

		l := Listener{
			Protocol: proto,
			Address:  c.Laddr.IP,
			Port:     c.Laddr.Port,
			PID:      c.Pid,
			Exposed:  isWildcard(c.Laddr.IP),
		}
		if c.Pid > 0 {
			p, ok := cache[c.Pid]
			if !ok {
				if np, err := process.NewProcessWithContext(ctx, c.Pid); err == nil {
					p = np
					cache[c.Pid] = np
				}
			}
			if p != nil {
				l.Process, _ = p.NameWithContext(ctx)
				l.Cmdline, _ = p.CmdlineWithContext(ctx)
				l.User, _ = p.UsernameWithContext(ctx)
			}
		}
		out = append(out, l)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Port != out[j].Port {
			return out[i].Port < out[j].Port
		}
		return out[i].Protocol < out[j].Protocol
	})
	return out, nil
}

func isWildcard(ip string) bool {
	return ip == "0.0.0.0" || ip == "::" || ip == "*" || ip == ""
}
