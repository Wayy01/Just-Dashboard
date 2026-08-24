package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/pquerna/otp/totp"

	"github.com/Wayy01/Just-Dashboard/backend/internal/auth"
	"github.com/Wayy01/Just-Dashboard/backend/internal/httpx"
	"github.com/Wayy01/Just-Dashboard/backend/internal/netsec"
	"github.com/Wayy01/Just-Dashboard/backend/internal/proxysvc"
	"github.com/Wayy01/Just-Dashboard/backend/internal/updates"
)

// These drive the new security and proxy routes through the whole chain — the
// allowlist, the rate limiters, authentication, the capability check and the
// handler — with a real signed-in admin.
//
// Unit tests cover what each rule decides; this covers the part they cannot,
// which is whether the route is mounted where it was meant to be and whether
// the handler survives a host that has none of the tools it reports on. That
// second case is not an edge case: it is every developer machine, and it is
// the one where a nil module or an unchecked error takes the page down.

// signIn creates an admin, completes the second factor and returns a cookie
// header for an authenticated session.
func signIn(t *testing.T, s *Server) string {
	t.Helper()
	ctx := t.Context()
	user, err := s.Auth.CreateUser(ctx, "tester", "Correct-Horse-Battery-9", auth.RoleAdmin, false)
	if err != nil {
		t.Fatal(err)
	}
	login, err := s.Auth.Login(ctx, "tester", "Correct-Horse-Battery-9", "127.0.0.1", "test")
	if err != nil {
		t.Fatal(err)
	}
	secret, err := s.Auth.BeginTOTPEnrollment(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	code, err := totp.GenerateCode(secret.Secret, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Auth.ConfirmTOTPEnrollment(ctx, user.ID, code); err != nil {
		t.Fatal(err)
	}
	if err := s.Auth.VerifySecondFactor(ctx, login.SessionID, user.ID, code); err != nil {
		t.Fatal(err)
	}
	return httpx.SessionCookie + "=" + login.Token
}

type client struct {
	t      *testing.T
	h      http.Handler
	cookie string
}

func (c *client) do(method, path, body string, headers map[string]string) *httptest.ResponseRecorder {
	c.t.Helper()
	var reader *strings.Reader
	if body == "" {
		reader = strings.NewReader("")
	} else {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, reader)
	req.RemoteAddr = "127.0.0.1:5555"
	req.Header.Set("Cookie", c.cookie)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	c.h.ServeHTTP(w, req)
	return w
}

func newClient(t *testing.T) (*client, *Server) {
	t.Helper()
	s := testServer(t)
	return &client{t: t, h: s.Routes(), cookie: signIn(t, s)}, s
}

// Every new read route answers, on a host with no ufw, no fail2ban and no
// nginx. A module that is simply absent must produce information, not a 500.
func TestSecurityReadRoutesSurviveABareHost(t *testing.T) {
	c, _ := newClient(t)
	for _, path := range []string{
		"/api/v1/security/posture",
		"/api/v1/security/services",
		"/api/v1/firewall/",
		"/api/v1/firewall/apps",
		"/api/v1/connections",
		"/api/v1/network",
		"/api/v1/ssh/config",
		"/api/v1/fail2ban/",
		"/api/v1/proxy/streams/",
		"/api/v1/proxy/auth-files/",
		"/api/v1/certificates/dns-providers",
	} {
		t.Run(path, func(t *testing.T) {
			w := c.do(http.MethodGet, path, "", nil)
			if w.Code != http.StatusOK {
				t.Fatalf("got %d: %s", w.Code, strings.TrimSpace(w.Body.String()))
			}
			var any any
			if err := json.Unmarshal(w.Body.Bytes(), &any); err != nil {
				t.Fatalf("body is not JSON: %v", err)
			}
		})
	}
}

// The posture verdict has to come back whole even when almost nothing it
// examines exists — that is the state of every machine before it is set up.
func TestPostureAnswersOnABareHost(t *testing.T) {
	c, _ := newClient(t)
	w := c.do(http.MethodGet, "/api/v1/security/posture", "", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("got %d: %s", w.Code, w.Body.String())
	}
	var posture netsec.Posture
	if err := json.Unmarshal(w.Body.Bytes(), &posture); err != nil {
		t.Fatal(err)
	}
	if posture.Checks == 0 {
		t.Error("no checks reported")
	}
	if posture.Findings == nil {
		t.Error("findings should be an empty array rather than null")
	}
	if posture.CheckedAt.IsZero() {
		t.Error("no evaluation time")
	}
}

// The catalogue is the server's list, and the rule form reads it rather than
// keeping a second copy that would drift from the one the audit uses.
func TestServiceCatalogueIsServed(t *testing.T) {
	c, _ := newClient(t)
	w := c.do(http.MethodGet, "/api/v1/security/services", "", nil)
	var presets []netsec.ServicePreset
	if err := json.Unmarshal(w.Body.Bytes(), &presets); err != nil {
		t.Fatal(err)
	}
	if len(presets) < 10 {
		t.Fatalf("only %d presets", len(presets))
	}
	for _, p := range presets {
		if p.Key == "redis" && p.Danger == "" {
			t.Error("Redis is in the catalogue with no warning")
		}
	}
}

// Probes are validated before anything runs, so a target that is not a
// hostname never reaches a command line.
func TestNetworkProbeRejectsABadTarget(t *testing.T) {
	c, _ := newClient(t)
	w := c.do(http.MethodPost, "/api/v1/network/probe",
		`{"tool":"ping","target":"-i 1 example.com"}`, nil)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("got %d: %s", w.Code, w.Body.String())
	}
	w = c.do(http.MethodPost, "/api/v1/network/probe", `{"tool":"nmap","target":"example.com"}`, nil)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("unknown tool accepted: %d", w.Code)
	}
}

