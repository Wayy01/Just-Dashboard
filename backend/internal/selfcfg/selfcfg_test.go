package selfcfg

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeEnv(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// The comments in this file are the operator's, written by install.sh and
// edited over years. A settings page that ate them the first time somebody
// changed a port would be a bad trade for the convenience.
func TestSaveKeepsCommentsAndOrder(t *testing.T) {
	path := writeEnv(t, `# Written by install.sh.
JD_MASTER_KEY=abc

# The port you connect to.
JD_PORT=8443
JD_FRONTEND_PORT=3000
`)
	env, err := LoadEnv(path)
	if err != nil {
		t.Fatal(err)
	}
	env.Set("JD_PORT", "9443")
	env.Set("JD_REQUIRE_2FA", "true")
	if _, err := env.Save(); err != nil {
		t.Fatal(err)
	}

	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	got := string(b)
	for _, want := range []string{
		"# Written by install.sh.",
		"# The port you connect to.\nJD_PORT=9443",
		"JD_MASTER_KEY=abc",
		"JD_REQUIRE_2FA=true",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("saved file lost %q:\n%s", want, got)
		}
	}
	if strings.Count(got, "JD_PORT=") != 1 {
		t.Fatalf("JD_PORT was appended rather than replaced:\n%s", got)
	}
}

// The backup is what the rollback restores, so a save that did not take one
// would quietly remove the safety net this whole package is built around.
func TestSaveTakesABackup(t *testing.T) {
	path := writeEnv(t, "JD_PORT=8443\n")
	env, err := LoadEnv(path)
	if err != nil {
		t.Fatal(err)
	}
	env.Set("JD_PORT", "9443")
	backup, err := env.Save()
	if err != nil {
		t.Fatal(err)
	}
	if backup == "" {
		t.Fatal("no backup path returned")
	}
	b, err := os.ReadFile(backup)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "JD_PORT=8443") {
		t.Fatalf("backup does not hold the previous value: %s", b)
	}
}

func TestReadSettingsFillsGapsWithTheShippedDefaults(t *testing.T) {
	path := writeEnv(t, "JD_SITE=box.tail1234.ts.net\nJD_TLS=tailscale\nJD_PORT=8443\n")
	env, err := LoadEnv(path)
	if err != nil {
		t.Fatal(err)
	}
	s := ReadSettings(env)
	if s.Site != "box.tail1234.ts.net" || s.TLS != TLSTailscale {
		t.Fatalf("site/tls not read: %+v", s)
	}
	if s.BackendPort != 8080 || s.FrontendPort != 3000 {
		t.Fatalf("absent ports should read as the compose defaults: %+v", s)
	}
	if s.Endpoint() != "https://box.tail1234.ts.net:8443" {
		t.Fatalf("endpoint = %q", s.Endpoint())
	}
}

func TestEndpointIsPlainHTTPOnlyWhenTLSIsOff(t *testing.T) {
	s := Settings{Site: "localhost", TLS: TLSOff, Port: 8443}
	if s.Endpoint() != "http://localhost:8443" {
		t.Fatalf("endpoint = %q", s.Endpoint())
	}
}

// The rule this package exists to enforce: the allowlist is checked before
// authentication, so an operator who drops their own network out of it does
// not get an error, they get a dashboard that has stopped existing for them.
func TestValidateRefusesAnAllowlistThatLocksTheCallerOut(t *testing.T) {
	old := defaults()
	next := old
	next.AllowedCIDRs = "127.0.0.1/32,10.0.0.0/8"
	err := next.Validate(old, "100.101.102.103", nil)
	if err == nil || !strings.Contains(err.Error(), "100.101.102.103") {
		t.Fatalf("validate = %v, want a refusal naming the caller's address", err)
	}
}

func TestValidateInsistsOnLoopback(t *testing.T) {
	old := defaults()
	next := old
	next.AllowedCIDRs = "100.64.0.0/10"
	if err := next.Validate(old, "100.64.0.1", nil); err == nil {
		t.Fatal("an allowlist without loopback was accepted; the SSH tunnel is the way back in")
	}
}

func TestValidateRefusesPlainHTTPOffLoopback(t *testing.T) {
	old := defaults()
	next := old
	next.Site = "100.101.102.103"
	next.TLS = TLSOff
	if err := next.Validate(old, "127.0.0.1", nil); err == nil {
		t.Fatal("plain HTTP was accepted on a routable address")
	}
}

func TestValidateRefusesATailscaleCertificateForAnIP(t *testing.T) {
	old := defaults()
	next := old
	next.Site = "100.101.102.103"
	next.TLS = TLSTailscale
	if err := next.Validate(old, "127.0.0.1", nil); err == nil {
		t.Fatal("a Tailscale certificate was accepted for an address it can never be issued for")
	}
}

func TestValidateRefusesCollidingPorts(t *testing.T) {
	old := defaults()
	next := old
	next.FrontendPort = next.BackendPort
	if err := next.Validate(old, "127.0.0.1", nil); err == nil {
		t.Fatal("two services were allowed to share one port")
	}
}

// Only a port that is moving is probed, because the three it is on now are
// held by this very stack. Testing those would report the dashboard as
// colliding with itself.
func TestValidateOnlyProbesPortsThatMove(t *testing.T) {
	old := defaults()
	next := old
	next.FrontendPort = 20001

	probed := []int{}
	free := func(port int) error {
		probed = append(probed, port)
		return nil
	}
	if err := next.Validate(old, "127.0.0.1", free); err != nil {
		t.Fatal(err)
	}
	if len(probed) != 1 || probed[0] != 20001 {
		t.Fatalf("probed %v, want only the port that changed", probed)
	}
}

func TestDiffNamesOnlyWhatMoved(t *testing.T) {
	old := defaults()
	next := old
	next.Port = 9443
	next.Require2FA = true

	changes := Diff(old, next)
	if len(changes) != 2 {
		t.Fatalf("diff = %+v, want two entries", changes)
	}
	if !MovesEndpoint(old, next) {
		t.Fatal("a port change was not recognised as moving the endpoint")
	}
	same := old
	same.Require2FA = true
	if MovesEndpoint(old, same) {
		t.Fatal("a policy change was reported as moving the endpoint")
	}
}

// Every way this feature can fail in a way an operator cannot recover from is
// a wrong flag here, and this is the only check on it that needs no Docker.
func TestSiblingArgv(t *testing.T) {
	a := NewApplier(NewStore("/var/lib/just-dashboard"), "/var/lib/just-dashboard",
		"unix:///var/run/docker.sock", slog.New(slog.NewTextHandler(io.Discard, nil)))
	args := a.siblingArgs(&Run{
		Dir:   "/opt/Just-Dashboard",
		Image: "just-dashboard-backend:latest",
	})
	line := strings.Join(args, " ")
	for _, want := range []string{
		"run --detach",
		"--name " + RestartContainer,
		"--network host",
		"--restart no",
		"-v /var/run/docker.sock:/var/run/docker.sock",
		"-v /opt/Just-Dashboard:/opt/Just-Dashboard",
		"-v /var/lib/just-dashboard:/var/lib/just-dashboard",
		"-w /opt/Just-Dashboard",
		"--entrypoint /usr/local/bin/just-dashboard",
		"just-dashboard-backend:latest -self-restart -state-dir /var/lib/just-dashboard",
	} {
		if !strings.Contains(line, want) {
			t.Fatalf("argv is missing %q:\n%s", want, line)
		}
	}
	if strings.Contains(line, "JD_MASTER_KEY") {
		t.Fatal("the sibling was handed a secret it has no use for")
	}
}
