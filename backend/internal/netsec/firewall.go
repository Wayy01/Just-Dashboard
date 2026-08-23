// Package netsec exposes the host's firewall, intrusion prevention and active
// login sessions.
//
// Editing firewall rules from a web UI carries an obvious hazard: a bad rule
// can lock the operator out of the very machine they are administering — and
// out of this dashboard with it. Rules that would drop the caller's own
// connection are therefore refused rather than applied.
package netsec

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/Wayy01/Just-Dashboard/backend/internal/hostexec"
)

var (
	ErrNoFirewall = errors.New("neither ufw nor iptables was found on this host")
	ErrLockout    = errors.New("this rule would cut off your own connection to the dashboard")
)

type Backend string

const (
	BackendUFW      Backend = "ufw"
	BackendIPTables Backend = "iptables"
)

type FirewallStatus struct {
	Backend   Backend `json:"backend"`
	Available bool    `json:"available"`
	Enabled   bool    `json:"enabled"`
	// Default is the policy line verbatim, kept because it is what ufw
	// itself prints and an operator may want to read it unmediated.
	Default string `json:"defaultPolicy,omitempty"`
	// Policy is the same thing split into the three directions, so the UI can
	// show "inbound: deny" as a control rather than as prose to be re-read.
	Policy DefaultPolicy `json:"policy"`
	// Logging is ufw's own level ("on (low)", "off"). A firewall that drops
	// silently leaves nothing to look at after an incident, which is why it
	// is worth a line of its own rather than being buried in the raw output.
	Logging string `json:"logging,omitempty"`
	Rules   []Rule `json:"rules"`
	Raw     string `json:"raw,omitempty"`
	Error   string `json:"error,omitempty"`
}

// DefaultPolicy is ufw's three default verdicts. Routed is "disabled" on a
// host that is not forwarding, which is not the same as "deny" and is worth
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
	// IPv6 marks ufw's duplicate of a rule for the v6 table. ufw prints both
	// and distinguishes them only by a "(v6)" suffix, so a rule list that
	// does not carry the flag reads as every rule having been added twice.
	IPv6 bool `json:"ipv6,omitempty"`
	// Service names the port from the catalogue, so a list of numbers reads
	// as a list of things.
	Service string `json:"service,omitempty"`
	// Danger is the catalogue's warning for a port that is open to everyone
	// and should not be. Attached to the rule rather than computed in the UI
	// so that "which of my rules are the dangerous ones" has one answer.
	Danger string `json:"danger,omitempty"`
	Raw    string `json:"raw"`
}

type Service struct{}

func New() *Service { return &Service{} }

func (s *Service) Backend() Backend {
	if hostexec.AvailableOnHost("ufw") {
		return BackendUFW
	}
	return BackendIPTables
}

func (s *Service) Status(ctx context.Context) (*FirewallStatus, error) {
	if hostexec.AvailableOnHost("ufw") {
		return s.ufwStatus(ctx)
	}
	if hostexec.AvailableOnHost("iptables") {
		return s.iptablesStatus(ctx)
	}
	return &FirewallStatus{Rules: []Rule{}, Error: ErrNoFirewall.Error()}, nil
}

// ufwNumberedRe matches ufw's numbered status output:
//
//	[ 1] 22/tcp   ALLOW IN  Anywhere   # ssh
var ufwNumberedRe = regexp.MustCompile(`^\[\s*(\d+)\]\s+(.*)$`)

