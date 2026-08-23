package proxysvc

import (
	"strings"
	"testing"
)

func proxySpec() *SiteSpec {
	return &SiteSpec{
		Name: "app", Kind: "proxy", Domains: []string{"app.example.com"},
		Upstream: "http://127.0.0.1:3000",
		TLS:      true, ForceHTTPS: true, HTTP2: true, HSTS: true, SecurityHeaders: true,
		CertPath:   "/etc/letsencrypt/live/app.example.com/fullchain.pem",
		KeyPath:    "/etc/letsencrypt/live/app.example.com/privkey.pem",
		WebSockets: true, Gzip: true, AccessLog: true,
		ClientMaxBody: "50m", ProxyTimeout: 60,
		AllowFrom: []string{}, DenyFrom: []string{}, Locations: []SiteLocation{},
	}
}

func TestValidateSpecAcceptsATypicalSite(t *testing.T) {
	if err := ValidateSpec(proxySpec()); err != nil {
		t.Fatalf("rejected: %v", err)
	}
}

func TestValidateSpecRejections(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*SiteSpec)
	}{
		{"no domains", func(s *SiteSpec) { s.Domains = nil }},
		{"bad domain", func(s *SiteSpec) { s.Domains = []string{"not a domain"} }},
		{"name with a slash", func(s *SiteSpec) { s.Name = "../etc/passwd" }},
		{"upstream with a semicolon", func(s *SiteSpec) { s.Upstream = "http://127.0.0.1:3000; root /" }},
		{"upstream with no scheme", func(s *SiteSpec) { s.Upstream = "127.0.0.1:3000" }},
		{"TLS with no certificate", func(s *SiteSpec) { s.CertPath = "" }},
		{"relative certificate path", func(s *SiteSpec) { s.CertPath = "certs/fullchain.pem" }},
		{"upload limit that is not a size", func(s *SiteSpec) { s.ClientMaxBody = "lots" }},
		{"timeout out of range", func(s *SiteSpec) { s.ProxyTimeout = 99999 }},
		{"acl entry that is not an address", func(s *SiteSpec) { s.AllowFrom = []string{"office"} }},
		{"unknown kind", func(s *SiteSpec) { s.Kind = "magic" }},
		{"unbalanced braces in the escape hatch", func(s *SiteSpec) { s.Custom = "location / {" }},
		{"location with nowhere to go", func(s *SiteSpec) {
			s.Locations = []SiteLocation{{Path: "/api"}}
		}},
		{"location path that is not a path", func(s *SiteSpec) {
			s.Locations = []SiteLocation{{Path: "api", Upstream: "http://127.0.0.1:4000"}}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			spec := proxySpec()
			tc.mutate(spec)
			if err := ValidateSpec(spec); err == nil {
				t.Fatal("accepted")
			}
		})
	}
}

// A newline in a scalar would put a directive of the caller's choosing into
// the file, past the person reading the form.
func TestValidateSpecRefusesNewlinesInScalars(t *testing.T) {
	spec := proxySpec()
	spec.Upstream = "http://127.0.0.1:3000\n    root /;"
	if err := ValidateSpec(spec); err == nil {
		t.Fatal("accepted an upstream carrying a second directive")
	}
}

func TestRenderNginxProxySite(t *testing.T) {
	out, err := RenderNginx(proxySpec())
	if err != nil {
		t.Fatal(err)
	}
	must := []string{
		managedMarker,
		"listen 443 ssl;",
		"http2 on;",
		"server_name app.example.com;",
		"ssl_certificate     /etc/letsencrypt/live/app.example.com/fullchain.pem;",
		"ssl_protocols TLSv1.2 TLSv1.3;",
		"add_header Strict-Transport-Security",
		"add_header X-Content-Type-Options nosniff always;",
		"client_max_body_size 50m;",
		"gzip on;",
		"proxy_pass http://127.0.0.1:3000;",
		"proxy_set_header X-Forwarded-For   $proxy_add_x_forwarded_for;",
		"proxy_set_header Upgrade    $http_upgrade;",
		"proxy_read_timeout    60s;",
		"return 301 https://$host$request_uri;",
		"location /.well-known/acme-challenge/ {",
	}
	for _, want := range must {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q from:\n%s", want, out)
		}
	}
	if strings.Count(out, "server {") != 2 {
		t.Errorf("a forced-HTTPS site needs a redirect server and a TLS one:\n%s", out)
	}
}

