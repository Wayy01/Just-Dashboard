package netsec

import (
	"bufio"
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/Wayy01/Just-Dashboard/backend/internal/hostexec"
)

// firewalld runs Fedora, RHEL and everything descended from it — Rocky, Alma,
// CentOS Stream, Oracle Linux — plus openSUSE. That is most of the server
// installations this dashboard will ever meet outside the Debian family, and
// until now every one of them saw a Security page that could read a rule list
// and change nothing.
//
// Its model is genuinely different from ufw's and the difference is the work.
// There are no rule numbers: a zone holds a set of services, a set of ports
// and a list of "rich rules", and each is removed by handing back the exact
// thing that was added. Numbers are assigned by this dashboard over the listed
// order so that "delete rule 4" means something, and the handle is what
// actually gets removed.
//
// Everything is written --permanent and then reloaded, because a runtime-only
// change is a firewall rule that disappears at the next boot — the same trap
// the fail2ban jail control was rejected for.
type firewalldBackend struct{}

func (firewalldBackend) Kind() Backend { return BackendFirewalld }
func (firewalldBackend) Detect() bool  { return hostexec.AvailableOnHost("firewall-cmd") }

func (firewalldBackend) Capabilities() FirewallCapabilities {
	return FirewallCapabilities{
		Editable: true, Toggle: true, DefaultPolicy: true,
		Logging: true, Profiles: true,
		// firewalld has no reset. Removing everything a zone holds one call
		// at a time is not the same operation and would leave a half-reset
		// firewall behind if any of them failed.
		Reset: false,
	}
}

var zoneNameRe = regexp.MustCompile(`^[A-Za-z0-9_-]{1,32}$`)

// zone reads the active zone, which is where rules are added and removed.
// Everything else in this backend takes it as an argument so the calls are
// explicit about which zone they touch.
func (firewalldBackend) zone(ctx context.Context) string {
	out, err := run(ctx, "firewall-cmd", "--get-default-zone")
	if err != nil {
		return "public"
	}
	name := strings.TrimSpace(out)
	if !zoneNameRe.MatchString(name) {
		return "public"
	}
	return name
}

func (b firewalldBackend) Status(ctx context.Context) (*FirewallStatus, error) {
	st := &FirewallStatus{Backend: BackendFirewalld, Available: true, Rules: []Rule{}}

	// --state exits non-zero when the daemon is stopped, which is a state
	// rather than a failure: a stopped firewalld is exactly what the page
	// needs to show.
	state, _ := run(ctx, "firewall-cmd", "--state")
	st.Enabled = strings.TrimSpace(state) == "running"

	zone := b.zone(ctx)
	st.Zone = zone
	if !st.Enabled {
		st.Default = "firewalld is not running"
		return st, nil
	}

	out, err := run(ctx, "firewall-cmd", "--zone="+zone, "--list-all")
	if err != nil {
		st.Error = err.Error()
		return st, nil
	}
	st.Raw = out
	target, rules := parseFirewalldZone(out)
	st.Rules = rules
	st.Policy = firewalldPolicy(target)
	st.Default = fmt.Sprintf("%s (zone %s)", target, zone)

	if logged, err := run(ctx, "firewall-cmd", "--get-log-denied"); err == nil {
		st.Logging = strings.TrimSpace(logged)
	}
	return st, nil
}

// firewalldPolicy translates a zone target into the three directions.
//
// The one worth spelling out is "default", which is firewalld's own word for
// "reject anything not explicitly allowed" — reading it as "no policy set"
// would report the safest configuration as the most permissive one. firewalld
// does not filter outgoing traffic in any zone, so saying so is more useful
// than leaving the field blank and letting the UI imply a restriction.
func firewalldPolicy(target string) DefaultPolicy {
	p := DefaultPolicy{Outgoing: "allow", Routed: "disabled"}
	switch strings.ToUpper(strings.Trim(target, "%")) {
	case "ACCEPT":
		p.Incoming = "allow"
	case "DROP":
		p.Incoming = "deny"
	case "REJECT", "DEFAULT", "":
		p.Incoming = "reject"
	default:
		p.Incoming = strings.ToLower(target)
	}
	return p
}

