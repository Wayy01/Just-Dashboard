package netsec

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

// The ufw status parse is the whole firewall tab, and it is fed by text that
// changes shape between the numbered and verbose forms. These pin the parts
// that were read wrong before: the policy line, the v6 duplicate, and which
// rules deserve a warning.

func TestParseDefaultPolicy(t *testing.T) {
	got := parseDefaultPolicy("deny (incoming), allow (outgoing), disabled (routed)")
	if got.Incoming != "deny" || got.Outgoing != "allow" || got.Routed != "disabled" {
		t.Fatalf("got %+v", got)
	}
}

func TestParseDefaultPolicyIgnoresJunk(t *testing.T) {
	got := parseDefaultPolicy("deny (incoming)")
	if got.Incoming != "deny" {
		t.Fatalf("incoming = %q", got.Incoming)
	}
	if got.Outgoing != "" || got.Routed != "" {
		t.Fatalf("invented values: %+v", got)
	}
}

func TestApplyUFWVerbose(t *testing.T) {
	st := &FirewallStatus{}
	applyUFWVerbose(st, strings.Join([]string{
		"Status: active",
		"Logging: on (low)",
		"Default: deny (incoming), allow (outgoing), disabled (routed)",
		"New profiles: skip",
	}, "\n"))
	if !st.Enabled {
		t.Error("Status: active did not enable")
	}
	if st.Logging != "on (low)" {
		t.Errorf("logging = %q", st.Logging)
	}
	if st.Policy.Incoming != "deny" {
		t.Errorf("policy = %+v", st.Policy)
	}
}

// Annotation is applied centrally in Status rather than by each backend, so
// firewalld's rules get the same names and warnings ufw's do. These drive it
// through the ufw parser because that is where the awkward text is.
func TestAnnotateNamesTheServiceAndFlagsTheDangerousOnes(t *testing.T) {
	rule := parseUFWRule(3, "6379/tcp                   ALLOW IN    Anywhere")
	if rule.Port != "6379" || rule.Protocol != "tcp" {
		t.Fatalf("port/proto = %q/%q", rule.Port, rule.Protocol)
	}
	annotateRule(&rule)
	if rule.Service != "Redis" {
		t.Errorf("service = %q, want Redis", rule.Service)
	}
	if rule.Danger == "" {
		t.Error("Redis open to Anywhere should carry a warning")
	}
}

// The same port restricted to a private source is the arrangement being
// recommended. Warning about it would train the operator to ignore warnings.
func TestAnnotateDoesNotWarnAboutARestrictedSource(t *testing.T) {
	rule := parseUFWRule(4, "6379/tcp                   ALLOW IN    10.0.0.0/8")
	annotateRule(&rule)
	if rule.Service != "Redis" {
		t.Fatalf("service = %q", rule.Service)
	}
	if rule.Danger != "" {
		t.Errorf("restricted rule warned anyway: %q", rule.Danger)
	}
}

// A firewalld rule reaches annotation with no protocol on a service entry and
// a full port on a rich rule; both have to come out named.
func TestAnnotateWorksForFirewalldShapes(t *testing.T) {
	port := Rule{Action: "ALLOW", From: "Anywhere", To: "5432/tcp", Port: "5432", Protocol: "tcp"}
	annotateRule(&port)
	if port.Service != "PostgreSQL" || port.Danger == "" {
		t.Fatalf("port rule = %+v", port)
	}
	rich := Rule{Action: "ALLOW", From: "10.0.0.0/8", To: "5432/tcp", Port: "5432", Protocol: "tcp"}
	annotateRule(&rich)
	if rich.Service != "PostgreSQL" {
		t.Errorf("rich rule unnamed: %+v", rich)
	}
	if rich.Danger != "" {
		t.Error("a source-restricted database rule is the recommendation, not a warning")
	}
}

func TestParseUFWRuleMarksTheV6Duplicate(t *testing.T) {
	rule := parseUFWRule(2, "22/tcp (v6)                ALLOW IN    Anywhere (v6)")
	if !rule.IPv6 {
		t.Fatal("v6 rule not marked, so it reads as a second rule for the same port")
	}
	if rule.Port != "22" {
		t.Errorf("port = %q, want 22 with the suffix stripped", rule.Port)
	}
}

func TestParseUFWRuleKeepsTheComment(t *testing.T) {
	rule := parseUFWRule(1, "443/tcp                    ALLOW IN    Anywhere    # web")
	if rule.Comment != "web" {
		t.Errorf("comment = %q", rule.Comment)
	}
	if rule.Action != "ALLOW" {
		t.Errorf("action = %q", rule.Action)
	}
}

func TestAdmitsAnything(t *testing.T) {
	if admitsAnything(nil) {
		t.Error("an empty rule list admits nobody")
	}
	if admitsAnything([]Rule{{Action: "DENY", Direction: ""}}) {
		t.Error("a deny rule is not an admission")
	}
	if !admitsAnything([]Rule{{Action: "ALLOW", Direction: ""}}) {
		t.Error("ufw leaves Direction empty on inbound rules")
	}
	if !admitsAnything([]Rule{{Action: "LIMIT", Direction: "IN"}}) {
		t.Error("LIMIT still lets connections in")
	}
	if admitsAnything([]Rule{{Action: "ALLOW", Direction: "OUT"}}) {
		t.Error("an outbound allow does not let anybody in")
	}
}

