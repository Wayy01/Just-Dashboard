package dockerx

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// This file turns a ContainerSpec back into the two texts an operator already
// knows how to read: a `docker run` line and a compose service.
//
// Both exist for the same reason. A form that quietly assembles an API call is
// a black box — you cannot check it, paste it into a ticket, or learn anything
// from it — and a dashboard that hides what it did is exactly the reason
// experienced operators refuse to use one. Showing the command is also the
// cheapest documentation there is: someone who has never run a container reads
// their own choices back as the line they would have had to type, and the next
// time they see that line somewhere else they know what it says.
//
// The compose rendering is the more useful half. A container created by hand
// exists only in the daemon's memory; the same container as four lines of YAML
// in a file is something you can commit, diff, back up and redeploy. "Save
// this as a stack" is therefore a first-class action rather than an export
// buried in a menu.

// RunCommand renders the spec as the `docker run` invocation that would
// produce it. Long options are used throughout: `-v` is shorter, `--volume`
// is legible to someone who has not memorised the short forms.
func (s ContainerSpec) RunCommand() string {
	parts := []string{"docker", "run", "--detach"}
	add := func(f ...string) { parts = append(parts, f...) }

	if s.Name != "" {
		add("--name", shellQuote(s.Name))
	}
	if s.RestartPolicy != "" && s.RestartPolicy != "no" {
		policy := s.RestartPolicy
		if policy == "on-failure" && s.MaxRetries > 0 {
			policy = fmt.Sprintf("on-failure:%d", s.MaxRetries)
		}
		add("--restart", policy)
	}
	// Rendered so the command on screen is the container that would exist. A
	// spec field the preview omits is one an operator copying the line out of
	// this dashboard loses without being told.
	if s.Logging.Driver != "" {
		add("--log-driver", shellQuote(s.Logging.Driver))
	}
	for _, k := range sortedKeys(s.Logging.Options) {
		add("--log-opt", shellQuote(k+"="+s.Logging.Options[k]))
	}
	for _, p := range s.Ports {
		if p.HostPort == 0 {
			add("--expose", portSpec(p.ContainerPort, p.Protocol))
			continue
		}
		add("--publish", shellQuote(publishSpec(p)))
	}
	for _, m := range s.Mounts {
		add("--mount", shellQuote(mountFlag(m)))
	}
	for _, e := range s.Env {
		add("--env", shellQuote(e.Name+"="+e.Value))
	}
	for _, l := range s.Labels {
		add("--label", shellQuote(l.Name+"="+l.Value))
	}
	for _, n := range s.Networks {
		add("--network", shellQuote(n))
	}
	if s.NetworkMode != "" {
		add("--network", shellQuote(s.NetworkMode))
	}
	if s.Hostname != "" {
		add("--hostname", shellQuote(s.Hostname))
	}
	for _, h := range s.ExtraHosts {
		add("--add-host", shellQuote(h))
	}
	for _, d := range s.DNS {
		add("--dns", d)
	}
	if s.User != "" {
		add("--user", shellQuote(s.User))
	}
	if s.WorkingDir != "" {
		add("--workdir", shellQuote(s.WorkingDir))
	}
	if s.Limits.MemoryMB > 0 {
		add("--memory", fmt.Sprintf("%dm", s.Limits.MemoryMB))
	}
	if s.Limits.CPUs > 0 {
		add("--cpus", trimFloat(s.Limits.CPUs))
	}
	if s.Limits.PidsLimit > 0 {
		add("--pids-limit", strconv.FormatInt(s.Limits.PidsLimit, 10))
	}
	if s.Limits.ShmSizeMB > 0 {
		add("--shm-size", fmt.Sprintf("%dm", s.Limits.ShmSizeMB))
	}
	for _, d := range s.Devices {
		add("--device", shellQuote(deviceFlag(d)))
	}
	for _, c := range s.CapAdd {
		add("--cap-add", c)
	}
	for _, c := range s.CapDrop {
		add("--cap-drop", c)
	}
	if s.Privileged {
		add("--privileged")
	}
	if s.Init {
		add("--init")
	}
	if s.ReadOnly {
		add("--read-only")
	}
	if s.AutoRemove {
		add("--rm")
	}
	if s.TTY {
		add("--tty")
	}
	if s.OpenStdin {
		add("--interactive")
	}
	if s.StopSignal != "" {
		add("--stop-signal", s.StopSignal)
	}
	if len(s.Entrypoint) > 0 {
		add("--entrypoint", shellQuote(strings.Join(s.Entrypoint, " ")))
	}
	if s.Health != nil && s.Health.Disable {
		add("--no-healthcheck")
	} else if s.Health != nil && len(s.Health.Test) > 0 {
		add("--health-cmd", shellQuote(healthCommand(s.Health.Test)))
		if s.Health.IntervalSec > 0 {
			add("--health-interval", fmt.Sprintf("%ds", s.Health.IntervalSec))
		}
		if s.Health.Retries > 0 {
			add("--health-retries", strconv.Itoa(s.Health.Retries))
		}
	}

	// The image is passed separately rather than appended to `parts`: it is a
	// positional argument, and wrapCommand keeps a flag's value on the flag's
	// line — which put the image on the end of `--memory 256m` and made the
	// most important token in the command the easiest one to miss.
	image := []string{shellQuote(s.Image)}
	for _, c := range s.Command {
		image = append(image, shellQuote(c))
	}
	return wrapCommand(parts) + " \\\n  " + strings.Join(image, " ")
}

