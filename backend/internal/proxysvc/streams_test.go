package proxysvc

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func tcpStream() *StreamSpec {
	return &StreamSpec{
		Name: "postgres-replica", Listen: 5432, Protocol: "tcp",
		Upstream: "10.0.0.5:5432", AllowFrom: []string{"10.0.0.0/8"},
	}
}

func TestValidateStream(t *testing.T) {
	if err := ValidateStream(tcpStream()); err != nil {
		t.Fatalf("rejected a valid stream: %v", err)
	}
	cases := []struct {
		name   string
		mutate func(*StreamSpec)
	}{
		{"bad name", func(s *StreamSpec) { s.Name = "../x" }},
		{"port out of range", func(s *StreamSpec) { s.Listen = 0 }},
		{"unknown protocol", func(s *StreamSpec) { s.Protocol = "sctp" }},
		{"upstream with no port", func(s *StreamSpec) { s.Upstream = "10.0.0.5" }},
		{"upstream with an injected directive", func(s *StreamSpec) { s.Upstream = "10.0.0.5:5432; root /" }},
		{"acl entry that is not an address", func(s *StreamSpec) { s.AllowFrom = []string{"office"} }},
		{"timeout out of range", func(s *StreamSpec) { s.Timeout = 999999 }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			spec := tcpStream()
			tc.mutate(spec)
			if err := ValidateStream(spec); err == nil {
				t.Fatal("accepted")
			}
		})
	}
}

func TestValidateStreamDefaultsToTCP(t *testing.T) {
	spec := tcpStream()
	spec.Protocol = ""
	if err := ValidateStream(spec); err != nil {
		t.Fatal(err)
	}
	if spec.Protocol != "tcp" {
		t.Fatalf("protocol = %q", spec.Protocol)
	}
}

func TestRenderStreamTCP(t *testing.T) {
	out, err := RenderStream(tcpStream())
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		managedMarker, "listen 5432;", "server 10.0.0.5:5432;",
		"proxy_pass postgres_replica_backend;", "allow 10.0.0.0/8;", "deny all;",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q from:\n%s", want, out)
		}
	}
	// An allow list with no deny after it allows everybody, which is the same
	// trap the site renderer guards against.
	if strings.Index(out, "allow 10.0.0.0/8;") > strings.Index(out, "deny all;") {
		t.Error("allow must come before deny all")
	}
}

// A dash is legal in a file name and not in an nginx identifier, so the
// upstream block name has to be translated or the config will not parse.
func TestRenderStreamMakesALegalUpstreamName(t *testing.T) {
	out, _ := RenderStream(tcpStream())
	if strings.Contains(out, "upstream postgres-replica_backend") {
		t.Fatalf("dash left in an nginx identifier:\n%s", out)
	}
	if !strings.Contains(out, "upstream postgres_replica_backend {") {
		t.Fatalf("upstream block missing:\n%s", out)
	}
}

func TestRenderStreamUDP(t *testing.T) {
	spec := tcpStream()
	spec.Protocol = "udp"
	spec.Listen = 5353
	out, err := RenderStream(spec)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "listen 5353 udp;") {
		t.Fatalf("udp listener missing:\n%s", out)
	}
	if !strings.Contains(out, "proxy_responses 1;") {
		t.Fatalf("udp needs its own session rule:\n%s", out)
	}
}

func TestStreamRoundTrip(t *testing.T) {
	original := tcpStream()
	original.ProxyProtocol = true
	original.Timeout = 300
	out, err := RenderStream(original)
	if err != nil {
		t.Fatal(err)
	}
	parsed := ParseStreamSpec("postgres-replica", out)
	if parsed.Listen != 5432 || parsed.Protocol != "tcp" {
		t.Fatalf("listener lost: %+v", parsed)
	}
	if parsed.Upstream != "10.0.0.5:5432" {
		t.Fatalf("upstream = %q", parsed.Upstream)
	}
	if !parsed.ProxyProtocol || parsed.Timeout != 300 {
		t.Fatalf("options lost: %+v", parsed)
	}
	if len(parsed.AllowFrom) != 1 || parsed.AllowFrom[0] != "10.0.0.0/8" {
		t.Fatalf("allow list lost: %v", parsed.AllowFrom)
	}
	if err := ValidateStream(parsed); err != nil {
		t.Fatalf("a parsed stream should still be valid: %v", err)
	}
}