// nginx 1.25 deprecated the listen parameter and warns on every reload.
func TestRenderUsesTheHTTP2Directive(t *testing.T) {
	out, _ := RenderNginx(proxySpec())
	if strings.Contains(out, "listen 443 ssl http2") {
		t.Error("used the deprecated listen parameter")
	}
}

// Certbot proves control over plain HTTP. A redirect that swallows the
// challenge path stops renewal working, and nobody finds out for sixty days.
func TestRenderKeepsTheACMEChallengeReachable(t *testing.T) {
	out, _ := RenderNginx(proxySpec())
	acme := strings.Index(out, "/.well-known/acme-challenge/")
	redirect := strings.Index(out, "return 301 https://$host$request_uri;")
	if acme < 0 || redirect < 0 || acme > redirect {
		t.Fatalf("the challenge location must come before the catch-all redirect:\n%s", out)
	}
}

func TestRenderStaticSite(t *testing.T) {
	spec := proxySpec()
	spec.Kind, spec.Upstream, spec.Root = "static", "", "/var/www/site"
	out, err := RenderNginx(spec)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "root /var/www/site;") || !strings.Contains(out, "try_files $uri $uri/ =404;") {
		t.Fatalf("static site not rendered:\n%s", out)
	}
	if strings.Contains(out, "proxy_pass") {
		t.Error("a static site should not proxy")
	}
}

func TestRenderRedirectSite(t *testing.T) {
	spec := &SiteSpec{
		Name: "old", Kind: "redirect", Domains: []string{"old.example.com"},
		RedirectTo: "https://new.example.com", Permanent: true,
	}
	out, err := RenderNginx(spec)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "return 301 https://new.example.com$request_uri;") {
		t.Fatalf("redirect not rendered:\n%s", out)
	}
}

func TestRenderAccessControls(t *testing.T) {
	spec := proxySpec()
	spec.AllowFrom = []string{"10.0.0.0/8"}
	spec.DenyFrom = []string{"all"}
	spec.BasicAuthFile = "/etc/nginx/.htpasswd"
	spec.BasicAuthRealm = "Staging"
	out, _ := RenderNginx(spec)
	for _, want := range []string{
		"allow 10.0.0.0/8;", "deny all;",
		`auth_basic "Staging";`, "auth_basic_user_file /etc/nginx/.htpasswd;",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q from:\n%s", want, out)
		}
	}
	// Order is the whole meaning: nginx stops at the first match, so an allow
	// after a deny all never runs.
	if strings.Index(out, "allow 10.0.0.0/8;") > strings.Index(out, "deny all;") {
		t.Error("allow rules must be rendered before deny all")
	}
}

func TestRenderExtraLocations(t *testing.T) {
	spec := proxySpec()
	spec.Locations = []SiteLocation{{Path: "/api", Upstream: "http://127.0.0.1:4000"}}
	out, _ := RenderNginx(spec)
	if !strings.Contains(out, "location /api {") || !strings.Contains(out, "proxy_pass http://127.0.0.1:4000;") {
		t.Fatalf("extra location missing:\n%s", out)
	}
	// nginx matches the longest prefix regardless of order, but a reader
	// expects the specific one first. Measured inside the TLS server block:
	// the redirect block above it has a catch-all of its own.
	tls := out[strings.LastIndex(out, "server {"):]
	if strings.Index(tls, "location /api {") > strings.Index(tls, "location / {") {
		t.Error("the specific location should be rendered before the catch-all")
	}
}

func TestSpecWarnings(t *testing.T) {
	plain := proxySpec()
	plain.TLS, plain.ForceHTTPS, plain.HSTS = false, false, false
	if len(SpecWarnings(plain)) == 0 {
		t.Error("a plain-HTTP site should warn")
	}

	noRedirect := proxySpec()
	noRedirect.ForceHTTPS = false
	if !containsSubstring(SpecWarnings(noRedirect), "Plain HTTP still serves") {
		t.Error("TLS without a redirect should warn")
	}

	danglingAllow := proxySpec()
	danglingAllow.AllowFrom = []string{"10.0.0.0/8"}
	if !containsSubstring(SpecWarnings(danglingAllow), "allows everybody") {
		t.Error("an allow list with no deny all allows everybody, and should say so")
	}

	if len(SpecWarnings(proxySpec())) != 0 {
		t.Errorf("a correct spec warned anyway: %v", SpecWarnings(proxySpec()))
	}
}

