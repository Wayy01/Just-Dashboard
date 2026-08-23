package netsec

import (
	"strings"
	"testing"
)

// firewalld's model is genuinely different from ufw's — zones, services and
// rich rules instead of numbered lines — so the translation into the one Rule
// shape the page renders is where the mistakes would live.

const zoneListing = `public (active)
  target: default
  icmp-block-inversion: no
  interfaces: eth0
  sources: 
  services: dhcpv6-client ssh http
  ports: 8080/tcp 9090/udp
  protocols: 
  forward: yes
  masquerade: no
  forward-ports: 
  source-ports: 
  icmp-blocks: 
  rich rules: 
	rule family="ipv4" source address="10.0.0.0/8" port port="5432" protocol="tcp" accept
	rule family="ipv4" source address="203.0.113.9" drop
`

func TestParseFirewalldZone(t *testing.T) {
	target, rules := parseFirewalldZone(zoneListing)
	if target != "default" {
		t.Fatalf("target = %q", target)
	}
	if len(rules) != 7 {
		t.Fatalf("got %d rules: %+v", len(rules), rules)
	}

	byHandle := map[string]Rule{}
	for _, r := range rules {
		byHandle[r.Handle] = r
	}
	ssh, ok := byHandle["service:ssh"]
	if !ok || ssh.Action != "ALLOW" || ssh.Service != "ssh" {
		t.Errorf("ssh service = %+v", ssh)
	}
	port, ok := byHandle["port:8080/tcp"]
	if !ok || port.Port != "8080" || port.Protocol != "tcp" {
		t.Errorf("port rule = %+v", port)
	}
	// A handle is what removal needs; a rule without one cannot be deleted.
	for _, r := range rules {
		if r.Handle == "" {
			t.Errorf("rule with no handle: %+v", r)
		}
	}
}

func TestParseRichRule(t *testing.T) {
	r, ok := parseRichRule(`rule family="ipv4" source address="10.0.0.0/8" port port="5432" protocol="tcp" accept`)
	if !ok {
		t.Fatal("not recognised")
	}
	if r.Action != "ALLOW" || r.From != "10.0.0.0/8" || r.Port != "5432" || r.Protocol != "tcp" {
		t.Fatalf("got %+v", r)
	}
	if r.To != "5432/tcp" {
		t.Errorf("to = %q", r.To)
	}
}

func TestParseRichRuleVerdicts(t *testing.T) {
	cases := map[string]string{
		`rule family="ipv4" source address="203.0.113.9" drop`:                      "DENY",
		`rule family="ipv4" source address="203.0.113.9" reject`:                    "REJECT",
		`rule family="ipv4" port port="22" protocol="tcp" accept limit value="3/m"`: "LIMIT",
		`rule family="ipv6" service name="http" accept`:                             "ALLOW",
	}
	for raw, want := range cases {
		r, ok := parseRichRule(raw)
		if !ok {
			t.Fatalf("%q not recognised", raw)
		}
		if r.Action != want {
			t.Errorf("%q = %q, want %q", raw, r.Action, want)
		}
	}
	// The v6 family has to be marked, or the same rule written for both
	// families reads as two different rules.
	if r, _ := parseRichRule(`rule family="ipv6" service name="http" accept`); !r.IPv6 {
		t.Error("ipv6 family not marked")
	}
}

// firewalld repeats a key inside its own element — `port port="80"` — so a
// naive attribute scan picks up the wrong one.
func TestParseRichRuleTakesTheOutermostAttribute(t *testing.T) {
	r, _ := parseRichRule(`rule family="ipv4" source address="10.0.0.1" port port="443" protocol="tcp" accept`)
	if r.Port != "443" {
		t.Fatalf("port = %q", r.Port)
	}
}

func TestParseRichRuleRejectsNonsense(t *testing.T) {
	if _, ok := parseRichRule("not a rule at all"); ok {
		t.Error("accepted a line that is not a rich rule")
	}
}