// parseFirewalldZone reads `firewall-cmd --list-all`, whose shape is a set of
// "key: values" lines with the rich rules indented underneath their own.
func parseFirewalldZone(out string) (target string, rules []Rule) {
	rules = []Rule{}
	target = "default"
	inRich := false
	sc := bufio.NewScanner(strings.NewReader(out))
	for sc.Scan() {
		line := sc.Text()
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		// A rich rule is indented under the "rich rules:" heading and is the
		// only multi-line section, so the indent is what ends it.
		if inRich {
			if strings.HasPrefix(line, "\t") || strings.HasPrefix(line, "  ") {
				if r, ok := parseRichRule(trimmed); ok {
					rules = append(rules, r)
				}
				continue
			}
			inRich = false
		}
		key, value, ok := strings.Cut(trimmed, ":")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		switch key {
		case "target":
			target = value
		case "services":
			for _, svc := range strings.Fields(value) {
				rules = append(rules, Rule{
					Action: "ALLOW", Direction: "IN", From: "Anywhere",
					To: svc, Service: svc, Handle: "service:" + svc,
					Raw: "service " + svc,
				})
			}
		case "ports":
			for _, port := range strings.Fields(value) {
				r := Rule{
					Action: "ALLOW", Direction: "IN", From: "Anywhere",
					To: port, Handle: "port:" + port, Raw: "port " + port,
				}
				r.Port, r.Protocol, _ = strings.Cut(port, "/")
				rules = append(rules, r)
			}
		case "rich rules":
			inRich = true
			if value != "" {
				if r, ok := parseRichRule(value); ok {
					rules = append(rules, r)
				}
			}
		}
	}
	return target, rules
}

var richAttrRe = regexp.MustCompile(`(\w[\w-]*)="([^"]*)"`)

// parseRichRule reads one of firewalld's rich rules:
//
//	rule family="ipv4" source address="10.0.0.0/8" port port="5432" protocol="tcp" accept
//
// Attributes are read as key/value pairs rather than by position, because
// firewalld emits them in a canonical order that is not the order they were
// written in and has changed between releases.
func parseRichRule(raw string) (Rule, bool) {
	if !strings.HasPrefix(raw, "rule") {
		return Rule{}, false
	}
	r := Rule{Direction: "IN", Raw: raw, Handle: "rich:" + raw}
	attrs := map[string]string{}
	for _, m := range richAttrRe.FindAllStringSubmatch(raw, -1) {
		// A rich rule repeats a key inside its own element — `port port="80"`
		// — so the first occurrence of each is the one that means something
		// at the top level.
		if _, seen := attrs[m[1]]; !seen {
			attrs[m[1]] = m[2]
		}
	}
	if attrs["family"] == "ipv6" {
		r.IPv6 = true
	}
	r.From = "Anywhere"
	if addr := attrs["address"]; addr != "" {
		r.From = addr
	}
	r.Port = attrs["port"]
	r.Protocol = attrs["protocol"]
	switch {
	case r.Port != "" && r.Protocol != "":
		r.To = r.Port + "/" + r.Protocol
	case attrs["name"] != "":
		r.To = attrs["name"]
		r.Service = attrs["name"]
	default:
		r.To = "any"
	}
	switch {
	case strings.Contains(raw, " accept"):
		r.Action = "ALLOW"
		if strings.Contains(raw, "limit value=") {
			r.Action = "LIMIT"
		}
	case strings.Contains(raw, " reject"):
		r.Action = "REJECT"
	case strings.Contains(raw, " drop"):
		r.Action = "DENY"
	default:
		r.Action = "UNKNOWN"
	}
	return r, true
}

func (b firewalldBackend) AddRule(ctx context.Context, req RuleRequest) (string, error) {
	if req.Direction == "out" {
		// firewalld filters egress only through direct rules or policies,
		// neither of which is a zone rule. Saying so beats writing something
		// that looks like an outbound rule and filters nothing.
		return "", fmt.Errorf("firewalld zones do not filter outbound traffic; use a policy object instead")
	}
	zone := b.zone(ctx)
	// A plain allow with no source restriction is a port or a service, which
	// is the shape firewalld prefers and the shape it can list back cleanly.
	if req.Action == "allow" && req.From == "" && req.To == "" {
		var arg string
		switch {
		case req.App != "":
			arg = "--add-service=" + req.App
		case req.Protocol != "":
			arg = "--add-port=" + req.Port + "/" + req.Protocol
		default:
			// firewalld has no protocol-less port. tcp is the overwhelmingly
			// common intent and is what every example assumes.
			arg = "--add-port=" + req.Port + "/tcp"
		}
		if _, err := run(ctx, "firewall-cmd", "--permanent", "--zone="+zone, arg); err != nil {
			return "", err
		}
		return run(ctx, "firewall-cmd", "--reload")
	}

	rule, err := buildRichRule(req)
	if err != nil {
		return "", err
	}
	if _, err := run(ctx, "firewall-cmd", "--permanent", "--zone="+zone, "--add-rich-rule="+rule); err != nil {
		return "", err
	}
	return run(ctx, "firewall-cmd", "--reload")
}

