package netsec

import (
	"testing"
	"time"
)

// The verdict is the product making a claim, so these pin the claims. Each
// case is one thing an operator would be told, and getting any of them
// backwards is the kind of mistake that is embarrassing rather than subtle.

func findingByID(p *Posture, id string) (SecurityFinding, bool) {
	for _, f := range p.Findings {
		if f.ID == id {
			return f, true
		}
	}
	return SecurityFinding{}, false
}

func TestAssessAQuietHostReportsNothing(t *testing.T) {
	p := Assess(AssessInput{
		Exposure: &Exposure{Grade: "tailscale"},
		Firewall: &FirewallStatus{
			Available: true, Enabled: true, Logging: "on (low)",
			Policy: DefaultPolicy{Incoming: "deny", Outgoing: "allow"},
			Rules:  []Rule{{Action: "ALLOW", To: "22/tcp", Port: "22"}},
		},
		Fail2ban: &Fail2banStatus{Available: true, Running: true, Jails: []Jail{{Name: "sshd"}}},
		SSH: &SSHDConfig{Available: true, Settings: []SSHSetting{
			{Key: "permitrootlogin", Value: "prohibit-password"},
			{Key: "passwordauthentication", Value: "no"},
			{Key: "permitemptypasswords", Value: "no"},
			{Key: "maxauthtries", Value: "3"},
		}},
		Now: time.Now(),
	})
	if p.Status != "ok" {
		t.Fatalf("status = %q with findings %+v", p.Status, p.Findings)
	}
	if len(p.Skipped) != 0 {
		t.Errorf("nothing should be skipped when everything answered: %q", p.Skipped)
	}
}

func TestAssessGradesExposure(t *testing.T) {
	open := Assess(AssessInput{Exposure: &Exposure{Grade: "open"}})
	if f, ok := findingByID(open, "exposure.open"); !ok || f.Level != "critical" {
		t.Fatalf("open exposure = %+v", f)
	}
	if open.Status != "critical" {
		t.Errorf("status = %q", open.Status)
	}
	public := Assess(AssessInput{Exposure: &Exposure{Grade: "public", Allowlist: []string{"203.0.113.0/24"}}})
	if f, ok := findingByID(public, "exposure.public"); !ok || f.Level != "warning" {
		t.Fatalf("public exposure = %+v", f)
	}
}

func TestAssessFirewall(t *testing.T) {
	absent := Assess(AssessInput{Firewall: &FirewallStatus{Available: false}})
	if _, ok := findingByID(absent, "firewall.absent"); !ok {
		t.Error("no firewall should be reported")
	}
	if len(absent.Skipped) == 0 {
		t.Error("a check that could not run must say so rather than passing quietly")
	}

	off := Assess(AssessInput{Firewall: &FirewallStatus{Available: true, Enabled: false, Rules: []Rule{{}}}})
	f, ok := findingByID(off, "firewall.disabled")
	if !ok || f.Level != "critical" {
		t.Fatalf("disabled firewall = %+v", f)
	}
	if f.Fix == "" {
		t.Error("the dashboard can turn it on, so the finding should offer to")
	}

	permissive := Assess(AssessInput{Firewall: &FirewallStatus{
		Available: true, Enabled: true, Policy: DefaultPolicy{Incoming: "allow"},
	}})
	if _, ok := findingByID(permissive, "firewall.default-allow"); !ok {
		t.Error("a default-allow inbound policy should be reported")
	}

	quiet := Assess(AssessInput{Firewall: &FirewallStatus{
		Available: true, Enabled: true, Logging: "off",
		Policy: DefaultPolicy{Incoming: "deny"},
	}})
	if _, ok := findingByID(quiet, "firewall.logging-off"); !ok {
		t.Error("logging off should be reported")
	}
}

func TestAssessDangerousFirewallRule(t *testing.T) {
	p := Assess(AssessInput{Firewall: &FirewallStatus{
		Available: true, Enabled: true, Policy: DefaultPolicy{Incoming: "deny"},
		Rules: []Rule{{
			Number: 3, Action: "ALLOW", To: "6379/tcp", Port: "6379",
			Service: "Redis", Danger: "Never open this to the world.", Raw: "6379/tcp ALLOW IN Anywhere",
		}},
	}})
	f, ok := findingByID(p, "firewall.dangerous-rule.3")
	if !ok || f.Level != "critical" {
		t.Fatalf("got %+v", f)
	}
}