// wrapCommand breaks the line at each flag so the result is readable rather
// than a 400-character single line that a terminal soft-wraps mid-token.
func wrapCommand(parts []string) string {
	var b strings.Builder
	line := ""
	flush := func() {
		if line != "" {
			b.WriteString(line)
			line = ""
		}
	}
	for i := 0; i < len(parts); i++ {
		token := parts[i]
		if strings.HasPrefix(token, "--") && i > 0 {
			flush()
			b.WriteString(" \\\n  ")
			line = token
			// A flag with a value keeps it on the same line.
			if i+1 < len(parts) && !strings.HasPrefix(parts[i+1], "--") {
				line += " " + parts[i+1]
				i++
			}
			continue
		}
		if line == "" {
			line = token
		} else {
			line += " " + token
		}
	}
	flush()
	return b.String()
}

func publishSpec(p PortMapping) string {
	spec := strconv.Itoa(p.HostPort) + ":" + strconv.Itoa(p.ContainerPort)
	if p.HostIP != "" {
		spec = p.HostIP + ":" + spec
	}
	if strings.EqualFold(p.Protocol, "udp") {
		spec += "/udp"
	}
	return spec
}

func portSpec(port int, proto string) string {
	if strings.EqualFold(proto, "udp") {
		return strconv.Itoa(port) + "/udp"
	}
	return strconv.Itoa(port)
}

func mountFlag(m MountSpec) string {
	fields := []string{"type=" + mountType(m.Type)}
	if m.Source != "" {
		fields = append(fields, "source="+m.Source)
	}
	fields = append(fields, "target="+m.Target)
	if m.ReadOnly {
		fields = append(fields, "readonly")
	}
	if m.Type == "tmpfs" && m.SizeMB > 0 {
		fields = append(fields, fmt.Sprintf("tmpfs-size=%dm", m.SizeMB))
	}
	return strings.Join(fields, ",")
}

func mountType(t string) string {
	if t == "" {
		return "volume"
	}
	return t
}

func deviceFlag(d DeviceSpec) string {
	spec := d.Host
	if d.Container != "" && d.Container != d.Host {
		spec += ":" + d.Container
	}
	if d.Permissions != "" && d.Permissions != "rwm" {
		spec += ":" + d.Permissions
	}
	return spec
}

func healthCommand(test []string) string {
	if len(test) == 0 {
		return ""
	}
	switch test[0] {
	case "CMD-SHELL":
		return strings.Join(test[1:], " ")
	case "CMD":
		return strings.Join(test[1:], " ")
	default:
		return strings.Join(test, " ")
	}
}