// buildRichRule renders the one form firewalld has for "this source, this
// port, this verdict".
//
// The whole string is a single argv element, so the quotes inside it are
// firewalld's own syntax rather than shell quoting — nothing here is passed
// through a shell. Every value has already been validated by normaliseRule.
func buildRichRule(req RuleRequest) (string, error) {
	parts := []string{"rule"}
	if req.From != "" && !strings.EqualFold(req.From, "any") {
		family := "ipv4"
		if strings.Contains(req.From, ":") {
			family = "ipv6"
		}
		parts = append(parts, fmt.Sprintf("family=%q", family),
			fmt.Sprintf("source address=%q", req.From))
	}
	switch {
	case req.App != "":
		parts = append(parts, fmt.Sprintf("service name=%q", req.App))
	case req.Port != "":
		proto := req.Protocol
		if proto == "" {
			proto = "tcp"
		}
		parts = append(parts, fmt.Sprintf("port port=%q", req.Port),
			fmt.Sprintf("protocol=%q", proto))
	}
	switch req.Action {
	case "allow":
		parts = append(parts, "accept")
	case "limit":
		// firewalld's rate limit attaches to the verdict rather than being a
		// verdict of its own, which is why ufw's fourth action has no direct
		// equivalent and is expressed as a rate-limited accept.
		parts = append(parts, "accept", `limit value="10/m"`)
	case "deny":
		parts = append(parts, "drop")
	case "reject":
		parts = append(parts, "reject")
	default:
		return "", fmt.Errorf("unsupported action %q", req.Action)
	}
	return strings.Join(parts, " "), nil
}

// DeleteRule resolves the dashboard's positional number back to the handle
// firewalld needs, by listing again. The listing is the only source of these
// handles, so a stale number removes nothing rather than removing the wrong
// thing — the failure mode worth having.
func (b firewalldBackend) DeleteRule(ctx context.Context, number int) (string, error) {
	st, err := b.Status(ctx)
	if err != nil {
		return "", err
	}
	var handle string
	for i, r := range st.Rules {
		if i+1 == number {
			handle = r.Handle
			break
		}
	}
	if handle == "" {
		return "", fmt.Errorf("no rule %d in this zone", number)
	}
	kind, value, _ := strings.Cut(handle, ":")
	var arg string
	switch kind {
	case "service":
		arg = "--remove-service=" + value
	case "port":
		arg = "--remove-port=" + value
	case "rich":
		arg = "--remove-rich-rule=" + value
	default:
		return "", fmt.Errorf("this rule cannot be removed from the dashboard")
	}
	if _, err := run(ctx, "firewall-cmd", "--permanent", "--zone="+b.zone(ctx), arg); err != nil {
		return "", err
	}
	return run(ctx, "firewall-cmd", "--reload")
}

// SetEnabled drives the service rather than the firewall: firewalld has no
// on/off of its own, and stopping the daemon is what "off" means for it.
func (firewalldBackend) SetEnabled(ctx context.Context, enabled bool) (string, error) {
	verb := "stop"
	if enabled {
		verb = "start"
	}
	out, err := run(ctx, "systemctl", verb, "firewalld")
	if err != nil {
		return out, err
	}
	// Enabling the unit as well, so the answer survives a reboot. A firewall
	// that is on until the next restart is the trap this backend writes
	// everything --permanent to avoid.
	enableVerb := "disable"
	if enabled {
		enableVerb = "enable"
	}
	run(ctx, "systemctl", enableVerb, "firewalld")
	return out, nil
}

func (b firewalldBackend) SetDefaultPolicy(ctx context.Context, direction, policy string) (string, error) {
	if direction != "incoming" {
		return "", fmt.Errorf("firewalld zones set a policy for inbound traffic only")
	}
	target := "default"
	switch policy {
	case "allow":
		target = "ACCEPT"
	case "deny":
		target = "DROP"
	case "reject":
		target = "%%REJECT%%"
	}
	if _, err := run(ctx, "firewall-cmd", "--permanent", "--zone="+b.zone(ctx), "--set-target="+target); err != nil {
		return "", err
	}
	return run(ctx, "firewall-cmd", "--reload")
}

// SetLogging maps our five levels onto firewalld's three. It logs denials
// globally rather than per zone, so this is a machine-wide setting and the
// mapping is deliberately coarse.
func (firewalldBackend) SetLogging(ctx context.Context, level string) (string, error) {
	value := "all"
	switch level {
	case "off":
		value = "off"
	case "low", "on":
		value = "unicast"
	}
	return run(ctx, "firewall-cmd", "--set-log-denied="+value)
}

func (firewalldBackend) Reset(ctx context.Context) (string, error) {
	return "", fmt.Errorf("%w: firewalld has no reset command", ErrReadOnly)
}

// Profiles are firewalld's predefined services. It ships a few hundred, and
// unlike ufw's profiles they carry no ports in the listing — resolving each
// would be one subprocess per service, which is a second of latency to
// populate a dropdown. The names are what the operator picks by, so the names
// are what is returned.
func (firewalldBackend) Profiles(ctx context.Context) ([]AppProfile, error) {
	out, err := run(ctx, "firewall-cmd", "--get-services")
	if err != nil {
		return nil, err
	}
	profiles := []AppProfile{}
	for _, name := range strings.Fields(out) {
		if !appNameRe.MatchString(name) {
			continue
		}
		profiles = append(profiles, AppProfile{Name: name, Ports: []string{}})
	}
	sort.Slice(profiles, func(i, j int) bool { return profiles[i].Name < profiles[j].Name })
	return profiles, nil
}