// ufw prints every rule twice on a dual-stack host. Reporting both would make
// one mistake look like two.
func TestAssessDoesNotDoubleCountTheV6Rule(t *testing.T) {
	p := Assess(AssessInput{Firewall: &FirewallStatus{
		Available: true, Enabled: true, Policy: DefaultPolicy{Incoming: "deny"},
		Rules: []Rule{
			{Number: 3, Action: "ALLOW", Port: "6379", Danger: "no"},
			{Number: 4, Action: "ALLOW", Port: "6379", Danger: "no", IPv6: true},
		},
	}})
	count := 0
	for _, f := range p.Findings {
		if f.Area == "firewall" && f.Level == "critical" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("reported %d findings for one rule", count)
	}
}

func TestAssessSSH(t *testing.T) {
	p := Assess(AssessInput{SSH: &SSHDConfig{Available: true, Settings: []SSHSetting{
		{Key: "permitrootlogin", Value: "yes"},
		{Key: "permitemptypasswords", Value: "yes"},
		{Key: "passwordauthentication", Value: "yes"},
		{Key: "maxauthtries", Value: "10"},
	}}})
	for _, id := range []string{"ssh.root-login", "ssh.empty-passwords", "ssh.password-auth", "ssh.max-auth-tries"} {
		if _, ok := findingByID(p, id); !ok {
			t.Errorf("%s not reported", id)
		}
	}
	if p.Status != "critical" {
		t.Errorf("status = %q", p.Status)
	}
}

// Telling somebody to turn off password authentication when they have no key
// is telling them to lock themselves out. The advice has to change, and the
// one-click fix has to disappear.
func TestAssessPasswordAdviceDependsOnWhetherAKeyExists(t *testing.T) {
	noKeys := Assess(AssessInput{SSH: &SSHDConfig{Available: true, Settings: []SSHSetting{
		{Key: "passwordauthentication", Value: "yes"},
	}}})
	f, _ := findingByID(noKeys, "ssh.password-auth")
	if f.Fix != "" {
		t.Error("offered a one-click fix that would lock the operator out")
	}
	if f.Level != "notice" {
		t.Errorf("level = %q; without a key this is not yet actionable", f.Level)
	}

	withKeys := Assess(AssessInput{SSH: &SSHDConfig{
		Available:     true,
		KeyedAccounts: []KeyedAccount{{User: "deploy", Keys: 1}},
		Settings:      []SSHSetting{{Key: "passwordauthentication", Value: "yes"}},
	}})
	f, _ = findingByID(withKeys, "ssh.password-auth")
	if f.Fix == "" {
		t.Error("with a key present the fix is safe and should be offered")
	}
	if f.Level != "warning" {
		t.Errorf("level = %q", f.Level)
	}
}

func TestAssessExposedPorts(t *testing.T) {
	p := Assess(AssessInput{
		Listeners: []ExposedPort{
			{Port: 6379, Protocol: "tcp", Address: "0.0.0.0", Process: "redis-server", Exposed: true},
			{Port: 5432, Protocol: "tcp", Address: "127.0.0.1", Process: "postgres", Exposed: false},
			{Port: 443, Protocol: "tcp", Address: "0.0.0.0", Process: "nginx", Exposed: true},
		},
	})
	if _, ok := findingByID(p, "ports.exposed.tcp.6379"); !ok {
		t.Error("an exposed Redis should be reported")
	}
	if _, ok := findingByID(p, "ports.exposed.tcp.5432"); ok {
		t.Error("a loopback-bound database is the correct arrangement and must stay silent")
	}
	if _, ok := findingByID(p, "ports.exposed.tcp.443"); ok {
		t.Error("an exposed web server is the point of the machine")
	}
}

// A firewall in front of the port changes what the finding means, and saying
// so is the difference between a finding and a false alarm.
func TestAssessSoftensAnExposedPortBehindADenyPolicy(t *testing.T) {
	p := Assess(AssessInput{
		Firewall:  &FirewallStatus{Available: true, Enabled: true, Policy: DefaultPolicy{Incoming: "deny"}},
		Listeners: []ExposedPort{{Port: 6379, Protocol: "tcp", Address: "0.0.0.0", Exposed: true}},
	})
	f, ok := findingByID(p, "ports.exposed.tcp.6379")
	if !ok {
		t.Fatal("still worth reporting")
	}
	if f.Level != "warning" {
		t.Errorf("level = %q, want warning behind a deny policy", f.Level)
	}
}