// shellQuote quotes a token for a shell only when it needs it, so the common
// case reads as the plain word it is.
func shellQuote(s string) string {
	if s == "" {
		return `""`
	}
	safe := true
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case strings.ContainsRune("_-./:=@+,", r):
		default:
			safe = false
		}
		if !safe {
			break
		}
	}
	if safe {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func trimFloat(f float64) string {
	return strings.TrimRight(strings.TrimRight(strconv.FormatFloat(f, 'f', 3, 64), "0"), ".")
}

// ComposeService renders the spec as one service in a compose file, together
// with the top-level `volumes:` and `networks:` blocks it needs.
//
// Hand-written rather than marshalled through a YAML library: the output is
// meant to be read and edited by a person, so key order carries meaning (image
// first, then what it is called, then what it talks to) and a marshaller would
// sort it alphabetically into something correct and unreadable. It also keeps
// the dependency list where it is.
func (s ContainerSpec) ComposeService(serviceName string) string {
	if serviceName == "" {
		serviceName = s.Name
	}
	if serviceName == "" {
		serviceName = "app"
	}
	var b strings.Builder
	w := func(indent int, format string, args ...any) {
		b.WriteString(strings.Repeat(" ", indent))
		fmt.Fprintf(&b, format, args...)
		b.WriteString("\n")
	}

	w(0, "services:")
	w(2, "%s:", serviceName)
	w(4, "image: %s", s.Image)
	if s.Name != "" && s.Name != serviceName {
		w(4, "container_name: %s", s.Name)
	}
	if s.RestartPolicy != "" && s.RestartPolicy != "no" {
		w(4, "restart: %s", s.RestartPolicy)
	}
	if s.Logging.Driver != "" || len(s.Logging.Options) > 0 {
		w(4, "logging:")
		if s.Logging.Driver != "" {
			w(6, "driver: %s", s.Logging.Driver)
		}
		if len(s.Logging.Options) > 0 {
			w(6, "options:")
			for _, k := range sortedKeys(s.Logging.Options) {
				// Quoted: compose reads an unquoted 10m as a string anyway,
				// but max-file: 3 becomes an int and the daemon wants a
				// string, which fails at `up` rather than here.
				w(8, "%s: %q", k, s.Logging.Options[k])
			}
		}
	}
	if len(s.Command) > 0 {
		w(4, "command: %s", yamlList(s.Command))
	}
	if len(s.Entrypoint) > 0 {
		w(4, "entrypoint: %s", yamlList(s.Entrypoint))
	}
	if s.User != "" {
		w(4, "user: %q", s.User)
	}
	if s.WorkingDir != "" {
		w(4, "working_dir: %s", s.WorkingDir)
	}
	if s.Hostname != "" {
		w(4, "hostname: %s", s.Hostname)
	}
	if len(s.Ports) > 0 {
		published := false
		for _, p := range s.Ports {
			if p.HostPort != 0 {
				published = true
			}
		}
		if published {
			w(4, "ports:")
			for _, p := range s.Ports {
				if p.HostPort == 0 {
					continue
				}
				w(6, "- %q", publishSpec(p))
			}
		}
		exposed := []string{}
		for _, p := range s.Ports {
			if p.HostPort == 0 {
				exposed = append(exposed, portSpec(p.ContainerPort, p.Protocol))
			}
		}
		if len(exposed) > 0 {
			w(4, "expose:")
			for _, e := range exposed {
				w(6, "- %q", e)
			}
		}
	}
	if len(s.Env) > 0 {
		w(4, "environment:")
		for _, e := range s.Env {
			w(6, "%s: %s", e.Name, yamlScalar(e.Value))
		}
	}
	named := map[string]bool{}
	if len(s.Mounts) > 0 {
		volumes := []string{}
		tmpfs := []string{}
		for _, m := range s.Mounts {
			switch mountType(m.Type) {
			case "tmpfs":
				tmpfs = append(tmpfs, m.Target)
			default:
				line := m.Source + ":" + m.Target
				if m.Source == "" {
					line = m.Target
				}
				if m.ReadOnly {
					line += ":ro"
				}
				volumes = append(volumes, line)
				if mountType(m.Type) == "volume" && m.Source != "" {
					named[m.Source] = true
				}
			}
		}
		if len(volumes) > 0 {
			w(4, "volumes:")
			for _, v := range volumes {
				w(6, "- %q", v)
			}
		}
		if len(tmpfs) > 0 {
			w(4, "tmpfs:")
			for _, t := range tmpfs {
				w(6, "- %s", t)
			}
		}
	}
	if len(s.Networks) > 0 {
		w(4, "networks:")
		for _, n := range s.Networks {
			w(6, "- %s", n)
		}
	} else if s.NetworkMode != "" {
		w(4, "network_mode: %s", s.NetworkMode)
	}
	if len(s.ExtraHosts) > 0 {
		w(4, "extra_hosts:")
		for _, h := range s.ExtraHosts {
			w(6, "- %q", h)
		}
	}
	if len(s.Labels) > 0 {
		w(4, "labels:")
		for _, l := range s.Labels {
			w(6, "%s: %s", l.Name, yamlScalar(l.Value))
		}
	}
	if len(s.CapAdd) > 0 {
		w(4, "cap_add:")
		for _, c := range s.CapAdd {
			w(6, "- %s", c)
		}
	}
	if len(s.CapDrop) > 0 {
		w(4, "cap_drop:")
		for _, c := range s.CapDrop {
			w(6, "- %s", c)
		}
	}
	if s.Privileged {
		w(4, "privileged: true")
	}
	if s.Init {
		w(4, "init: true")
	}
	if s.ReadOnly {
		w(4, "read_only: true")
	}
	if s.TTY {
		w(4, "tty: true")
	}
	if s.OpenStdin {
		w(4, "stdin_open: true")
	}
	if s.Health != nil && (s.Health.Disable || len(s.Health.Test) > 0) {
		w(4, "healthcheck:")
		if s.Health.Disable {
			w(6, "disable: true")
		} else {
			w(6, "test: %s", yamlList(s.Health.Test))
			if s.Health.IntervalSec > 0 {
				w(6, "interval: %ds", s.Health.IntervalSec)
			}
			if s.Health.TimeoutSec > 0 {
				w(6, "timeout: %ds", s.Health.TimeoutSec)
			}
			if s.Health.Retries > 0 {
				w(6, "retries: %d", s.Health.Retries)
			}
			if s.Health.StartPeriodSec > 0 {
				w(6, "start_period: %ds", s.Health.StartPeriodSec)
			}
		}
	}
	// Compose expresses limits under deploy.resources on the v2 schema, which
	// the compose plugin honours on a plain Docker host too.
	if s.Limits.MemoryMB > 0 || s.Limits.CPUs > 0 {
		w(4, "deploy:")
		w(6, "resources:")
		w(8, "limits:")
		if s.Limits.CPUs > 0 {
			w(10, "cpus: %q", trimFloat(s.Limits.CPUs))
		}
		if s.Limits.MemoryMB > 0 {
			w(10, "memory: %dM", s.Limits.MemoryMB)
		}
	}

	if len(named) > 0 {
		names := make([]string, 0, len(named))
		for n := range named {
			names = append(names, n)
		}
		sort.Strings(names)
		b.WriteString("\nvolumes:\n")
		// `external: true` because these are named volumes that already exist,
		// which is the case when this file was rendered from a running
		// container. Without it compose creates a *second* volume with the
		// project name prefixed and the service comes up with none of its
		// data — a silent failure that looks like the application lost
		// everything. With it, a volume that does not exist yet is a loud
		// error at deploy time saying exactly that, which is the better half
		// of the trade. The comment is in the emitted file because that is
		// where somebody hits it.
		b.WriteString("  # These already exist on this server. Drop `external: true`\n")
		b.WriteString("  # from any volume you want compose to create for you.\n")
		for _, n := range names {
			w(2, "%s:", n)
			w(4, "external: true")
		}
	}
	if len(s.Networks) > 0 {
		b.WriteString("\nnetworks:\n")
		for _, n := range s.Networks {
			w(2, "%s:", n)
			w(4, "external: true")
		}
	}
	return b.String()
}

func yamlList(items []string) string {
	quoted := make([]string, 0, len(items))
	for _, i := range items {
		quoted = append(quoted, strconv.Quote(i))
	}
	return "[" + strings.Join(quoted, ", ") + "]"
}

// yamlScalar quotes a value unless it is unambiguously a plain string. YAML's
// implicit typing is the classic source of a config that reads "no" as false
// and a version number as a float.
func yamlScalar(v string) string {
	if v == "" {
		return `""`
	}
	lower := strings.ToLower(v)
	switch lower {
	case "y", "n", "yes", "no", "true", "false", "on", "off", "null", "~":
		return strconv.Quote(v)
	}
	if strings.ContainsAny(v, ":#{}[],&*?|<>=!%@`\"'\\\n\t") || strings.TrimSpace(v) != v {
		return strconv.Quote(v)
	}
	if _, err := strconv.ParseFloat(v, 64); err == nil {
		return strconv.Quote(v)
	}
	return v
}

// ShortID is the twelve-character form of an id, which is what Docker prints
// and what fits in a table cell. The `sha256:` prefix an image id carries is
// dropped: it is the same for every image and tells the reader nothing.
func ShortID(id string) string {
	id = strings.TrimPrefix(id, "sha256:")
	if len(id) > 12 {
		return id[:12]
	}
	return id
}

// sortedKeys keeps rendered output stable. Go randomises map iteration, and a
// preview that reorders its own lines between two identical requests reads as
// the server having changed its mind.
func sortedKeys(m map[string]string) []string {
	if len(m) == 0 {
		return nil
	}
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
