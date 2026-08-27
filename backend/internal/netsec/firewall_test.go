package netsec

import (
	"context"
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

// An edit is a delete and an add, and the order it happens in is the whole
// safety property: the replacement goes in first so a failure leaves the
// original rule standing. These drive the sequence against a backend that
// records what it was asked to do, because the alternative is finding out on a
// live firewall.

type recordedBackend struct {
	kind    Backend
	caps    FirewallCapabilities
	rules   []Rule
	calls   []string
	addErr  error
	delErr  error
	lastAdd RuleRequest
}

func (b *recordedBackend) Kind() Backend { return b.kind }
func (b *recordedBackend) Detect() bool  { return true }
func (b *recordedBackend) Status(context.Context) (*FirewallStatus, error) {
	b.calls = append(b.calls, "status")
	return &FirewallStatus{Enabled: true, Backend: b.kind, Rules: b.rules}, nil
}
func (b *recordedBackend) AddRule(_ context.Context, req RuleRequest) (string, error) {
	b.calls = append(b.calls, fmt.Sprintf("add %s/%s at %d", req.Port, req.Protocol, req.Position))
	b.lastAdd = req
	if b.addErr != nil {
		return "", b.addErr
	}
	// A numbered backend renumbers everything below an insert, which is the
	// whole reason the original cannot be found again by arithmetic. Modelled
	// here so the sequence under test is the one a real ufw would produce.
	if b.kind == BackendUFW && req.Position > 0 && req.Position <= len(b.rules)+1 {
		added := Rule{Action: strings.ToUpper(req.Action), Direction: "IN",
			To: req.Port + "/" + req.Protocol, From: "Anywhere",
			Port: req.Port, Protocol: req.Protocol, Comment: req.Comment}
		b.rules = append(b.rules[:req.Position-1],
			append([]Rule{added}, b.rules[req.Position-1:]...)...)
		b.renumber()
	}
	return "rule added", nil
}
func (b *recordedBackend) DeleteRule(_ context.Context, number int) (string, error) {
	b.calls = append(b.calls, fmt.Sprintf("delete %d", number))
	if b.delErr != nil {
		return "", b.delErr
	}
	for i, r := range b.rules {
		if r.Number == number {
			b.rules = append(b.rules[:i], b.rules[i+1:]...)
			b.renumber()
			break
		}
	}
	return "rule deleted", nil
}
func (b *recordedBackend) renumber() {
	for i := range b.rules {
		b.rules[i].Number = i + 1
	}
}
func (b *recordedBackend) findRule(_ context.Context, want Rule, ipv6 bool) (int, bool) {
	for _, r := range b.rules {
		if r.IPv6 == ipv6 && sameRule(r, want) {
			return r.Number, true
		}
	}
	return 0, false
}
func (b *recordedBackend) removeHandle(_ context.Context, handle string) (string, error) {
	b.calls = append(b.calls, "remove "+handle)
	if b.delErr != nil {
		return "", b.delErr
	}
	return "rule removed", nil
}
func (b *recordedBackend) SetEnabled(context.Context, bool) (string, error) { return "", nil }
func (b *recordedBackend) SetDefaultPolicy(context.Context, string, string) (string, error) {
	return "", nil
}
func (b *recordedBackend) SetLogging(context.Context, string) (string, error) { return "", nil }
func (b *recordedBackend) Reset(context.Context) (string, error)              { return "", nil }
func (b *recordedBackend) Profiles(context.Context) ([]AppProfile, error)     { return nil, nil }
func (b *recordedBackend) Capabilities() FirewallCapabilities                 { return b.caps }

func editable(kind Backend) *recordedBackend {
	return &recordedBackend{kind: kind, caps: FirewallCapabilities{Editable: true}}
}

// numbered seeds a ufw-shaped listing: ports in order, numbered from one.
func numbered(ports ...string) []Rule {
	rules := make([]Rule, 0, len(ports))
	for i, p := range ports {
		rules = append(rules, Rule{
			Number: i + 1, Action: "ALLOW", Direction: "IN", From: "Anywhere",
			To: p + "/tcp", Port: p, Protocol: "tcp",
		})
	}
	return rules
}

// ufw stops at the first match, so the replacement has to land where the
// original was rather than at the bottom — and the original, pushed down by the
// insert, is found again by what it says rather than by number+1. The
// arithmetic is right only while the insert is guaranteed to have happened,
// which is exactly what ufw does not guarantee.
func TestReplaceRuleOnUFWInsertsAtThePositionThenRemovesTheOriginal(t *testing.T) {
	b := editable(BackendUFW)
	b.rules = numbered("22", "80", "443", "3000")
	if _, err := replaceRule(t.Context(), b, 3, RuleRequest{Action: "allow", Port: "8443", Protocol: "tcp"}, "203.0.113.9"); err != nil {
		t.Fatal(err)
	}
	want := []string{"status", "add 8443/tcp at 3", "delete 4"}
	if strings.Join(b.calls, "; ") != strings.Join(want, "; ") {
		t.Fatalf("calls = %v, want %v", b.calls, want)
	}
	var ports []string
	for _, r := range b.rules {
		ports = append(ports, r.Port)
	}
	if strings.Join(ports, ",") != "22,80,8443,3000" {
		t.Fatalf("rules = %v, want the replacement in the original's place", ports)
	}
}

// An add ufw declined to perform must not be followed by a delete. ufw answers
// a duplicate with "Skipping adding existing rule" and exit 0, so the number+1
// this used to delete was the operator's *next* rule — a rule they never
// touched, removed by an edit that changed nothing.
func TestReplaceRuleDeletesNothingWhenTheAddWasSkipped(t *testing.T) {
	b := editable(BackendUFW)
	b.rules = numbered("22", "80", "443")
	b.addErr = errRuleExists
	_, err := replaceRule(t.Context(), b, 2, RuleRequest{Action: "allow", Port: "443", Protocol: "tcp"}, "")
	if err == nil {
		t.Fatal("an edit that added nothing reported as success")
	}
	for _, c := range b.calls {
		if strings.HasPrefix(c, "delete") {
			t.Fatalf("deleted a rule after an add that did nothing: %v", b.calls)
		}
	}
	if len(b.rules) != 3 {
		t.Fatalf("rules = %d, want the list untouched", len(b.rules))
	}
}

// firewalld has no order and no numbers of its own — the position in the
// listing is this dashboard's — so the old rule has to be identified before
// anything is added, or the number points at something else by then.
func TestReplaceRuleOnFirewalldResolvesTheHandleBeforeAdding(t *testing.T) {
	b := editable(BackendFirewalld)
	b.rules = []Rule{{Handle: "svc:ssh"}, {Handle: "port:8080/tcp"}}
	if _, err := replaceRule(t.Context(), b, 2, RuleRequest{Action: "allow", Port: "9090", Protocol: "tcp"}, ""); err != nil {
		t.Fatal(err)
	}
	want := []string{"status", "add 9090/tcp at 0", "remove port:8080/tcp"}
	if strings.Join(b.calls, "; ") != strings.Join(want, "; ") {
		t.Fatalf("calls = %v, want %v", b.calls, want)
	}
	// Position is ufw's concept; sending one to firewalld would be noise.
	if b.lastAdd.Position != 0 {
		t.Errorf("position %d sent to a backend with no ordering", b.lastAdd.Position)
	}
}

// If the add fails there must be no delete at all: the original rule is still
// the only thing standing between the host and the network.
func TestReplaceRuleLeavesTheOriginalWhenTheAddFails(t *testing.T) {
	b := editable(BackendUFW)
	b.rules = numbered("22", "80")
	b.addErr = errors.New("ufw said no")
	if _, err := replaceRule(t.Context(), b, 2, RuleRequest{Action: "allow", Port: "22", Protocol: "tcp"}, ""); err == nil {
		t.Fatal("expected the failure to be reported")
	}
	for _, c := range b.calls {
		if strings.HasPrefix(c, "delete") || strings.HasPrefix(c, "remove") {
			t.Fatalf("deleted the original after a failed add: %v", b.calls)
		}
	}
}

// The opposite failure leaves two rules, which is visible in the list and
// harmless — the firewall is at least as strict as it was. It still has to be
// reported rather than passed off as a clean edit.
func TestReplaceRuleReportsAnOrphanedOriginal(t *testing.T) {
	b := editable(BackendUFW)
	b.rules = numbered("22", "80")
	b.delErr = errors.New("no such rule")
	out, err := replaceRule(t.Context(), b, 2, RuleRequest{Action: "allow", Port: "22", Protocol: "tcp"}, "")
	if err == nil {
		t.Fatal("a half-finished edit reported as success")
	}
	if !strings.Contains(err.Error(), "original") {
		t.Errorf("error does not say what state the firewall is in: %v", err)
	}
	// The add did happen, and the caller is told so rather than being left to
	// guess whether to retry.
	if out == "" {
		t.Error("the output of the successful half was dropped")
	}
}

// The same guard the add path has: an edit that turns a rule into one severing
// the caller's own connection is still a lockout.
func TestReplaceRuleRefusesALockout(t *testing.T) {
	b := editable(BackendUFW)
	_, err := replaceRule(t.Context(), b, 1, RuleRequest{Action: "deny", Direction: "in", From: "203.0.113.0/24"}, "203.0.113.9")
	if err == nil {
		t.Fatal("accepted a rule covering the caller's own address")
	}
	if len(b.calls) != 0 {
		t.Fatalf("touched the firewall before the guard ran: %v", b.calls)
	}
}

func TestReplaceRuleRefusesAReadOnlyBackend(t *testing.T) {
	b := &recordedBackend{kind: BackendIPTables, caps: iptablesBackend{}.Capabilities()}
	_, err := replaceRule(t.Context(), b, 1, RuleRequest{Action: "allow", Port: "22"}, "")
	if !errors.Is(err, ErrReadOnly) {
		t.Fatalf("err = %v, want ErrReadOnly so the API can answer 501", err)
	}
	if len(b.calls) != 0 {
		t.Fatalf("a read-only backend was written to: %v", b.calls)
	}
}

func TestReplaceRuleRejectsBadInput(t *testing.T) {
	b := editable(BackendUFW)
	if _, err := replaceRule(t.Context(), b, 0, RuleRequest{Action: "allow", Port: "22"}, ""); err == nil {
		t.Error("accepted rule number 0, which no listing ever contains")
	}
	if _, err := replaceRule(t.Context(), b, 1, RuleRequest{Action: "drop", Port: "22"}, ""); err == nil {
		t.Error("skipped validation that the add path applies")
	}
	if len(b.calls) != 0 {
		t.Fatalf("touched the firewall on a rejected request: %v", b.calls)
	}
}

// A number past the end of firewalld's listing has to be refused before the
// add, or the edit silently becomes an add.
func TestReplaceRuleRefusesAnUnknownFirewalldRule(t *testing.T) {
	b := editable(BackendFirewalld)
	b.rules = []Rule{{Handle: "svc:ssh"}}
	if _, err := replaceRule(t.Context(), b, 4, RuleRequest{Action: "allow", Port: "22", Protocol: "tcp"}, ""); err == nil {
		t.Fatal("accepted a rule number nothing in the listing has")
	}
	for _, c := range b.calls {
		if strings.HasPrefix(c, "add") {
			t.Fatalf("added a rule for an edit that could not be resolved: %v", b.calls)
		}
	}
}
