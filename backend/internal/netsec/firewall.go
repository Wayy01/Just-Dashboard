// Package netsec exposes the host's firewall, intrusion prevention and active
// login sessions.
//
// Editing firewall rules from a web UI carries an obvious hazard: a bad rule
// can lock the operator out of the very machine they are administering — and
// out of this dashboard with it. Rules that would drop the caller's own
// connection are therefore refused rather than applied, and that guard lives
// here rather than in any one backend so it cannot be forgotten by the next
// one somebody adds.
package netsec

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/Wayy01/Just-Dashboard/backend/internal/hostexec"
)

var (
	ErrNoFirewall = errors.New("no supported firewall was found on this host")
	ErrLockout    = errors.New("this rule would cut off your own connection to the dashboard")
	// ErrReadOnly is returned when the host's firewall can be read but not
	// safely written from here. Reporting it as a distinct condition is the
	// point: "this dashboard will not edit iptables directly" is information,
	// and a greyed-out button with no reason is not.
	ErrReadOnly = errors.New("this firewall backend is read-only from the dashboard")
)

type Backend string

const (
	BackendUFW       Backend = "ufw"
	BackendFirewalld Backend = "firewalld"
	BackendIPTables  Backend = "iptables"
)

// FirewallCapabilities says what this host's firewall can actually be told to
// do from here.
//
// Every backend answers the same questions and they answer them differently:
// ufw has an on/off switch and firewalld has a service, firewalld has named
// zones and ufw has none, and raw iptables has no persistence story at all.
// Rather than pretend one shape fits, the status carries what is possible and
// the UI hides the rest — with a reason, so a missing control is explained
// rather than merely absent.
type FirewallCapabilities struct {
	// Editable covers adding and deleting rules.
	Editable bool `json:"editable"`
	// Toggle is turning the whole firewall on and off.
	Toggle bool `json:"toggle"`
	// DefaultPolicy is changing what happens to unmatched traffic.
	DefaultPolicy bool `json:"defaultPolicy"`
	Logging       bool `json:"logging"`
	Reset         bool `json:"reset"`
	// Profiles reports whether the host defines named service bundles.
	Profiles bool `json:"profiles"`
	// ReadOnlyReason explains a backend that can only be read.
	ReadOnlyReason string `json:"readOnlyReason,omitempty"`
}

type FirewallStatus struct {
	Backend   Backend `json:"backend"`
	Available bool    `json:"available"`
	Enabled   bool    `json:"enabled"`
	// Default is the policy line verbatim, kept because it is what the tool
	// itself prints and an operator may want to read it unmediated.
	Default string `json:"defaultPolicy,omitempty"`
	// Policy is the same thing split into the three directions, so the UI can
	// show "inbound: deny" as a control rather than as prose to be re-read.
	Policy DefaultPolicy `json:"policy"`
	// Logging is the tool's own level ("on (low)", "off"). A firewall that
	// drops silently leaves nothing to look at after an incident, which is
	// why it is worth a line of its own rather than being buried in the raw
	// output.
	Logging string `json:"logging,omitempty"`
	// Zone is firewalld's active zone. Empty for backends with no such idea.
	Zone         string               `json:"zone,omitempty"`
	Capabilities FirewallCapabilities `json:"capabilities"`
	Rules        []Rule               `json:"rules"`
	Raw          string               `json:"raw,omitempty"`
	Error        string               `json:"error,omitempty"`
}

// DefaultPolicy is the three default verdicts. Routed is "disabled" on a host
// that is not forwarding, which is not the same as "deny" and is worth
// reporting as itself.
type DefaultPolicy struct {
	Incoming string `json:"incoming,omitempty"`
	Outgoing string `json:"outgoing,omitempty"`
	Routed   string `json:"routed,omitempty"`
}