// ufwStatus reads the rule list and the policy block.
//
// Two calls, not one. `ufw status numbered verbose` looks like it should work
// and is rejected outright — ufw's own parser accepts exactly one of the two
// words and raises "Invalid syntax" for both. Asking for the pair therefore
// produced an error, no rules, and a firewall the page reported as inactive on
// every host that actually had one. numbered supplies the rules with the
// numbers the delete route needs; verbose supplies the defaults and the
// logging level, which numbered omits.
func (s *Service) ufwStatus(ctx context.Context) (*FirewallStatus, error) {
	out, err := run(ctx, "ufw", "status", "numbered")
	st := &FirewallStatus{Backend: BackendUFW, Available: true, Rules: []Rule{}, Raw: out}
	if err != nil {
		st.Error = err.Error()
		return st, nil
	}
	sc := bufio.NewScanner(strings.NewReader(out))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		if after, ok := strings.CutPrefix(line, "Status:"); ok {
			st.Enabled = strings.TrimSpace(after) == "active"
			continue
		}
		m := ufwNumberedRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		num, _ := strconv.Atoi(m[1])
		st.Rules = append(st.Rules, parseUFWRule(num, m[2]))
	}
	// The verbose block is a second call and a soft failure: rules without a
	// policy line is a worse page than rules with one, but far better than
	// the error the combined call produced.
	if verbose, err := run(ctx, "ufw", "status", "verbose"); err == nil {
		st.Raw = out + "\n" + verbose
		applyUFWVerbose(st, verbose)
	}
	return st, nil
}

// applyUFWVerbose reads the header block ufw prints under `status verbose`.
func applyUFWVerbose(st *FirewallStatus, out string) {
	sc := bufio.NewScanner(strings.NewReader(out))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		switch {
		case strings.HasPrefix(line, "Status:"):
			st.Enabled = strings.TrimSpace(strings.TrimPrefix(line, "Status:")) == "active"
		case strings.HasPrefix(line, "Logging:"):
			st.Logging = strings.TrimSpace(strings.TrimPrefix(line, "Logging:"))
		case strings.HasPrefix(line, "Default:"):
			st.Default = strings.TrimSpace(strings.TrimPrefix(line, "Default:"))
			st.Policy = parseDefaultPolicy(st.Default)
		}
	}
}

// parseDefaultPolicy splits ufw's one-line summary of its three defaults:
//
//	deny (incoming), allow (outgoing), disabled (routed)
func parseDefaultPolicy(line string) DefaultPolicy {
	var p DefaultPolicy
	for _, part := range strings.Split(line, ",") {
		part = strings.TrimSpace(part)
		verdict, rest, ok := strings.Cut(part, " ")
		if !ok {
			continue
		}
		switch strings.Trim(strings.TrimSpace(rest), "()") {
		case "incoming":
			p.Incoming = verdict
		case "outgoing":
			p.Outgoing = verdict
		case "routed":
			p.Routed = verdict
		}
	}
	return p
}

func parseUFWRule(num int, body string) Rule {
	r := Rule{Number: num, Raw: strings.TrimSpace(body)}
	if idx := strings.Index(body, "#"); idx >= 0 {
		r.Comment = strings.TrimSpace(body[idx+1:])
		body = body[:idx]
	}
	fields := strings.Fields(body)
	// ufw's layout is: <to> <ACTION> <IN|OUT> <from>
	for i, f := range fields {
		upper := strings.ToUpper(f)
		if upper == "ALLOW" || upper == "DENY" || upper == "REJECT" || upper == "LIMIT" {
			r.Action = upper
			r.To = strings.Join(fields[:i], " ")
			rest := fields[i+1:]
			if len(rest) > 0 && (rest[0] == "IN" || rest[0] == "OUT") {
				r.Direction = rest[0]
				rest = rest[1:]
			}
			r.From = strings.Join(rest, " ")
			break
		}
	}
	if r.Action == "" {
		r.Action = "UNKNOWN"
		r.To = strings.TrimSpace(body)
	}
	// ufw appends "(v6)" to the destination of the IPv6 half of a rule. Left
	// in the To field it makes the same rule look like two different ones.
	if trimmed, ok := strings.CutSuffix(r.To, " (v6)"); ok {
		r.IPv6, r.To = true, trimmed
	}
	if trimmed, ok := strings.CutSuffix(r.From, " (v6)"); ok {
		r.IPv6, r.From = true, trimmed
	}
	if to, proto, ok := strings.Cut(r.To, "/"); ok {
		r.Port, r.Protocol = to, proto
	}
	if preset, ok := PresetFor(r.Port, r.Protocol); ok {
		r.Service = preset.Name
		// Only a rule that admits everyone earns the warning. The same port
		// restricted to a private source is the arrangement being
		// recommended, and flagging it would train the operator to ignore
		// the flag.
		if preset.Danger != "" && r.Action == "ALLOW" && isAnywhere(r.From) {
			r.Danger = preset.Danger
		}
	}
	return r
}

