package netsec

import (
	"strings"
	"testing"
)

func TestPresetFor(t *testing.T) {
	if p, ok := PresetFor("443", "tcp"); !ok || p.Name != "HTTPS" {
		t.Fatalf("443/tcp = %+v %v", p, ok)
	}
	// The same port on UDP is HTTP/3, and the catalogue has to tell them
	// apart or a QUIC rule reads as a duplicate.
	if p, ok := PresetFor("443", "udp"); !ok || !strings.Contains(p.Name, "HTTP/3") {
		t.Fatalf("443/udp = %+v %v", p, ok)
	}
	if _, ok := PresetFor("31337", "tcp"); ok {
		t.Error("an unknown port should not match")
	}
}

func TestDangerousPortsCarryAReason(t *testing.T) {
	for _, key := range []string{"redis", "mysql", "postgres", "docker", "mongodb"} {
		found := false
		for _, p := range ServiceCatalogue {
			if p.Key != key {
				continue
			}
			found = true
			if p.Danger == "" {
				t.Errorf("%s is in the catalogue with no warning attached", key)
			}
		}
		if !found {
			t.Errorf("%s missing from the catalogue", key)
		}
	}
}

func TestParseAppList(t *testing.T) {
	out := strings.Join([]string{
		"Available applications:",
		"  Nginx Full",
		"  Nginx HTTP",
		"  OpenSSH",
	}, "\n")
	got := parseAppList(out)
	if len(got) != 3 || got[0] != "Nginx Full" || got[2] != "OpenSSH" {
		t.Fatalf("got %q", got)
	}
}

func TestParseAppInfo(t *testing.T) {
	out := strings.Join([]string{
		"Profile: Nginx Full",
		"Title: Web Server (Nginx, HTTP + HTTPS)",
		"Description: Small, but very powerful and efficient web server",
		"",
		"Ports:",
		"  80,443/tcp|137,138/udp",
	}, "\n")
	title, description, ports := parseAppInfo(out)
	if title != "Web Server (Nginx, HTTP + HTTPS)" {
		t.Errorf("title = %q", title)
	}
	if description == "" {
		t.Error("description lost")
	}
	// The comma groups a list under one protocol; only the pipe separates
	// entries. Splitting on the comma loses the protocol.
	if len(ports) != 2 || ports[0] != "80,443/tcp" || ports[1] != "137,138/udp" {
		t.Errorf("ports = %q", ports)
	}
}

// A profile name reaches ufw again as an argument, so it is checked against a
// pattern first — the file it came from is writable by root, and root is not
// the same as this code.
func TestAppNamePattern(t *testing.T) {
	for _, ok := range []string{"OpenSSH", "Nginx Full", "WWW Cache"} {
		if !appNameRe.MatchString(ok) {
			t.Errorf("%q rejected", ok)
		}
	}
	for _, bad := range []string{"", "-rf", "a;b", "x\ny", strings.Repeat("a", 65)} {
		if appNameRe.MatchString(bad) {
			t.Errorf("%q accepted", bad)
		}
	}
}