func TestAssessIntrusion(t *testing.T) {
	stopped := Assess(AssessInput{Fail2ban: &Fail2banStatus{Available: true, Running: false}})
	if f, ok := findingByID(stopped, "intrusion.stopped"); !ok || f.Level != "warning" {
		t.Errorf("installed-and-stopped = %+v", f)
	}
	empty := Assess(AssessInput{Fail2ban: &Fail2banStatus{Available: true, Running: true}})
	if _, ok := findingByID(empty, "intrusion.no-jails"); !ok {
		t.Error("running with no jails bans nobody and should say so")
	}
	missing := Assess(AssessInput{SSH: &SSHDConfig{Available: true}})
	if _, ok := findingByID(missing, "intrusion.absent"); !ok {
		t.Error("no fail2ban on a host running sshd should be a notice")
	}
}

func TestAssessFailedLoginVolume(t *testing.T) {
	quiet := Assess(AssessInput{FailedLogins: 5})
	if _, ok := findingByID(quiet, "intrusion.failed-logins"); ok {
		t.Error("a handful of failures is background noise")
	}
	busy := Assess(AssessInput{FailedLogins: failedLoginNoticeCount})
	if f, _ := findingByID(busy, "intrusion.failed-logins"); f.Level != "notice" {
		t.Errorf("level = %q", f.Level)
	}
	loud := Assess(AssessInput{FailedLogins: failedLoginWarningCount})
	if f, _ := findingByID(loud, "intrusion.failed-logins"); f.Level != "warning" {
		t.Errorf("level = %q", f.Level)
	}
}

func TestAssessCertificates(t *testing.T) {
	p := Assess(AssessInput{Certificates: []CertSummary{
		{Name: "expired.example", DaysLeft: -2, Expired: true},
		{Name: "soon.example", DaysLeft: 2},
		{Name: "later.example", DaysLeft: 10},
		{Name: "fine.example", DaysLeft: 60},
	}})
	if f, ok := findingByID(p, "tls.expired.expired.example"); !ok || f.Level != "critical" {
		t.Errorf("expired = %+v", f)
	}
	if f, ok := findingByID(p, "tls.expiring.soon.example"); !ok || f.Level != "critical" {
		t.Errorf("two days left = %+v", f)
	}
	if f, ok := findingByID(p, "tls.expiring.later.example"); !ok || f.Level != "warning" {
		t.Errorf("ten days left = %+v", f)
	}
	if _, ok := findingByID(p, "tls.expiring.fine.example"); ok {
		t.Error("sixty days left is not a finding")
	}
}

func TestAssessUpdates(t *testing.T) {
	few := Assess(AssessInput{SecurityUpdates: 2})
	if f, _ := findingByID(few, "updates.security"); f.Level != "notice" {
		t.Errorf("level = %q", f.Level)
	}
	many := Assess(AssessInput{SecurityUpdates: securityUpdateWarningCount})
	if f, _ := findingByID(many, "updates.security"); f.Level != "warning" {
		t.Errorf("level = %q", f.Level)
	}
	reboot := Assess(AssessInput{RebootRequired: true})
	if _, ok := findingByID(reboot, "updates.reboot"); !ok {
		t.Error("a pending reboot should be reported")
	}
}

// Worst first, and stable between polls: a list that reshuffles itself is one
// nobody can read while it refreshes.
func TestAssessOrdersFindingsWorstFirst(t *testing.T) {
	p := Assess(AssessInput{
		Exposure:        &Exposure{Grade: "public"},
		SecurityUpdates: 1,
		SSH:             &SSHDConfig{Available: true, Settings: []SSHSetting{{Key: "permitrootlogin", Value: "yes"}}},
	})
	if len(p.Findings) < 3 {
		t.Fatalf("expected several findings, got %d", len(p.Findings))
	}
	last := 4
	for _, f := range p.Findings {
		rank := levelRank(f.Level)
		if rank > last {
			t.Fatalf("finding %s (%s) came after a milder one", f.ID, f.Level)
		}
		last = rank
	}
}