// "default" is firewalld's own word for "reject anything not explicitly
// allowed". Reading it as "no policy set" would report the safest
// configuration as the most permissive one.
func TestFirewalldPolicy(t *testing.T) {
	cases := map[string]string{
		"default":    "reject",
		"ACCEPT":     "allow",
		"DROP":       "deny",
		"%%REJECT%%": "reject",
		"":           "reject",
	}
	for target, want := range cases {
		if got := firewalldPolicy(target).Incoming; got != want {
			t.Errorf("target %q = %q, want %q", target, got, want)
		}
	}
	// firewalld zones do not filter egress; leaving it blank would let the UI
	// imply a restriction that is not there.
	if firewalldPolicy("default").Outgoing != "allow" {
		t.Error("outgoing should be reported as unrestricted")
	}
}

func TestBuildRichRule(t *testing.T) {
	cases := []struct {
		name string
		req  RuleRequest
		want []string
	}{
		{
			"source-restricted allow",
			RuleRequest{Action: "allow", From: "10.0.0.0/8", Port: "5432", Protocol: "tcp"},
			[]string{`family="ipv4"`, `source address="10.0.0.0/8"`, `port port="5432"`, `protocol="tcp"`, "accept"},
		},
		{
			"v6 source picks the right family",
			RuleRequest{Action: "deny", From: "2001:db8::1", Port: "22", Protocol: "tcp"},
			[]string{`family="ipv6"`, "drop"},
		},
		{
			"a service by name",
			RuleRequest{Action: "allow", From: "10.0.0.0/8", App: "postgresql"},
			[]string{`service name="postgresql"`, "accept"},
		},
		{
			"limit becomes a rate-limited accept",
			RuleRequest{Action: "limit", Port: "22", Protocol: "tcp"},
			[]string{"accept", `limit value=`},
		},
		{
			"a port with no protocol defaults to tcp",
			RuleRequest{Action: "reject", From: "10.0.0.1", Port: "3000"},
			[]string{`protocol="tcp"`, "reject"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := buildRichRule(tc.req)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.HasPrefix(got, "rule ") {
				t.Fatalf("not a rich rule: %q", got)
			}
			for _, want := range tc.want {
				if !strings.Contains(got, want) {
					t.Errorf("missing %q from %q", want, got)
				}
			}
		})
	}
}

func TestBuildRichRuleRefusesAnUnknownAction(t *testing.T) {
	if _, err := buildRichRule(RuleRequest{Action: "maybe", Port: "22"}); err == nil {
		t.Fatal("accepted")
	}
}

// Raw iptables is read-only on purpose, and the reason has to reach the UI:
// a greyed-out button with no explanation is worse than an absent one.
func TestIPTablesIsReadOnlyWithAReason(t *testing.T) {
	caps := iptablesBackend{}.Capabilities()
	if caps.Editable || caps.Toggle || caps.DefaultPolicy || caps.Logging || caps.Reset {
		t.Fatalf("iptables claims to be writable: %+v", caps)
	}
	if !strings.Contains(caps.ReadOnlyReason, "reboot") {
		t.Errorf("the reason should say why: %q", caps.ReadOnlyReason)
	}
	b := iptablesBackend{}
	if _, err := b.AddRule(t.Context(), RuleRequest{}); err == nil {
		t.Error("AddRule did not refuse")
	}
	if _, err := b.SetEnabled(t.Context(), true); err == nil {
		t.Error("SetEnabled did not refuse")
	}
}

// Every backend has to declare its capabilities, or the UI has nothing to key
// off and shows controls that cannot work.
func TestEveryBackendDeclaresItself(t *testing.T) {
	seen := map[Backend]bool{}
	for _, b := range backends() {
		if b.Kind() == "" {
			t.Fatalf("%T has no kind", b)
		}
		if seen[b.Kind()] {
			t.Fatalf("two backends called %q", b.Kind())
		}
		seen[b.Kind()] = true
		caps := b.Capabilities()
		if !caps.Editable && caps.ReadOnlyReason == "" {
			t.Errorf("%s is read-only and does not say why", b.Kind())
		}
	}
	if len(seen) != 3 {
		t.Fatalf("expected ufw, firewalld and iptables, got %v", seen)
	}
}