// Invariant: a rule that would sever the caller's own connection is refused
// before it is applied.
func TestGuardLockout(t *testing.T) {
	cases := []struct {
		name                            string
		action, direction, from, caller string
		refuse                          bool
	}{
		{"blanket inbound deny", "deny", "in", "", "203.0.113.9", true},
		{"deny the caller exactly", "deny", "in", "203.0.113.9", "203.0.113.9", true},
		{"reject a range covering the caller", "reject", "in", "203.0.113.0/24", "203.0.113.9", true},
		{"deny someone else", "deny", "in", "198.51.100.4", "203.0.113.9", false},
		{"allow is never a lockout", "allow", "in", "", "203.0.113.9", false},
		{"outbound is not this guard's business", "deny", "out", "", "203.0.113.9", false},
		{"unknown caller cannot be judged", "deny", "in", "", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := guardLockout(tc.action, tc.direction, tc.from, tc.caller)
			if tc.refuse && err == nil {
				t.Fatal("expected the rule to be refused")
			}
			if !tc.refuse && err != nil {
				t.Fatalf("refused a safe rule: %v", err)
			}
		})
	}
}

func TestValidAddress(t *testing.T) {
	for _, ok := range []string{"", "any", "10.0.0.1", "10.0.0.0/8", "2001:db8::1", "fd00::/8"} {
		if err := validAddress(ok, "from"); err != nil {
			t.Errorf("%q rejected: %v", ok, err)
		}
	}
	for _, bad := range []string{"example.com", "10.0.0.1; rm -rf /", "10.0.0.0/99"} {
		if err := validAddress(bad, "from"); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

// Validation is a pure function now that there is more than one backend: the
// checks run once, before dispatch, so a new firewall cannot be added without
// them. These exercise it directly rather than through a host that may have no
// firewall installed at all.
func TestNormaliseRuleRejectsBadInput(t *testing.T) {
	cases := []struct {
		name string
		req  RuleRequest
	}{
		{"unknown action", RuleRequest{Action: "drop", Port: "22"}},
		{"bad direction", RuleRequest{Action: "allow", Direction: "sideways", Port: "22"}},
		{"non-numeric port", RuleRequest{Action: "allow", Port: "http"}},
		{"port list without a protocol", RuleRequest{Action: "allow", Port: "80,443"}},
		{"unknown protocol", RuleRequest{Action: "allow", Port: "22", Protocol: "sctp"}},
		{"comment with a semicolon", RuleRequest{Action: "allow", Port: "22", Comment: "web; drop"}},
		{"source that is not an address", RuleRequest{Action: "allow", Port: "22", From: "$(whoami)"}},
		{"profile and port together", RuleRequest{Action: "allow", Port: "22", App: "OpenSSH"}},
		{"position out of range", RuleRequest{Action: "allow", Port: "22", Position: -1}},
		{"nothing to match on", RuleRequest{Action: "allow"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := normaliseRule(tc.req); err == nil {
				t.Fatal("accepted")
			}
		})
	}
}

func TestNormaliseRuleAcceptsAndCanonicalises(t *testing.T) {
	got, err := normaliseRule(RuleRequest{Action: "ALLOW", Port: "443", Protocol: "TCP"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Action != "allow" || got.Protocol != "tcp" {
		t.Fatalf("not lowercased: %+v", got)
	}
	// An omitted direction is inbound, which is what every rule form means.
	if got.Direction != "in" {
		t.Fatalf("direction = %q", got.Direction)
	}
}

// A read-only backend has to refuse at the Service layer too, or the capability
// flags would be advice the API ignores.
func TestServiceRefusesWritesOnAReadOnlyBackend(t *testing.T) {
	caps := iptablesBackend{}.Capabilities()
	if caps.Editable {
		t.Skip("iptables became writable; this test guards the read-only path")
	}
	if !errors.Is(fmt.Errorf("%w: %s", ErrReadOnly, caps.ReadOnlyReason), ErrReadOnly) {
		t.Fatal("the refusal must be identifiable as ErrReadOnly so the API can map it")
	}
}

func TestSetDefaultPolicyRejectsBadInput(t *testing.T) {
	s := New()
	if _, err := s.SetDefaultPolicy(t.Context(), "sideways", "deny"); err == nil {
		t.Error("accepted an unknown direction")
	}
	if _, err := s.SetDefaultPolicy(t.Context(), "incoming", "drop"); err == nil {
		t.Error("accepted an unknown policy")
	}
}

func TestSetLoggingRejectsUnknownLevels(t *testing.T) {
	s := New()
	if _, err := s.SetLogging(t.Context(), "verbose"); err == nil {
		t.Error("accepted an unknown level")
	}
}