// isAnywhere reports a source that restricts nothing. ufw prints the word for
// an unrestricted rule and the CIDR for a default-route one.
func isAnywhere(from string) bool {
	switch strings.ToLower(strings.TrimSpace(from)) {
	case "", "anywhere", "anywhere (v6)", "0.0.0.0/0", "::/0":
		return true
	}
	return false
}

func (s *Service) iptablesStatus(ctx context.Context) (*FirewallStatus, error) {
	out, err := run(ctx, "iptables", "-L", "-n", "-v", "--line-numbers")
	st := &FirewallStatus{Backend: BackendIPTables, Available: true, Rules: []Rule{}, Raw: out}
	if err != nil {
		st.Error = err.Error()
		return st, nil
	}
	chain := ""
	sc := bufio.NewScanner(strings.NewReader(out))
	for sc.Scan() {
		line := sc.Text()
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "Chain ") {
			fields := strings.Fields(trimmed)
			if len(fields) > 1 {
				chain = fields[1]
			}
			if strings.Contains(trimmed, "policy") && chain == "INPUT" {
				if idx := strings.Index(trimmed, "policy "); idx >= 0 {
					st.Default = strings.Trim(strings.Fields(trimmed[idx+7:])[0], "()")
				}
			}
			continue
		}
		if strings.HasPrefix(trimmed, "num ") || strings.HasPrefix(trimmed, "pkts ") {
			continue
		}
		fields := strings.Fields(trimmed)
		if len(fields) < 9 {
			continue
		}
		num, err := strconv.Atoi(fields[0])
		if err != nil {
			continue
		}
		st.Rules = append(st.Rules, Rule{
			Number: num, Action: fields[3], Protocol: fields[4],
			From: fields[7], To: fields[8], Direction: chain,
			Raw: trimmed,
		})
	}
	// A default DROP with rules present is the practical definition of
	// "enabled" for raw iptables, which has no on/off switch of its own.
	st.Enabled = st.Default == "DROP" || len(st.Rules) > 0
	return st, nil
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
	// App names a ufw application profile ("Nginx Full") instead of a port.
	// The host's own packages define these, and a rule written in their terms
	// keeps meaning what it says when a package later adds a port.
	App string `json:"app,omitempty"`
}

var (
	// ufw takes a single port, a range, or a comma-separated list — the last
	// only together with a protocol, which is enforced below rather than in
	// the pattern so the error can say why.
	portRe    = regexp.MustCompile(`^\d{1,5}(:\d{1,5})?(,\d{1,5}(:\d{1,5})?)*$`)
	commentRe = regexp.MustCompile(`^[A-Za-z0-9 ._:\-]{0,64}$`)
)

