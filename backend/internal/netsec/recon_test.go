package netsec

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
)

// hostPort splits an httptest server URL into the pieces the recon tools take:
// a validated host and a numeric port.
func hostPort(t *testing.T, raw string) (string, int) {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	host, portStr, err := net.SplitHostPort(u.Host)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatal(err)
	}
	return host, port
}

// The scan set is what an operator will read as "which of my services answer",
// so a UDP port reported closed on a working service is a false claim of
// exactly the kind this package pins elsewhere.
func TestScanPortsAreTCPOnlyAndDeduped(t *testing.T) {
	ports := scanPorts()
	if len(ports) == 0 {
		t.Fatal("no ports to scan")
	}
	seen := map[string]bool{}
	for _, p := range ports {
		if p.Protocol != "tcp" {
			t.Errorf("non-TCP port in the scan set: %s (%s/%s)", p.Name, p.Port, p.Protocol)
		}
		if seen[p.Port] {
			t.Errorf("port %s appears more than once", p.Port)
		}
		seen[p.Port] = true
	}
	// 443 is in the catalogue twice — HTTPS over TCP and HTTP/3 over UDP — and
	// must survive as exactly the TCP one.
	if !seen["443"] {
		t.Error("443 missing from the scan set")
	}
	for _, key := range []string{"dns", "http3", "wireguard", "tailscale"} {
		for _, p := range ports {
			if p.Key == key {
				t.Errorf("UDP service %q reached the TCP scan set", key)
			}
		}
	}
}

func TestHTTPCheckReportsStatusRedirectAndHeaders(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/final", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Server", "recon-test")
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/final", http.StatusMovedPermanently)
	})
	srv := httptest.NewTLSServer(mux)
	defer srv.Close()

	host, port := hostPort(t, srv.URL)
	res, err := New().HTTPCheck(context.Background(), host, port)
	if err != nil {
		t.Fatal(err)
	}
	if !res.OK {
		t.Fatalf("not ok: output=%q error=%q", res.Output, res.Error)
	}
	for _, want := range []string{"301", "/final", "200", "recon-test", "TLS tool"} {
		if !strings.Contains(res.Output, want) {
			t.Errorf("output missing %q:\n%s", want, res.Output)
		}
	}
}

func TestTLSCertInspectsThePresentedCertificate(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer srv.Close()

	host, port := hostPort(t, srv.URL)
	res, err := New().TLSCert(context.Background(), host, port)
	if err != nil {
		t.Fatal(err)
	}
	if !res.OK {
		t.Fatalf("not ok: output=%q error=%q", res.Output, res.Error)
	}
	if !strings.Contains(res.Output, "TLS 1.") {
		t.Errorf("no negotiated version:\n%s", res.Output)
	}
	if !strings.Contains(res.Output, "Subject:") {
		t.Errorf("no subject line:\n%s", res.Output)
	}
	// httptest signs its own certificate, so it is not in the system roots and
	// must be reported as untrusted rather than silently passed.
	if !strings.Contains(res.Output, "Not trusted") {
		t.Errorf("a self-signed certificate was reported as trusted:\n%s", res.Output)
	}
}

// A bad target must be refused before anything runs, for every tool that will
// put it on a command line or a connection — the same guard the existing
// probes carry.
func TestReconToolsRejectBadTargets(t *testing.T) {
	s := New()
	ctx := context.Background()
	bad := "-i example.com"
	if _, err := s.HTTPCheck(ctx, bad, 0); err == nil {
		t.Error("HTTPCheck accepted a bad target")
	}
	if _, err := s.TLSCert(ctx, bad, 0); err == nil {
		t.Error("TLSCert accepted a bad target")
	}
	if _, err := s.PortScan(ctx, bad); err == nil {
		t.Error("PortScan accepted a bad target")
	}
	if _, err := s.Whois(ctx, bad); err == nil {
		t.Error("Whois accepted a bad target")
	}
}