type Rule struct {
	Number    int    `json:"number,omitempty"`
	Action    string `json:"action"`
	Protocol  string `json:"protocol,omitempty"`
	From      string `json:"from"`
	To        string `json:"to"`
	Port      string `json:"port,omitempty"`
	Direction string `json:"direction,omitempty"`
	Comment   string `json:"comment,omitempty"`
	// IPv6 marks a backend's duplicate of a rule for the v6 table. ufw prints
	// both and distinguishes them only by a "(v6)" suffix, so a rule list that
	// does not carry the flag reads as every rule having been added twice.
	IPv6 bool `json:"ipv6,omitempty"`
	// Service names the port from the catalogue, so a list of numbers reads
	// as a list of things.
	Service string `json:"service,omitempty"`
	// Danger is the catalogue's warning for a port that is open to everyone
	// and should not be. Attached to the rule rather than computed in the UI
	// so that "which of my rules are the dangerous ones" has one answer.
	Danger string `json:"danger,omitempty"`
	// Handle is how the owning backend identifies this rule when removing it.
	// ufw deletes by number; firewalld has no numbers at all and needs the
	// exact thing back. Never shown, never accepted from a client — the
	// delete route takes the listed number and the backend resolves it.
	Handle string `json:"-"`
	Raw    string `json:"raw"`
}

// fwBackend is one firewall this dashboard knows how to drive.
//
// Validation and the lockout guards deliberately do *not* live here: they are
// applied by Service before dispatching, so a new backend cannot be added
// without them.
type fwBackend interface {
	Kind() Backend
	Detect() bool
	Status(ctx context.Context) (*FirewallStatus, error)
	AddRule(ctx context.Context, req RuleRequest) (string, error)
	DeleteRule(ctx context.Context, number int) (string, error)
	SetEnabled(ctx context.Context, enabled bool) (string, error)
	SetDefaultPolicy(ctx context.Context, direction, policy string) (string, error)
	SetLogging(ctx context.Context, level string) (string, error)
	Reset(ctx context.Context) (string, error)
	Profiles(ctx context.Context) ([]AppProfile, error)
	Capabilities() FirewallCapabilities
}

type Service struct{}

func New() *Service { return &Service{} }

// backends are tried in order. ufw first because a host with both installed is
// almost always a Debian machine where ufw is the one in charge; iptables last
// because it is present everywhere and would otherwise mask the others.
func backends() []fwBackend {
	return []fwBackend{ufwBackend{}, firewalldBackend{}, iptablesBackend{}}
}

func (s *Service) backend() fwBackend {
	for _, b := range backends() {
		if b.Detect() {
			return b
		}
	}
	return nil
}

func (s *Service) Backend() Backend {
	if b := s.backend(); b != nil {
		return b.Kind()
	}
	return ""
}

func (s *Service) Status(ctx context.Context) (*FirewallStatus, error) {
	b := s.backend()
	if b == nil {
		return &FirewallStatus{Rules: []Rule{}, Error: ErrNoFirewall.Error()}, nil
	}
	st, err := b.Status(ctx)
	if err != nil {
		return &FirewallStatus{Backend: b.Kind(), Rules: []Rule{}, Error: err.Error()}, nil
	}
	st.Capabilities = b.Capabilities()
	// Numbers are positional and assigned here rather than by each backend, so
	// "delete rule 4" means the same thing whichever tool is underneath. ufw
	// supplies its own and they are left alone; the others have no notion of
	// one at all.
	for i := range st.Rules {
		if st.Rules[i].Number == 0 {
			st.Rules[i].Number = i + 1
		}
		annotateRule(&st.Rules[i])
	}
	return st, nil
}

// annotateRule attaches the catalogue's name and warning. Done centrally so
// every backend's rules are read the same way.
func annotateRule(r *Rule) {
	if r.Service != "" && r.Danger != "" {
		return
	}
	preset, ok := PresetFor(r.Port, r.Protocol)
	if !ok {
		return
	}
	if r.Service == "" {
		r.Service = preset.Name
	}
	// Only a rule that admits everyone earns the warning. The same port
	// restricted to a private source is the arrangement being recommended,
	// and flagging it would train the operator to ignore the flag.
	if preset.Danger != "" && r.Action == "ALLOW" && isAnywhere(r.From) {
		r.Danger = preset.Danger
	}
}

