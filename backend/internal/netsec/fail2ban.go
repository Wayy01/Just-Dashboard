package netsec

import (
	"context"
	"fmt"
	"net"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/Wayy01/Just-Dashboard/backend/internal/hostexec"
)

type Jail struct {
	Name         string   `json:"name"`
	Currently    int      `json:"currentlyFailed"`
	TotalFailed  int      `json:"totalFailed"`
	CurrentlyBan int      `json:"currentlyBanned"`
	TotalBanned  int      `json:"totalBanned"`
	BannedIPs    []string `json:"bannedIps"`
	FileList     []string `json:"fileList"`
}

type Fail2banStatus struct {
	Available bool   `json:"available"`
	Running   bool   `json:"running"`
	Jails     []Jail `json:"jails"`
	Error     string `json:"error,omitempty"`
}

func (s *Service) Fail2banAvailable() bool {
	return hostexec.AvailableOnHost("fail2ban-client")
}

// Fail2banStatus queries fail2ban-client. Its output is a fixed-shape text
// report rather than JSON, so it is parsed by the labels it prints.
func (s *Service) Fail2banStatus(ctx context.Context) (*Fail2banStatus, error) {
	st := &Fail2banStatus{Jails: []Jail{}}
	if !s.Fail2banAvailable() {
		return st, nil
	}
	st.Available = true
	out, err := run(ctx, "fail2ban-client", "status")
	if err != nil {
		st.Error = err.Error()
		return st, nil
	}
	st.Running = true
	names := parseJailList(out)
	for _, name := range names {
		jail, err := s.jailStatus(ctx, name)
		if err != nil {
			continue
		}
		st.Jails = append(st.Jails, *jail)
	}
	sort.Slice(st.Jails, func(i, j int) bool { return st.Jails[i].Name < st.Jails[j].Name })
	return st, nil
}

func parseJailList(out string) []string {
	for _, line := range strings.Split(out, "\n") {
		if !strings.Contains(line, "Jail list:") {
			continue
		}
		_, after, _ := strings.Cut(line, "Jail list:")
		names := []string{}
		for _, n := range strings.Split(after, ",") {
			if n = strings.TrimSpace(n); n != "" {
				names = append(names, n)
			}
		}
		return names
	}
	return nil
}

var jailNameRe = regexp.MustCompile(`^[A-Za-z0-9._\-]{1,64}$`)

func (s *Service) jailStatus(ctx context.Context, name string) (*Jail, error) {
	if !jailNameRe.MatchString(name) {
		return nil, fmt.Errorf("invalid jail name %q", name)
	}
	out, err := run(ctx, "fail2ban-client", "status", name)
	if err != nil {
		return nil, err
	}
	j := &Jail{Name: name, BannedIPs: []string{}, FileList: []string{}}
	for _, line := range strings.Split(out, "\n") {
		label, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		value = strings.TrimSpace(value)
		switch {
		case strings.Contains(label, "Currently failed"):
			j.Currently = atoi(value)
		case strings.Contains(label, "Total failed"):
			j.TotalFailed = atoi(value)
		case strings.Contains(label, "Currently banned"):
			j.CurrentlyBan = atoi(value)
		case strings.Contains(label, "Total banned"):
			j.TotalBanned = atoi(value)
		case strings.Contains(label, "Banned IP list"):
			j.BannedIPs = strings.Fields(value)
		case strings.Contains(label, "File list"):
			j.FileList = strings.Fields(value)
		}
	}
	return j, nil
}

func atoi(s string) int {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return 0
	}
	return n
}

// Unban releases one address from a jail. Both arguments are validated: the
// jail name against a name pattern and the address by actually parsing it as
// an IP, so neither can carry anything but what it claims to be.
func (s *Service) Unban(ctx context.Context, jail, ip string) (string, error) {
	if !jailNameRe.MatchString(jail) {
		return "", fmt.Errorf("invalid jail name %q", jail)
	}
	if net.ParseIP(ip) == nil {
		return "", fmt.Errorf("%q is not a valid IP address", ip)
	}
	return run(ctx, "fail2ban-client", "set", jail, "unbanip", ip)
}

func (s *Service) Ban(ctx context.Context, jail, ip string) (string, error) {
	if !jailNameRe.MatchString(jail) {
		return "", fmt.Errorf("invalid jail name %q", jail)
	}
	if net.ParseIP(ip) == nil {
		return "", fmt.Errorf("%q is not a valid IP address", ip)
	}
	return run(ctx, "fail2ban-client", "set", jail, "banip", ip)
}

// Everything below is jail control rather than jail reporting.
//
// A dashboard that can only unban one address at a time is a viewer with a
// button. The jail's own parameters — how many failures, in what window, for
// how long — are the knobs that decide whether fail2ban is doing anything at
// all, and they live in a file under /etc/fail2ban that is a different shape
// on every distribution. fail2ban-client can read and set them on the running
// server, which is both easier to get right and honest about what is in force
// now rather than what a file says.