// AddRule appends a ufw rule. Every component is validated against a strict
// pattern and passed as a separate argument, so nothing the operator types can
// become part of a different command.
func (s *Service) AddRule(ctx context.Context, req RuleRequest, callerIP string) (string, error) {
	action := strings.ToLower(strings.TrimSpace(req.Action))
	switch action {
	case "allow", "deny", "reject", "limit":
	default:
		return "", fmt.Errorf("action must be allow, deny, reject or limit")
	}
	direction := strings.ToLower(strings.TrimSpace(req.Direction))
	if direction == "" {
		direction = "in"
	}
	if direction != "in" && direction != "out" {
		return "", fmt.Errorf("direction must be in or out")
	}
	if req.Port != "" && !portRe.MatchString(req.Port) {
		return "", fmt.Errorf("port must be a number, a range like 8000:8010, or a list like 80,443")
	}
	proto := strings.ToLower(strings.TrimSpace(req.Protocol))
	if proto != "" && proto != "tcp" && proto != "udp" {
		return "", fmt.Errorf("protocol must be tcp or udp")
	}
	// ufw builds a multiport match for a list, and iptables has no multiport
	// without a protocol. Saying so beats letting ufw reject it in its own
	// words three layers down.
	if strings.Contains(req.Port, ",") && proto == "" {
		return "", fmt.Errorf("a list of ports needs a protocol")
	}
	if req.App != "" {
		if !appNameRe.MatchString(req.App) {
			return "", fmt.Errorf("invalid application profile name")
		}
		if req.Port != "" {
			return "", fmt.Errorf("an application profile already names its ports")
		}
	}
	if !commentRe.MatchString(req.Comment) {
		return "", fmt.Errorf("comment may only contain letters, digits, spaces and . _ : -")
	}
	if err := validAddress(req.From, "from"); err != nil {
		return "", err
	}
	if err := validAddress(req.To, "to"); err != nil {
		return "", err
	}
	if req.Position < 0 || req.Position > 9999 {
		return "", fmt.Errorf("position must be a rule number")
	}
	if err := guardLockout(action, direction, req.From, callerIP); err != nil {
		return "", err
	}

	args := []string{}
	if req.Position > 0 {
		args = append(args, "insert", strconv.Itoa(req.Position))
	}
	args = append(args, action, direction)
	if req.From != "" {
		args = append(args, "from", req.From)
	} else if req.To != "" || req.Port != "" {
		// ufw's grammar wants a source before a destination, and "any" is how
		// it spells "unrestricted" in that position.
		args = append(args, "from", "any")
	}
	if req.To != "" || req.Port != "" {
		dest := "any"
		if req.To != "" {
			dest = req.To
		}
		args = append(args, "to", dest)
		if req.Port != "" {
			args = append(args, "port", req.Port)
		}
		if proto != "" {
			args = append(args, "proto", proto)
		}
	}
	if req.App != "" {
		args = append(args, "app", req.App)
	}
	if req.Comment != "" {
		args = append(args, "comment", req.Comment)
	}
	return run(ctx, "ufw", args...)
}

// validAddress accepts an IP, a CIDR, or ufw's own word for "no restriction".
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
	if number <= 0 {
		return "", fmt.Errorf("rule number must be positive")
	}
	// ufw prompts before deleting; --force answers it. The rule number comes
	// from our own parsed listing, never from free text.
	return run(ctx, "ufw", "--force", "delete", strconv.Itoa(number))
}

// SetDefaultPolicy changes what happens to a packet no rule matched.
//
// This is the single most consequential control on the page: switching the
// inbound default to deny on a host whose rule list admits nobody takes the
// machine off the network, this dashboard included, in one command. The guard
// below refuses exactly that case — a host with no inbound allow at all — and
// leaves the ambiguous ones to the typed confirmation, because a rule list
// that admits *something* cannot be judged from here without knowing which
// port the operator's browser arrived on.
func (s *Service) SetDefaultPolicy(ctx context.Context, direction, policy string) (string, error) {
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
	// ufw's argument order is policy first, then direction.
	return run(ctx, "ufw", "default", policy, direction)
}

// admitsAnything reports whether any inbound rule lets a connection in. ufw
// leaves Direction empty on the rules it prints without one, and those are
// inbound — the listing only marks the exceptions.
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

// SetLogging changes how much ufw writes about what it dropped.
//
// Worth exposing because the default is low and the difference matters after
// the fact: a firewall that logs nothing leaves an incident with no record of
// what was refused, and "off" is a choice somebody should have made on
// purpose rather than inherited.
func (s *Service) SetLogging(ctx context.Context, level string) (string, error) {
	level = strings.ToLower(strings.TrimSpace(level))
	switch level {
	case "off", "on", "low", "medium", "high", "full":
	default:
		return "", fmt.Errorf("logging level must be off, on, low, medium, high or full")
	}
	return run(ctx, "ufw", "logging", level)
}

// Reset removes every rule and returns ufw to its installed state.
//
// It also disables the firewall, which is why it is behind the same typed
// confirmation as the disable toggle rather than treated as a tidy-up.
func (s *Service) Reset(ctx context.Context) (string, error) {
	return run(ctx, "ufw", "--force", "reset")
}

func (s *Service) SetEnabled(ctx context.Context, enabled bool) (string, error) {
	if enabled {
		return run(ctx, "ufw", "--force", "enable")
	}
	return run(ctx, "ufw", "--force", "disable")
}

func run(ctx context.Context, name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	// These manage host services, so they run on the host. A copy of ufw or
	// fail2ban inside this image would otherwise report on the container.
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