func TestIsPublicUpstream(t *testing.T) {
	if isPublicUpstream("http://127.0.0.1:3000") || isPublicUpstream("http://10.0.0.4:8080") {
		t.Error("a local upstream is not public")
	}
	if isPublicUpstream("http://api-container:8080") {
		t.Error("a container name is the common case and should not warn")
	}
	if !isPublicUpstream("http://203.0.113.9:8080") {
		t.Error("a public address should warn")
	}
}

// The form reads a file back. A round trip has to survive, or opening a site
// to change one field would silently drop the rest.
func TestParseSiteSpecRoundTrip(t *testing.T) {
	original := proxySpec()
	original.Locations = []SiteLocation{{Path: "/api", Upstream: "http://127.0.0.1:4000"}}
	out, err := RenderNginx(original)
	if err != nil {
		t.Fatal(err)
	}
	parsed, managed := ParseSiteSpec("app", out)
	if !managed {
		t.Error("a file we wrote should be recognised as ours")
	}
	if parsed.Kind != "proxy" {
		t.Errorf("kind = %q", parsed.Kind)
	}
	if len(parsed.Domains) != 1 || parsed.Domains[0] != "app.example.com" {
		t.Errorf("domains = %v", parsed.Domains)
	}
	if parsed.Upstream != "http://127.0.0.1:3000" {
		t.Errorf("upstream = %q", parsed.Upstream)
	}
	if !parsed.TLS || !parsed.HTTP2 || !parsed.HSTS || !parsed.ForceHTTPS {
		t.Errorf("TLS options lost: %+v", parsed)
	}
	if parsed.CertPath != original.CertPath || parsed.KeyPath != original.KeyPath {
		t.Errorf("certificate paths lost: %q %q", parsed.CertPath, parsed.KeyPath)
	}
	if !parsed.WebSockets || !parsed.Gzip || !parsed.AccessLog || !parsed.SecurityHeaders {
		t.Errorf("switches lost: %+v", parsed)
	}
	if parsed.ClientMaxBody != "50m" || parsed.ProxyTimeout != 60 {
		t.Errorf("limits lost: %q %d", parsed.ClientMaxBody, parsed.ProxyTimeout)
	}
	if len(parsed.Locations) != 1 || parsed.Locations[0].Path != "/api" {
		t.Errorf("locations lost: %+v", parsed.Locations)
	}
	if err := ValidateSpec(parsed); err != nil {
		t.Fatalf("a parsed spec should still be valid: %v", err)
	}
}

// The redirect block this renderer writes carries an ACME challenge location.
// Reading it back as one of the operator's own would emit it twice on the next
// save — once where it works and once where it does nothing.
func TestParseSiteSpecIgnoresTheGeneratedACMELocation(t *testing.T) {
	out, err := RenderNginx(proxySpec())
	if err != nil {
		t.Fatal(err)
	}
	parsed, _ := ParseSiteSpec("app", out)
	for _, loc := range parsed.Locations {
		if strings.Contains(loc.Path, "acme-challenge") {
			t.Fatalf("read back the generated challenge location: %+v", parsed.Locations)
		}
	}
}

func TestParseSiteSpecOnAHandWrittenFile(t *testing.T) {
	content := `
server {
    listen 80;
    server_name legacy.example.com;
    location / {
        proxy_pass http://127.0.0.1:9000;
    }
}
`
	spec, managed := ParseSiteSpec("legacy", content)
	if managed {
		t.Error("a hand-written file must not claim to be ours")
	}
	if spec.Upstream != "http://127.0.0.1:9000" || spec.TLS {
		t.Fatalf("got %+v", spec)
	}
}

func TestParseSiteSpecStaticSite(t *testing.T) {
	spec, _ := ParseSiteSpec("site", "server {\n listen 80;\n server_name a.example.com;\n root /var/www/a;\n}\n")
	if spec.Kind != "static" || spec.Root != "/var/www/a" {
		t.Fatalf("got %+v", spec)
	}
}

func containsSubstring(list []string, want string) bool {
	for _, s := range list {
		if strings.Contains(s, want) {
			return true
		}
	}
	return false
}