// JailConfig is a jail's working parameters.
type JailConfig struct {
	Name string `json:"name"`
	// BanTime, FindTime are seconds. MaxRetry is a count. Together they are
	// the whole policy: this many failures inside this window earns this long
	// a ban.
	BanTime  int      `json:"banTime"`
	FindTime int      `json:"findTime"`
	MaxRetry int      `json:"maxRetry"`
	IgnoreIP []string `json:"ignoreIp"`
	Actions  []string `json:"actions"`
	Error    string   `json:"error,omitempty"`
}

func (s *Service) JailConfig(ctx context.Context, jail string) (*JailConfig, error) {
	if !jailNameRe.MatchString(jail) {
		return nil, fmt.Errorf("invalid jail name %q", jail)
	}
	cfg := &JailConfig{Name: jail, IgnoreIP: []string{}, Actions: []string{}}
	get := func(param string) string {
		out, err := run(ctx, "fail2ban-client", "get", jail, param)
		if err != nil {
			return ""
		}
		return strings.TrimSpace(out)
	}
	cfg.BanTime = atoi(get("bantime"))
	cfg.FindTime = atoi(get("findtime"))
	cfg.MaxRetry = atoi(get("maxretry"))
	cfg.IgnoreIP = parseClientList(get("ignoreip"))
	cfg.Actions = parseClientList(get("actions"))
	return cfg, nil
}

// parseClientList reads the Python-ish list fail2ban-client prints for the
// multi-valued parameters: `['127.0.0.1/8', '::1']`, or a bare space-separated
// line on older builds. Both shapes appear in the wild and neither is JSON.
func parseClientList(out string) []string {
	out = strings.TrimSpace(out)
	items := []string{}
	if strings.HasPrefix(out, "[") && strings.HasSuffix(out, "]") {
		out = strings.TrimSuffix(strings.TrimPrefix(out, "["), "]")
		for _, part := range strings.Split(out, ",") {
			part = strings.TrimSpace(part)
			part = strings.Trim(part, `'"`)
			if part != "" {
				items = append(items, part)
			}
		}
		return items
	}
	for _, part := range strings.Fields(out) {
		if part != "" {
			items = append(items, part)
		}
	}
	return items
}

// jailParams is the closed set of parameters this dashboard will set, with
// the bounds each is allowed. Closed for the same reason the sshd list is:
// every entry is a command this code is willing to run against the running
// intrusion-prevention service.
var jailParams = map[string]struct{ min, max int }{
	"bantime":  {60, 31536000},
	"findtime": {30, 604800},
	"maxretry": {1, 100},
}

// SetJailParam changes one of a jail's parameters on the running server.
//
// The change lives until fail2ban restarts, which is deliberate and is said so
// in the UI: writing it into jail.local would mean parsing and rewriting a
// file whose layout differs by distribution, and getting that wrong disables
// intrusion prevention silently. Tightening a jail from here and then making
// it permanent in the file is the honest workflow.
func (s *Service) SetJailParam(ctx context.Context, jail, param string, value int) (string, error) {
	if !jailNameRe.MatchString(jail) {
		return "", fmt.Errorf("invalid jail name %q", jail)
	}
	bounds, ok := jailParams[strings.ToLower(param)]
	if !ok {
		return "", fmt.Errorf("%q is not a parameter this dashboard sets", param)
	}
	if value < bounds.min || value > bounds.max {
		return "", fmt.Errorf("%s must be between %d and %d", param, bounds.min, bounds.max)
	}
	return run(ctx, "fail2ban-client", "set", jail, strings.ToLower(param), strconv.Itoa(value))
}

// There is deliberately no start/stop for a jail. fail2ban-client's status
// lists only the jails that are running, so a jail stopped from here would
// vanish from every listing with nothing left to start it again — a control
// that can only be used once is a trap, not a feature.

// IgnoreIP adds or removes an address from a jail's allowlist. This is the
// control an operator reaches for the moment they ban themselves, so it is
// worth having on the page rather than in a file.
func (s *Service) IgnoreIP(ctx context.Context, jail, ip string, add bool) (string, error) {
	if !jailNameRe.MatchString(jail) {
		return "", fmt.Errorf("invalid jail name %q", jail)
	}
	// A CIDR is as valid here as a single address, and is what somebody
	// allowlisting their office wants.
	if net.ParseIP(ip) == nil {
		if _, _, err := net.ParseCIDR(ip); err != nil {
			return "", fmt.Errorf("%q is not a valid IP address or CIDR", ip)
		}
	}
	verb := "addignoreip"
	if !add {
		verb = "delignoreip"
	}
	return run(ctx, "fail2ban-client", "set", jail, verb, ip)
}

// UnbanAll releases every address a jail currently holds.
//
// One call per address rather than `unbanip --all`, which only exists from
// fail2ban 0.10 and fails with a syntax error on the older builds still
// shipping in long-term releases. The list comes from our own parse of the
// jail's status, so nothing free-form reaches the client.
func (s *Service) UnbanAll(ctx context.Context, jail string) (int, error) {
	if !jailNameRe.MatchString(jail) {
		return 0, fmt.Errorf("invalid jail name %q", jail)
	}
	status, err := s.jailStatus(ctx, jail)
	if err != nil {
		return 0, err
	}
	unbanned := 0
	for _, ip := range status.BannedIPs {
		if _, err := s.Unban(ctx, jail, ip); err == nil {
			unbanned++
		}
	}
	return unbanned, nil
}