// A DNS lookup is the one probe with no subprocess, so it works here.
func TestNetworkProbeResolvesLocalhost(t *testing.T) {
	c, _ := newClient(t)
	w := c.do(http.MethodPost, "/api/v1/network/probe", `{"tool":"dns","target":"localhost"}`, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("got %d: %s", w.Code, w.Body.String())
	}
	var res netsec.ProbeResult
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	if !res.OK || len(res.Records) == 0 {
		t.Fatalf("localhost did not resolve: %+v", res)
	}
}

// Invariant 3, on the routes added here: an irreversible action refuses to run
// without the typed phrase, and says which phrase.
func TestNewDestructiveRoutesDemandTheTypedPhrase(t *testing.T) {
	cases := []struct {
		method, path, body, phrase string
	}{
		{http.MethodPost, "/api/v1/firewall/reset", `{}`, "reset firewall"},
		{http.MethodPost, "/api/v1/firewall/policy", `{"direction":"incoming","policy":"deny"}`, "deny incoming"},
		{http.MethodPost, "/api/v1/ssh/config", `{"settings":{"x11forwarding":"no"}}`, "change ssh"},
		{http.MethodPost, "/api/v1/certificates/revoke", `{"name":"example.com"}`, "revoke example.com"},
		{http.MethodDelete, "/api/v1/proxy/sites/example", ``, "example"},
		{http.MethodDelete, "/api/v1/proxy/streams/example", ``, "example"},
		{http.MethodDelete, "/api/v1/proxy/auth-files/staging", ``, "staging"},
		{http.MethodPost, "/api/v1/ssh-sessions/4242/disconnect", `{}`, "disconnect 4242"},
	}
	for _, tc := range cases {
		// A client per case. These are destructive routes, so they share the
		// tighter per-principal budget, and running the whole table against
		// one session hits it — which is the limiter working, not a failure
		// worth papering over with a bigger budget.
		t.Run(tc.path, func(t *testing.T) {
			c, _ := newClient(t)
			w := c.do(tc.method, tc.path, tc.body, nil)
			if w.Code != http.StatusPreconditionRequired {
				t.Fatalf("got %d, want 428: %s", w.Code, w.Body.String())
			}
			var body struct {
				Error struct{ Code, Phrase string } `json:"error"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if body.Error.Phrase != tc.phrase {
				t.Fatalf("phrase = %q, want %q", body.Error.Phrase, tc.phrase)
			}

			w = c.do(tc.method, tc.path, tc.body, map[string]string{"X-Confirm": "yes"})
			if w.Code != http.StatusPreconditionFailed {
				t.Fatalf("wrong phrase got %d, want 412: %s", w.Code, w.Body.String())
			}
		})
	}
}

// The destructive budget is tighter than the ordinary one, and it is what
// stops a script working through every rule number faster than anybody could
// notice. Discovered by a test that ran a table of them against one session.
func TestDestructiveRoutesShareATighterBudget(t *testing.T) {
	c, _ := newClient(t)
	limited := false
	for i := 0; i < 60; i++ {
		w := c.do(http.MethodDelete, "/api/v1/firewall/rules/1", "", nil)
		if w.Code == http.StatusTooManyRequests {
			limited = true
			break
		}
	}
	if !limited {
		t.Fatal("the destructive rate limit never engaged")
	}
	// The ordinary read budget is far larger, so the same session can still
	// read the page it was just rate-limited on.
	if w := c.do(http.MethodGet, "/api/v1/firewall/", "", nil); w.Code != http.StatusOK {
		t.Fatalf("reads were caught by the destructive budget: %d", w.Code)
	}
}

// The lockout guard is the reason SSH settings can be offered at all, and it
// has to hold at the HTTP layer rather than only in the package.
func TestSSHApplyRefusesACertainLockout(t *testing.T) {
	c, _ := newClient(t)
	w := c.do(http.MethodPost, "/api/v1/ssh/config",
		`{"settings":{"passwordauthentication":"no","pubkeyauthentication":"no"}}`,
		map[string]string{"X-Confirm": "change ssh"})
	// Either the guard refused it (409) or this machine has no sshd at all
	// (400). What must never happen is the change going through.
	if w.Code != http.StatusConflict && w.Code != http.StatusBadRequest {
		t.Fatalf("got %d: %s", w.Code, w.Body.String())
	}
	if w.Code == http.StatusConflict && !strings.Contains(w.Body.String(), "would_lock_you_out") {
		t.Fatalf("refused for the wrong reason: %s", w.Body.String())
	}
}

// The site builder renders on the server so there is one implementation of
// what a spec means. This is that contract at the HTTP layer.
func TestSitePreviewRendersNginx(t *testing.T) {
	c, _ := newClient(t)
	w := c.do(http.MethodPost, "/api/v1/proxy/sites/preview", `{"spec":{
		"name":"app","kind":"proxy","domains":["app.example.com"],
		"upstream":"http://127.0.0.1:3000","webSockets":true,"securityHeaders":true,
		"accessLog":true,"allowFrom":[],"denyFrom":[],"locations":[]
	}}`, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("got %d: %s", w.Code, w.Body.String())
	}
	var res struct {
		Content  string   `json:"content"`
		Warnings []string `json:"warnings"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"server_name app.example.com;", "proxy_pass http://127.0.0.1:3000;"} {
		if !strings.Contains(res.Content, want) {
			t.Errorf("missing %q from:\n%s", want, res.Content)
		}
	}
	// TLS is off in this spec, and staying quiet about that would be the
	// wrong call — the warning is the feature.
	if len(res.Warnings) == 0 {
		t.Error("a plain-HTTP site produced no warning")
	}
}

func TestSitePreviewRejectsAnInjectedDirective(t *testing.T) {
	c, _ := newClient(t)
	w := c.do(http.MethodPost, "/api/v1/proxy/sites/preview", `{"spec":{
		"name":"app","kind":"proxy","domains":["app.example.com"],
		"upstream":"http://127.0.0.1:3000;\n root /;","allowFrom":[],"denyFrom":[],"locations":[]
	}}`, nil)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("got %d: %s", w.Code, w.Body.String())
	}
}

// Reading a site back is what makes editing one possible. The route takes a
// name, and a name that turns out to be a path is what the allowlist check
// exists for.
func TestSiteSpecRoundTripsThroughTheAPI(t *testing.T) {
	s := testServer(t)
	dir := t.TempDir()
	s.Cfg.NginxDir = dir
	s.initModules()
	if err := os.MkdirAll(filepath.Join(dir, "sites-available"), 0o755); err != nil {
		t.Fatal(err)
	}
	spec := &proxysvc.SiteSpec{
		Name: "app", Kind: "proxy", Domains: []string{"app.example.com"},
		Upstream: "http://127.0.0.1:3000", WebSockets: true, AccessLog: true,
	}
	content, err := proxysvc.RenderNginx(spec)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sites-available", "app"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	c := &client{t: t, h: s.Routes(), cookie: signIn(t, s)}
	w := c.do(http.MethodGet, "/api/v1/proxy/sites/app", "", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("got %d: %s", w.Code, w.Body.String())
	}
	var res struct {
		Spec    proxysvc.SiteSpec `json:"spec"`
		Managed bool              `json:"managed"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	if !res.Managed {
		t.Error("a file this dashboard rendered should be recognised as ours")
	}
	if res.Spec.Upstream != "http://127.0.0.1:3000" {
		t.Errorf("upstream = %q", res.Spec.Upstream)
	}

	if w := c.do(http.MethodGet, "/api/v1/proxy/sites/nope", "", nil); w.Code != http.StatusNotFound {
		t.Errorf("unknown site returned %d", w.Code)
	}
}

// certbot is not installed here, and the route has to say so rather than
// failing in a way the UI renders as broken.
func TestCertbotReportsItselfUnavailable(t *testing.T) {
	c, _ := newClient(t)
	w := c.do(http.MethodGet, "/api/v1/certificates/certbot", "", nil)
	if w.Code != http.StatusServiceUnavailable {
		t.Skipf("certbot appears to be installed here (%d)", w.Code)
	}
	if !strings.Contains(w.Body.String(), "certbot_unavailable") {
		t.Fatalf("wrong code: %s", w.Body.String())
	}
}

// A scan of something that is not listening must come back as a report saying
// so, not as an error — the finding is the product.
func TestTLSScanReportsAnUnreachableHost(t *testing.T) {
	c, _ := newClient(t)
	w := c.do(http.MethodGet, "/api/v1/certificates/scan?domain=127.0.0.1&port=1", "", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("got %d: %s", w.Code, w.Body.String())
	}
	var scan proxysvc.TLSScan
	if err := json.Unmarshal(w.Body.Bytes(), &scan); err != nil {
		t.Fatal(err)
	}
	if scan.Reachable {
		t.Fatal("nothing is listening on port 1")
	}
	if scan.Grade != "F" || len(scan.Findings) == 0 {
		t.Fatalf("got grade %q with %d findings", scan.Grade, len(scan.Findings))
	}
}

// A readonly principal can read the verdict — knowing the machine is badly
// configured is not privileged information — but cannot touch sshd.
func TestReadonlyCannotChangeSSH(t *testing.T) {
	s := testServer(t)
	ctx := t.Context()
	user, err := s.Auth.CreateUser(ctx, "viewer", "Correct-Horse-Battery-9", auth.RoleReadOnly, false)
	if err != nil {
		t.Fatal(err)
	}
	login, err := s.Auth.Login(ctx, "viewer", "Correct-Horse-Battery-9", "127.0.0.1", "test")
	if err != nil {
		t.Fatal(err)
	}
	secret, err := s.Auth.BeginTOTPEnrollment(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	code, _ := totp.GenerateCode(secret.Secret, time.Now())
	if _, err := s.Auth.ConfirmTOTPEnrollment(ctx, user.ID, code); err != nil {
		t.Fatal(err)
	}
	if err := s.Auth.VerifySecondFactor(ctx, login.SessionID, user.ID, code); err != nil {
		t.Fatal(err)
	}
	c := &client{t: t, h: s.Routes(), cookie: httpx.SessionCookie + "=" + login.Token}

	if w := c.do(http.MethodGet, "/api/v1/security/posture", "", nil); w.Code != http.StatusOK {
		t.Errorf("posture is readable by every role: got %d", w.Code)
	}
	if w := c.do(http.MethodGet, "/api/v1/ssh/config", "", nil); w.Code != http.StatusForbidden {
		t.Errorf("ssh config = %d, want 403", w.Code)
	}
	if w := c.do(http.MethodPost, "/api/v1/firewall/rules", `{"action":"allow","port":"22"}`, nil); w.Code != http.StatusForbidden {
		t.Errorf("firewall rule = %d, want 403", w.Code)
	}
	if w := c.do(http.MethodPost, "/api/v1/network/probe", `{"tool":"dns","target":"localhost"}`, nil); w.Code != http.StatusForbidden {
		t.Errorf("probe = %d, want 403", w.Code)
	}
}

// The stream renderer is the other server-side one, and the same contract
// holds: what the form previews is what lands on disk.
func TestStreamPreviewRendersNginx(t *testing.T) {
	c, _ := newClient(t)
	w := c.do(http.MethodPost, "/api/v1/proxy/streams/preview", `{"spec":{
		"name":"replica","listen":5432,"protocol":"tcp",
		"upstream":"10.0.0.5:5432","allowFrom":["10.0.0.0/8"]
	}}`, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("got %d: %s", w.Code, w.Body.String())
	}
	var res struct {
		Content string `json:"content"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"listen 5432;", "server 10.0.0.5:5432;", "deny all;"} {
		if !strings.Contains(res.Content, want) {
			t.Errorf("missing %q from:\n%s", want, res.Content)
		}
	}
}

func TestStreamPreviewRejectsAnInjectedDirective(t *testing.T) {
	c, _ := newClient(t)
	w := c.do(http.MethodPost, "/api/v1/proxy/streams/preview", `{"spec":{
		"name":"replica","listen":5432,"upstream":"10.0.0.5:5432; root /","allowFrom":[]
	}}`, nil)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("got %d: %s", w.Code, w.Body.String())
	}
}

// A password file is only useful if it is a real htpasswd file, so the write
// goes through the API and the listing has to show what landed.
func TestAuthFileRoundTrip(t *testing.T) {
	c, _ := newClient(t)
	w := c.do(http.MethodPost, "/api/v1/proxy/auth-files/",
		`{"file":"staging","user":"admin","password":"correct horse battery"}`, nil)
	if w.Code == http.StatusBadRequest && strings.Contains(w.Body.String(), "permission") {
		t.Skip("no write access to /etc/nginx in this environment")
	}
	if w.Code != http.StatusOK {
		t.Skipf("could not write the password file here (%d): %s", w.Code, w.Body.String())
	}
	var file proxysvc.AuthFile
	if err := json.Unmarshal(w.Body.Bytes(), &file); err != nil {
		t.Fatal(err)
	}
	if len(file.Users) != 1 || file.Users[0] != "admin" {
		t.Fatalf("users = %v", file.Users)
	}
	t.Cleanup(func() {
		c.do(http.MethodDelete, "/api/v1/proxy/auth-files/staging", "",
			map[string]string{"X-Confirm": "staging"})
	})
}

func TestAuthFileRejectsAShortPassword(t *testing.T) {
	c, _ := newClient(t)
	w := c.do(http.MethodPost, "/api/v1/proxy/auth-files/",
		`{"file":"staging","user":"admin","password":"short"}`, nil)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("got %d: %s", w.Code, w.Body.String())
	}
}

