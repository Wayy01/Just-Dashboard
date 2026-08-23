package netsec

import (
	"bufio"
	"context"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/Wayy01/Just-Dashboard/backend/internal/hostexec"
)

// ufw is the Debian and Ubuntu answer, and the one this dashboard was written
// against first. It is a front end to iptables with an on/off switch, numbered
// rules and named application profiles — which is to say, all the affordances
// the other backends have to approximate.
type ufwBackend struct{}

func (ufwBackend) Kind() Backend { return BackendUFW }
func (ufwBackend) Detect() bool  { return hostexec.AvailableOnHost("ufw") }

func (ufwBackend) Capabilities() FirewallCapabilities {
	return FirewallCapabilities{
		Editable: true, Toggle: true, DefaultPolicy: true,
		Logging: true, Reset: true, Profiles: true,
	}
}

// ufwNumberedRe matches ufw's numbered status output:
//
//	[ 1] 22/tcp   ALLOW IN  Anywhere   # ssh
var ufwNumberedRe = regexp.MustCompile(`^\[\s*(\d+)\]\s+(.*)$`)

// Status reads the rule list and the policy block.
//
// Two calls, not one. `ufw status numbered verbose` looks like it should work
// and is rejected outright — ufw's own parser accepts exactly one of the two
// words and raises "Invalid syntax" for both. Asking for the pair therefore
// produced an error, no rules, and a firewall the page reported as inactive on
// every host that actually had one. numbered supplies the rules with the
// numbers the delete route needs; verbose supplies the defaults and the
// logging level, which numbered omits.
func (ufwBackend) Status(ctx context.Context) (*FirewallStatus, error) {
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
	r := Rule{Number: num, Raw: strings.TrimSpace(body), Handle: strconv.Itoa(num)}
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
	return r
}

func (ufwBackend) AddRule(ctx context.Context, req RuleRequest) (string, error) {
	args := []string{}
	if req.Position > 0 {
		args = append(args, "insert", strconv.Itoa(req.Position))
	}
	args = append(args, req.Action, req.Direction)
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
		if req.Protocol != "" {
			args = append(args, "proto", req.Protocol)
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

func (ufwBackend) DeleteRule(ctx context.Context, number int) (string, error) {
	// ufw prompts before deleting; --force answers it. The rule number comes
	// from our own parsed listing, never from free text.
	return run(ctx, "ufw", "--force", "delete", strconv.Itoa(number))
}

func (ufwBackend) SetEnabled(ctx context.Context, enabled bool) (string, error) {
	if enabled {
		return run(ctx, "ufw", "--force", "enable")
	}
	return run(ctx, "ufw", "--force", "disable")
}

func (ufwBackend) SetDefaultPolicy(ctx context.Context, direction, policy string) (string, error) {
	// ufw's argument order is policy first, then direction.
	return run(ctx, "ufw", "default", policy, direction)
}

func (ufwBackend) SetLogging(ctx context.Context, level string) (string, error) {
	return run(ctx, "ufw", "logging", level)
}

// Reset also disables the firewall, which is why it is behind the same typed
// confirmation as the disable toggle rather than treated as a tidy-up.
func (ufwBackend) Reset(ctx context.Context) (string, error) {
	return run(ctx, "ufw", "--force", "reset")
}

// Profiles expands `ufw app list`, which gives only names, with `ufw app info`
// per entry. The names come from ufw's own listing and are checked against a
// pattern before being passed back to it — the file they come from is writable
// by root, and root is not the same as this code.
func (ufwBackend) Profiles(ctx context.Context) ([]AppProfile, error) {
	out, err := run(ctx, "ufw", "app", "list")
	if err != nil {
		return nil, err
	}
	profiles := []AppProfile{}
	for _, name := range parseAppList(out) {
		if !appNameRe.MatchString(name) {
			continue
		}
		p := AppProfile{Name: name, Ports: []string{}}
		if info, err := run(ctx, "ufw", "app", "info", name); err == nil {
			p.Title, p.Description, p.Ports = parseAppInfo(info)
		}
		profiles = append(profiles, p)
	}
	sort.Slice(profiles, func(i, j int) bool { return profiles[i].Name < profiles[j].Name })
	return profiles, nil
}
