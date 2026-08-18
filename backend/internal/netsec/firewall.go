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

	"github.com/Wayy01/vps-dashboard/backend/internal/hostexec"
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
	Default   string  `json:"defaultPolicy,omitempty"`
	Rules     []Rule  `json:"rules"`
	Raw       string  `json:"raw,omitempty"`
	Error     string  `json:"error,omitempty"`
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
	Raw       string `json:"raw"`
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

func (s *Service) ufwStatus(ctx context.Context) (*FirewallStatus, error) {
	out, err := run(ctx, "ufw", "status", "numbered", "verbose")
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
		if after, ok := strings.CutPrefix(line, "Default:"); ok {
			st.Default = strings.TrimSpace(after)
			continue
		}
		m := ufwNumberedRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		num, _ := strconv.Atoi(m[1])
		st.Rules = append(st.Rules, parseUFWRule(num, m[2]))
	}
	return st, nil
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
	if to, proto, ok := strings.Cut(r.To, "/"); ok {
		r.Port, r.Protocol = to, proto
	}
	return r
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
	Comment   string `json:"comment"`
}

var (
	portRe    = regexp.MustCompile(`^\d{1,5}(:\d{1,5})?$`)
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
		return "", fmt.Errorf("port must be a number or a range like 8000:8010")
	}
	proto := strings.ToLower(strings.TrimSpace(req.Protocol))
	if proto != "" && proto != "tcp" && proto != "udp" {
		return "", fmt.Errorf("protocol must be tcp or udp")
	}
	if !commentRe.MatchString(req.Comment) {
		return "", fmt.Errorf("comment may only contain letters, digits, spaces and . _ : -")
	}
	if req.From != "" {
		if _, _, err := net.ParseCIDR(req.From); err != nil && net.ParseIP(req.From) == nil {
			return "", fmt.Errorf("from must be an IP address or CIDR")
		}
	}
	if err := guardLockout(action, direction, req.From, callerIP); err != nil {
		return "", err
	}

	args := []string{action, direction}
	if req.From != "" {
		args = append(args, "from", req.From)
	}
	if req.Port != "" {
		args = append(args, "to", "any", "port", req.Port)
		if proto != "" {
			args = append(args, "proto", proto)
		}
	}
	if req.Comment != "" {
		args = append(args, "comment", req.Comment)
	}
	return run(ctx, "ufw", args...)
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