// The v4 and v6 listen lines carry the same port; reading both would leave the
// form showing whichever came last rather than one value.
func TestParseStreamSpecReadsOnePort(t *testing.T) {
	spec := ParseStreamSpec("x", "server {\n listen 5432;\n listen [::]:5432;\n proxy_pass a_backend;\n}\n")
	if spec.Listen != 5432 {
		t.Fatalf("listen = %d", spec.Listen)
	}
}

func TestStreamWarnings(t *testing.T) {
	open := tcpStream()
	open.AllowFrom = nil
	warnings := streamWarnings(open)
	if len(warnings) < 2 {
		t.Fatalf("an unrestricted database stream should warn twice: %v", warnings)
	}
	if !containsSubstring(warnings, "no authentication") {
		t.Error("the absence of any auth on a raw stream is the headline")
	}
	if !containsSubstring(warnings, "database") {
		t.Error("the port catalogue's judgement should carry over to streams")
	}

	if got := streamWarnings(tcpStream()); len(got) != 0 {
		t.Errorf("a source-restricted stream warned anyway: %v", got)
	}

	udp := tcpStream()
	udp.Protocol, udp.ProxyProtocol = "udp", true
	if !containsSubstring(streamWarnings(udp), "PROXY protocol is a TCP thing") {
		t.Error("PROXY protocol on UDP silently does nothing and should say so")
	}
}

// The snippet this page prints is exactly what people paste into nginx.conf
// commented out while they think about it. A banner that disappears then is
// worse than none: the files go on being written and silently ignored.
func TestStreamIncludeIgnoresACommentedOutInclude(t *testing.T) {
	dir := t.TempDir()
	streams := filepath.Join(dir, "stream.d")
	write := func(body string) {
		if err := os.WriteFile(filepath.Join(dir, "nginx.conf"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("# stream {\n#     include " + streams + "/*.conf;\n# }\nhttp { }\n")
	if streamIncludeFound(dir, streams) {
		t.Error("a commented-out include was read as present")
	}
	write("stream {\n    include " + streams + "/*.conf;\n}\nhttp { }\n")
	if !streamIncludeFound(dir, streams) {
		t.Error("a real include was not found")
	}
}

// The stream directory hangs off the configured nginx directory, so a host
// with JD_NGINX_DIR set somewhere else does not write into /etc/nginx.
func TestStreamsLiveUnderTheConfiguredNginxDir(t *testing.T) {
	dir := t.TempDir()
	svc := New(dir, filepath.Join(t.TempDir(), "Caddyfile"))
	if got := svc.Streams(context.Background()).Dir; got != filepath.Join(dir, "stream.d") {
		t.Fatalf("stream dir is %s", got)
	}
}

// "New stream" and "Edit this stream" post to one route, so without the guard
// a new one named after an existing one replaced it in silence — a forwarding
// rule that quietly stopped pointing where it used to.
func TestApplyStreamRefusesToReplaceWithoutOverwrite(t *testing.T) {
	dir := t.TempDir()
	svc := New(dir, filepath.Join(t.TempDir(), "Caddyfile"))
	if err := os.MkdirAll(svc.streamDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(svc.streamDir(), "postgres-replica.conf")
	if err := os.WriteFile(path, []byte("# existing\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ApplyStream(context.Background(), tcpStream(), false, false); err == nil {
		t.Fatal("an existing stream was replaced without asking")
	}
	b, err := os.ReadFile(path)
	if err != nil || string(b) != "# existing\n" {
		t.Fatalf("the existing file was touched: %q %v", b, err)
	}
}
