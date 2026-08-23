package netsec

import (
	"context"
	"net"
	"sort"
	"strconv"
	"strings"

	gnet "github.com/shirou/gopsutil/v4/net"
	"github.com/shirou/gopsutil/v4/process"
)

// Who is talking to this machine right now.
//
// The Ports view answers what is listening; this answers who took it up on the
// offer, which is the question during an incident and the one no panel in this
// class shows. It is deliberately a *summary* by remote address rather than a
// connection list: a busy host holds thousands of sockets and forty of them
// are one client, so a raw table buries the single address with two hundred
// connections underneath four hundred rows of noise.
type Connection struct {
	Protocol   string `json:"protocol"`
	LocalAddr  string `json:"localAddress"`
	LocalPort  uint32 `json:"localPort"`
	RemoteAddr string `json:"remoteAddress"`
	RemotePort uint32 `json:"remotePort"`
	Status     string `json:"status"`
	PID        int32  `json:"pid,omitempty"`
	Process    string `json:"process,omitempty"`
	User       string `json:"user,omitempty"`
}

// Peer is one remote address, with everything it is connected to.
type Peer struct {
	Address string `json:"address"`
	// Count is how many sockets this address holds; Established is how many
	// of them are carrying traffic rather than closing.
	Count       int      `json:"count"`
	Established int      `json:"established"`
	Ports       []uint32 `json:"ports"`
	Processes   []string `json:"processes"`
	// Private marks a peer inside RFC1918, a tailnet or loopback. Most of a
	// healthy host's connections are private, and being able to say so is
	// what makes the public ones worth looking at.
	Private bool `json:"private"`
	// Service names the local port from the catalogue, so "who is on the
	// database" reads as a sentence.
	Service string `json:"service,omitempty"`
}

// Connections summarises the host's current inbound and outbound sockets.
type Connections struct {
	Peers []Peer `json:"peers"`
	// Total is every connection counted, including the ones folded away.
	Total int `json:"total"`
	// Listening is excluded from the peer list and reported separately so the
	// two numbers add up for a reader.
	Listening int `json:"listening"`
	// Loopback is how many never left the machine.
	Loopback int `json:"loopback"`
}

func (s *Service) Connections(ctx context.Context) (*Connections, error) {
	conns, err := gnet.ConnectionsWithContext(ctx, "inet")
	if err != nil {
		return nil, err
	}
	cache := map[int32]string{}
	list := make([]Connection, 0, len(conns))
	listening := 0
	for _, c := range conns {
		if c.Status == "LISTEN" {
			listening++
			continue
		}
		// A socket with no peer is an unconnected datagram socket, not a
		// listener; counting it as one made the two figures fail to add up.
		if c.Raddr.IP == "" {
			continue
		}
		conn := Connection{
			Protocol:   protocolOf(c.Type),
			LocalAddr:  c.Laddr.IP,
			LocalPort:  c.Laddr.Port,
			RemoteAddr: c.Raddr.IP,
			RemotePort: c.Raddr.Port,
			Status:     c.Status,
			PID:        c.Pid,
		}
		if c.Pid > 0 {
			name, ok := cache[c.Pid]
			if !ok {
				if p, err := process.NewProcessWithContext(ctx, c.Pid); err == nil {
					name, _ = p.NameWithContext(ctx)
				}
				cache[c.Pid] = name
			}
			conn.Process = name
		}
		list = append(list, conn)
	}
	out := SummarisePeers(list)
	out.Listening = listening
	return out, nil
}

func protocolOf(sockType uint32) string {
	if sockType == 2 { // SOCK_DGRAM
		return "udp"
	}
	return "tcp"
}

// SummarisePeers folds a connection list by remote address.
func SummarisePeers(conns []Connection) *Connections {
	out := &Connections{Peers: []Peer{}, Total: len(conns)}
	byAddr := map[string]*Peer{}
	ports := map[string]map[uint32]bool{}
	procs := map[string]map[string]bool{}
	for _, c := range conns {
		ip := net.ParseIP(c.RemoteAddr)
		if ip != nil && ip.IsLoopback() {
			out.Loopback++
			continue
		}
		p, ok := byAddr[c.RemoteAddr]
		if !ok {
			p = &Peer{Address: c.RemoteAddr, Ports: []uint32{}, Processes: []string{}}
			p.Private = ip != nil && (isPrivate(ip) || tailscaleNet.Contains(ip))
			byAddr[c.RemoteAddr] = p
			ports[c.RemoteAddr] = map[uint32]bool{}
			procs[c.RemoteAddr] = map[string]bool{}
		}
		p.Count++
		if strings.EqualFold(c.Status, "ESTABLISHED") {
			p.Established++
		}
		ports[c.RemoteAddr][c.LocalPort] = true
		if c.Process != "" {
			procs[c.RemoteAddr][c.Process] = true
		}
	}
	for addr, p := range byAddr {
		for port := range ports[addr] {
			p.Ports = append(p.Ports, port)
		}
		sort.Slice(p.Ports, func(i, j int) bool { return p.Ports[i] < p.Ports[j] })
		for name := range procs[addr] {
			p.Processes = append(p.Processes, name)
		}
		sort.Strings(p.Processes)
		// Named after the first local port, which for an inbound connection
		// is the service being used. Ambiguous for an outbound one, and the
		// UI shows both ends, so the name is a hint rather than a claim.
		if len(p.Ports) > 0 {
			if preset, ok := PresetFor(portString(p.Ports[0]), ""); ok {
				p.Service = preset.Name
			}
		}
		out.Peers = append(out.Peers, *p)
	}
	// Busiest first: the address holding forty sockets is the one worth a
	// look, and a list sorted by address makes it invisible.
	sort.Slice(out.Peers, func(i, j int) bool {
		a, b := out.Peers[i], out.Peers[j]
		if a.Count != b.Count {
			return a.Count > b.Count
		}
		return a.Address < b.Address
	})
	return out
}

func portString(p uint32) string {
	return strconv.FormatUint(uint64(p), 10)
}