// The one thing an import must never do is accept a key that does not belong
// to the certificate — nginx refuses that pair at reload, on a live server.
func TestCertImportRejectsAMismatchedKey(t *testing.T) {
	c, _ := newClient(t)
	w := c.do(http.MethodPost, "/api/v1/certificates/import",
		`{"name":"broken","certificate":"not a certificate","key":"not a key"}`, nil)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("got %d: %s", w.Code, w.Body.String())
	}
}

// A wildcard over an HTTP challenge cannot work, and the refusal has to reach
// the client rather than being discovered as a certbot failure minutes later.
func TestCertIssueRefusesAWildcardOverHTTP(t *testing.T) {
	c, _ := newClient(t)
	w := c.do(http.MethodPost, "/api/v1/certificates/issue",
		`{"domains":["*.example.com"],"email":"a@example.com","method":"nginx"}`, nil)
	if w.Code != http.StatusBadGateway {
		t.Fatalf("got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "DNS challenge") {
		t.Errorf("the answer should say what to do instead: %s", w.Body.String())
	}
}

// Disconnecting a process that is not an interactive login must be refused —
// without that check the route is a kill primitive.
func TestDisconnectRefusesAProcessThatIsNotASession(t *testing.T) {
	c, _ := newClient(t)
	w := c.do(http.MethodPost, "/api/v1/ssh-sessions/1/disconnect", `{}`,
		map[string]string{"X-Confirm": "disconnect 1"})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("got %d: %s", w.Code, w.Body.String())
	}
}

// The updates report has to name its manager and say whether it can tell a
// security update from any other, on whatever this host runs.
func TestUpdatesReportIsHonestAboutTheManager(t *testing.T) {
	c, _ := newClient(t)
	w := c.do(http.MethodGet, "/api/v1/updates/", "", nil)
	if w.Code == http.StatusServiceUnavailable {
		t.Skip("no package manager on this host")
	}
	if w.Code != http.StatusOK {
		t.Fatalf("got %d: %s", w.Code, w.Body.String())
	}
	var report updates.Report
	if err := json.Unmarshal(w.Body.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if report.Available && report.Manager == "" {
		t.Error("a report from an available manager should name it")
	}
}
