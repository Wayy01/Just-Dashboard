package netsec

import (
	"testing"
)

func TestSummarisePeersFoldsByAddress(t *testing.T) {
	out := SummarisePeers([]Connection{
		{Protocol: "tcp", LocalPort: 443, RemoteAddr: "203.0.113.9", Status: "ESTABLISHED", Process: "nginx"},
		{Protocol: "tcp", LocalPort: 443, RemoteAddr: "203.0.113.9", Status: "TIME_WAIT", Process: "nginx"},
		{Protocol: "tcp", LocalPort: 22, RemoteAddr: "203.0.113.9", Status: "ESTABLISHED", Process: "sshd"},
		{Protocol: "tcp", LocalPort: 443, RemoteAddr: "198.51.100.7", Status: "ESTABLISHED", Process: "nginx"},
	})
	if len(out.Peers) != 2 {
		t.Fatalf("got %d peers, want 2", len(out.Peers))
	}
	// Busiest first: the address holding three sockets is the one worth
	// looking at, and sorting by address would bury it.
	top := out.Peers[0]
	if top.Address != "203.0.113.9" || top.Count != 3 || top.Established != 2 {
		t.Fatalf("top peer = %+v", top)
	}
	if len(top.Ports) != 2 || top.Ports[0] != 22 {
		t.Errorf("ports = %v, want them sorted", top.Ports)
	}
	if len(top.Processes) != 2 {
		t.Errorf("processes = %v", top.Processes)
	}
}

func TestSummarisePeersSeparatesLoopback(t *testing.T) {
	out := SummarisePeers([]Connection{
		{Protocol: "tcp", LocalPort: 5432, RemoteAddr: "127.0.0.1", Status: "ESTABLISHED"},
		{Protocol: "tcp", LocalPort: 443, RemoteAddr: "203.0.113.9", Status: "ESTABLISHED"},
	})
	if out.Loopback != 1 {
		t.Errorf("loopback = %d", out.Loopback)
	}
	if len(out.Peers) != 1 {
		t.Fatalf("loopback should not appear as a peer: %+v", out.Peers)
	}
	if out.Total != 2 {
		t.Errorf("total = %d; the folded ones still count", out.Total)
	}
}

// Most of a healthy host's connections are private, and being able to say so
// is what makes the public ones worth looking at.
func TestSummarisePeersMarksPrivateAddresses(t *testing.T) {
	out := SummarisePeers([]Connection{
		{Protocol: "tcp", LocalPort: 443, RemoteAddr: "10.1.2.3"},
		{Protocol: "tcp", LocalPort: 443, RemoteAddr: "100.101.102.103"},
		{Protocol: "tcp", LocalPort: 443, RemoteAddr: "203.0.113.9"},
	})
	byAddr := map[string]Peer{}
	for _, p := range out.Peers {
		byAddr[p.Address] = p
	}
	if !byAddr["10.1.2.3"].Private {
		t.Error("RFC1918 not marked private")
	}
	if !byAddr["100.101.102.103"].Private {
		t.Error("a tailnet address is not the internet")
	}
	if byAddr["203.0.113.9"].Private {
		t.Error("a public address marked private")
	}
}

func TestSummarisePeersNamesTheService(t *testing.T) {
	out := SummarisePeers([]Connection{{Protocol: "tcp", LocalPort: 5432, RemoteAddr: "203.0.113.9"}})
	if out.Peers[0].Service != "PostgreSQL" {
		t.Errorf("service = %q", out.Peers[0].Service)
	}
}

// The two figures under the table have to add up, and they did not while
// "listening" was computed by subtraction: an unconnected datagram socket has
// no peer and is not a listener, so it was counted as one.
func TestSummarisePeersTotalsAddUp(t *testing.T) {
	conns := []Connection{
		{Protocol: "tcp", LocalPort: 443, RemoteAddr: "203.0.113.9"},
		{Protocol: "tcp", LocalPort: 443, RemoteAddr: "127.0.0.1"},
	}
	out := SummarisePeers(conns)
	peered := 0
	for _, p := range out.Peers {
		peered += p.Count
	}
	if peered+out.Loopback != out.Total {
		t.Fatalf("%d in peers + %d loopback != %d total", peered, out.Loopback, out.Total)
	}
}