// isAnywhere reports a source that restricts nothing.
func isAnywhere(from string) bool {
	switch strings.ToLower(strings.TrimSpace(from)) {
	case "", "anywhere", "anywhere (v6)", "0.0.0.0/0", "::/0", "any":
		return true
	}
	return false
}

type RuleRequest struct {
	Action    string `json:"action"`
	Direction string `json:"direction"`
	Port      string `json:"port"`
	Protocol  string `json:"protocol"`
	From      string `json:"from"`
	To        string `json:"to"`
	Comment   string `json:"comment"`
	// Position inserts the rule at a given number instead of appending. ufw
	// evaluates in order and stops at the first match, so a deny added after
	// a broad allow does nothing at all — which looks, from the rule list,
	// exactly like a deny that is working.
	Position int `json:"position,omitempty"`
	// App names an application profile ("Nginx Full", "postgresql") instead
	// of a port. The host's own packages define these, and a rule written in
	// their terms keeps meaning what it says when a package adds a port.
	App string `json:"app,omitempty"`
}

var (
	// A single port, a range, or a comma-separated list — the last only
	// together with a protocol, which is enforced below rather than in the
	// pattern so the error can say why.
	portRe    = regexp.MustCompile(`^\d{1,5}(:\d{1,5})?(,\d{1,5}(:\d{1,5})?)*$`)
	commentRe = regexp.MustCompile(`^[A-Za-z0-9 ._:\-]{0,64}$`)
	// A profile name is free text in a package's own file, so it may contain
	// spaces — but not the characters that would make it something other than
	// a name.
	appNameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9 ._/+-]{0,63}$`)
)

// AddRule validates, guards, and hands the request to whichever firewall this
// host runs. Every component is checked against a strict pattern and passed as
// a separate argument, so nothing the operator types can become part of a
// different command.
func (s *Service) AddRule(ctx context.Context, req RuleRequest, callerIP string) (string, error) {
	b := s.backend()
	if b == nil {
		return "", ErrNoFirewall
	}
	if !b.Capabilities().Editable {
		return "", fmt.Errorf("%w: %s", ErrReadOnly, b.Capabilities().ReadOnlyReason)
	}
	clean, err := normaliseRule(req)
	if err != nil {
		return "", err
	}
	if err := guardLockout(clean.Action, clean.Direction, clean.From, callerIP); err != nil {
		return "", err
	}
	return b.AddRule(ctx, clean)
}

// normaliseRule validates a request and returns it in canonical form.
//
// Separate from AddRule so the rules are one thing to read and one thing to
// test, and so every backend receives the same already-checked shape.
func normaliseRule(req RuleRequest) (RuleRequest, error) {
	req.Action = strings.ToLower(strings.TrimSpace(req.Action))
	switch req.Action {
	case "allow", "deny", "reject", "limit":
	default:
		return req, fmt.Errorf("action must be allow, deny, reject or limit")
	}
	req.Direction = strings.ToLower(strings.TrimSpace(req.Direction))
	if req.Direction == "" {
		req.Direction = "in"
	}
	if req.Direction != "in" && req.Direction != "out" {
		return req, fmt.Errorf("direction must be in or out")
	}
	if req.Port != "" && !portRe.MatchString(req.Port) {
		return req, fmt.Errorf("port must be a number, a range like 8000:8010, or a list like 80,443")
	}
	req.Protocol = strings.ToLower(strings.TrimSpace(req.Protocol))
	if req.Protocol != "" && req.Protocol != "tcp" && req.Protocol != "udp" {
		return req, fmt.Errorf("protocol must be tcp or udp")
	}
	// A list becomes a multiport match, and iptables has no multiport without
	// a protocol. Saying so beats letting the tool reject it three layers down.
	if strings.Contains(req.Port, ",") && req.Protocol == "" {
		return req, fmt.Errorf("a list of ports needs a protocol")
	}
	if req.App != "" {
		if !appNameRe.MatchString(req.App) {
			return req, fmt.Errorf("invalid application profile name")
		}
		if req.Port != "" {
			return req, fmt.Errorf("an application profile already names its ports")
		}
	}
	if req.Port == "" && req.App == "" && req.From == "" {
		return req, fmt.Errorf("a rule needs a port, a profile or a source address")
	}
	if !commentRe.MatchString(req.Comment) {
		return req, fmt.Errorf("comment may only contain letters, digits, spaces and . _ : -")
	}
	if err := validAddress(req.From, "from"); err != nil {
		return req, err
	}
	if err := validAddress(req.To, "to"); err != nil {
		return req, err
	}
	if req.Position < 0 || req.Position > 9999 {
		return req, fmt.Errorf("position must be a rule number")
	}
	return req, nil
}

// validAddress accepts an IP, a CIDR, or the word for "no restriction".
func validAddress(addr, field string) error {
	if addr == "" || strings.EqualFold(addr, "any") {
		return nil
	}
	if _, _, err := net.ParseCIDR(addr); err == nil {
		return nil
	}
	if net.ParseIP(addr) != nil {
		return nil
	}
	return fmt.Errorf("%s must be an IP address or CIDR", field)
}

// guardLockout refuses a deny rule that covers the address the operator is
// connecting from. Applying it would sever the session mid-request and leave
// the machine reachable only through the provider's console.
func guardLockout(action, direction, from, callerIP string) error {
	if direction != "in" || (action != "deny" && action != "reject") {
		return nil
	}
	caller := net.ParseIP(callerIP)
	if caller == nil {
		return nil
	}
	if from == "" {
		return fmt.Errorf("%w: a blanket inbound %s has no source restriction", ErrLockout, action)
	}
	if ip := net.ParseIP(from); ip != nil {
		if ip.Equal(caller) {
			return fmt.Errorf("%w: %s is the address you are connected from", ErrLockout, from)
		}
		return nil
	}
	if _, network, err := net.ParseCIDR(from); err == nil && network.Contains(caller) {
		return fmt.Errorf("%w: %s covers %s, the address you are connected from", ErrLockout, from, callerIP)
	}
	return nil
}

func (s *Service) DeleteRule(ctx context.Context, number int) (string, error) {
	b := s.backend()
	if b == nil {
		return "", ErrNoFirewall
	}
	if !b.Capabilities().Editable {
		return "", fmt.Errorf("%w: %s", ErrReadOnly, b.Capabilities().ReadOnlyReason)
	}
	if number <= 0 {
		return "", fmt.Errorf("rule number must be positive")
	}
	return b.DeleteRule(ctx, number)
}

func (s *Service) SetEnabled(ctx context.Context, enabled bool) (string, error) {
	b := s.backend()
	if b == nil {
		return "", ErrNoFirewall
	}
	if !b.Capabilities().Toggle {
		return "", fmt.Errorf("%w: %s", ErrReadOnly, b.Capabilities().ReadOnlyReason)
	}
	return b.SetEnabled(ctx, enabled)
}

// SetDefaultPolicy changes what happens to a packet no rule matched.
//
// This is the single most consequential control on the page: switching the
// inbound default to deny on a host whose rule list admits nobody takes the
// machine off the network, this dashboard included, in one command. The guard
// below refuses exactly that case, and leaves the ambiguous ones to the typed
// confirmation — a rule list that admits *something* cannot be judged from
// here without knowing which port the operator's browser arrived on.
func (s *Service) SetDefaultPolicy(ctx context.Context, direction, policy string) (string, error) {
	b := s.backend()
	if b == nil {
		return "", ErrNoFirewall
	}
	if !b.Capabilities().DefaultPolicy {
		return "", fmt.Errorf("%w: %s", ErrReadOnly, b.Capabilities().ReadOnlyReason)
	}
	direction = strings.ToLower(strings.TrimSpace(direction))
	policy = strings.ToLower(strings.TrimSpace(policy))
	switch direction {
	case "incoming", "outgoing", "routed":
	default:
		return "", fmt.Errorf("direction must be incoming, outgoing or routed")
	}
	switch policy {
	case "allow", "deny", "reject":
	default:
		return "", fmt.Errorf("policy must be allow, deny or reject")
	}
	if direction == "incoming" && policy != "allow" {
		st, err := s.Status(ctx)
		if err == nil && st.Enabled && !admitsAnything(st.Rules) {
			return "", fmt.Errorf("%w: no inbound allow rule exists, so a default of %s would refuse every connection to this host",
				ErrLockout, policy)
		}
	}
	return b.SetDefaultPolicy(ctx, direction, policy)
}

// admitsAnything reports whether any inbound rule lets a connection in. A rule
// printed without a direction is inbound — the listings only mark exceptions.
func admitsAnything(rules []Rule) bool {
	for _, r := range rules {
		if r.Action == "ALLOW" || r.Action == "LIMIT" {
			if r.Direction == "" || strings.EqualFold(r.Direction, "in") {
				return true
			}
		}
	}
	return false
}

// SetLogging changes how much the firewall writes about what it dropped.
//
// Worth exposing because the default is quiet and the difference matters after
// the fact: a firewall that logs nothing leaves an incident with no record of
// what was refused, and "off" is a choice somebody should have made on purpose
// rather than inherited.
func (s *Service) SetLogging(ctx context.Context, level string) (string, error) {
	b := s.backend()
	if b == nil {
		return "", ErrNoFirewall
	}
	if !b.Capabilities().Logging {
		return "", fmt.Errorf("%w: %s", ErrReadOnly, b.Capabilities().ReadOnlyReason)
	}
	level = strings.ToLower(strings.TrimSpace(level))
	switch level {
	case "off", "on", "low", "medium", "high", "full":
	default:
		return "", fmt.Errorf("logging level must be off, on, low, medium, high or full")
	}
	return b.SetLogging(ctx, level)
}

// Reset removes every rule and returns the firewall to its installed state.
func (s *Service) Reset(ctx context.Context) (string, error) {
	b := s.backend()
	if b == nil {
		return "", ErrNoFirewall
	}
	if !b.Capabilities().Reset {
		return "", fmt.Errorf("%w: this firewall has no reset that is safe to offer", ErrReadOnly)
	}
	return b.Reset(ctx)
}

// AppProfiles lists the named service bundles this host defines — ufw's
// application profiles, firewalld's services.
//
// They are worth surfacing because they are the form the host's own packages
// speak: a rule added as "Nginx Full" keeps meaning what it says if the
// package later adds a port, and reads better in the rule list than 80,443.
func (s *Service) AppProfiles(ctx context.Context) ([]AppProfile, error) {
	b := s.backend()
	if b == nil {
		return []AppProfile{}, nil
	}
	return b.Profiles(ctx)
}

// run invokes a firewall tool on the host. These manage host services, so they
// run there: a copy of ufw or firewall-cmd inside this image would report on
// the container.
func run(ctx context.Context, name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := hostexec.CommandOnHost(ctx, name, args...)
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	if err := cmd.Run(); err != nil {
		var execErr *exec.Error
		if errors.As(err, &execErr) {
			return "", fmt.Errorf("%s is not installed on this host", name)
		}
		return buf.String(), fmt.Errorf("%s: %s", name, strings.TrimSpace(buf.String()))
	}
	return buf.String(), nil
}
