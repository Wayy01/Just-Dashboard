package logsx

import (
	"bufio"
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type SourceKind string

const (
	KindSystem  SourceKind = "system"
	KindNginx   SourceKind = "nginx"
	KindApp     SourceKind = "app"
	KindPM2     SourceKind = "pm2"
	KindDocker  SourceKind = "docker"
	KindJournal SourceKind = "journal"
)

// Source is one thing the unified viewer can open. Docker containers and the
// journal are addressed by ID rather than by path, since neither is a file
// this process reads directly.
type Source struct {
	ID       string     `json:"id"`
	Label    string     `json:"label"`
	Kind     SourceKind `json:"kind"`
	Path     string     `json:"path,omitempty"`
	Size     int64      `json:"size,omitempty"`
	Modified *time.Time `json:"modified,omitempty"`
	Rotated  bool       `json:"rotated"`
}

// wellKnown are the files an operator expects to find without hunting. Any
// that do not exist on this host are simply skipped.
var wellKnown = []struct {
	path  string
	label string
	kind  SourceKind
}{
	{"/var/log/syslog", "syslog", KindSystem},
	{"/var/log/messages", "messages", KindSystem},
	{"/var/log/auth.log", "auth", KindSystem},
	{"/var/log/secure", "secure", KindSystem},
	{"/var/log/kern.log", "kernel", KindSystem},
	{"/var/log/dpkg.log", "dpkg", KindSystem},
	{"/var/log/ufw.log", "ufw", KindSystem},
	{"/var/log/fail2ban.log", "fail2ban", KindSystem},
	{"/var/log/nginx/access.log", "nginx access", KindNginx},
	{"/var/log/nginx/error.log", "nginx error", KindNginx},
	{"/var/log/caddy/access.log", "caddy access", KindNginx},
}

// Discover enumerates readable log files. It walks the configured roots so
// application logs dropped into /var/log/myapp are found without configuration.
func (s *Service) Discover(ctx context.Context) ([]Source, error) {
	seen := map[string]bool{}
	out := []Source{}

	add := func(path, label string, kind SourceKind) {
		if seen[path] {
			return
		}
		st, err := os.Stat(path)
		if err != nil || st.IsDir() {
			return
		}
		seen[path] = true
		mod := st.ModTime().UTC()
		out = append(out, Source{
			ID: path, Label: label, Kind: kind, Path: path,
			Size: st.Size(), Modified: &mod,
		})
	}

	for _, wk := range wellKnown {
		add(wk.path, wk.label, wk.kind)
	}
	for _, root := range s.roots {
		filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil || ctx.Err() != nil {
				return nil
			}
			if d.IsDir() {
				if strings.Count(strings.TrimPrefix(path, root), string(os.PathSeparator)) > 3 {
					return filepath.SkipDir
				}
				return nil
			}
			name := d.Name()
			// Rotated and compressed archives are noise in a live viewer;
			// they are reachable through the file manager if needed.
			if !strings.HasSuffix(name, ".log") && !strings.HasSuffix(name, ".err") && !strings.HasSuffix(name, ".out") {
				return nil
			}
			if strings.HasSuffix(name, ".gz") || strings.HasSuffix(name, ".xz") {
				return nil
			}
			kind := KindApp
			if strings.Contains(path, "nginx") || strings.Contains(path, "caddy") {
				kind = KindNginx
			}
			add(path, strings.TrimPrefix(path, root+string(os.PathSeparator)), kind)
			return nil
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		return out[i].Label < out[j].Label
	})
	return out, nil
}

// RotateRule is one logrotate stanza, paired with what is currently on disk so
// the UI can answer "is rotation actually happening for this file".
type RotateRule struct {
	ConfigFile string   `json:"configFile"`
	Patterns   []string `json:"patterns"`
	Frequency  string   `json:"frequency,omitempty"`
	Rotate     string   `json:"rotate,omitempty"`
	Compress   bool     `json:"compress"`
	Size       string   `json:"size,omitempty"`
	MaxSize    string   `json:"maxSize,omitempty"`
	Missing    bool     `json:"missingOk"`
	Options    []string `json:"options"`
}

type RotateStatus struct {
	Available bool         `json:"available"`
	Rules     []RotateRule `json:"rules"`
	StateFile string       `json:"stateFile,omitempty"`
	LastRun   *time.Time   `json:"lastRun,omitempty"`
}

// LogrotateStatus parses /etc/logrotate.conf and its drop-ins. The parser is
// deliberately shallow — enough to report frequency, retention and compression
// per pattern, which is what an operator checks — rather than a full
// reimplementation of logrotate's grammar.
func LogrotateStatus(ctx context.Context) (*RotateStatus, error) {
	st := &RotateStatus{Rules: []RotateRule{}}
	main := "/etc/logrotate.conf"
	if _, err := os.Stat(main); err != nil {
		return st, nil
	}
	st.Available = true

	files := []string{main}
	if entries, err := os.ReadDir("/etc/logrotate.d"); err == nil {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			if !e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
				names = append(names, filepath.Join("/etc/logrotate.d", e.Name()))
			}
		}
		files = append(files, sortStrings(names)...)
	}
	for _, f := range files {
		if ctx.Err() != nil {
			break
		}
		st.Rules = append(st.Rules, parseLogrotateFile(f)...)
	}
	for _, state := range []string{"/var/lib/logrotate/status", "/var/lib/logrotate.status"} {
		if fi, err := os.Stat(state); err == nil {
			st.StateFile = state
			mod := fi.ModTime().UTC()
			st.LastRun = &mod
			break
		}
	}
	return st, nil
}

func parseLogrotateFile(path string) []RotateRule {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	rules := []RotateRule{}
	var current *RotateRule
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasSuffix(line, "{") {
			patterns := strings.Fields(strings.TrimSpace(strings.TrimSuffix(line, "{")))
			current = &RotateRule{ConfigFile: path, Patterns: patterns, Options: []string{}}
			continue
		}
		if line == "}" {
			if current != nil {
				rules = append(rules, *current)
				current = nil
			}
			continue
		}
		if current == nil {
			continue
		}
		fields := strings.Fields(line)
		switch fields[0] {
		case "daily", "weekly", "monthly", "yearly", "hourly":
			current.Frequency = fields[0]
		case "rotate":
			if len(fields) > 1 {
				current.Rotate = fields[1]
			}
		case "compress":
			current.Compress = true
		case "size", "minsize":
			if len(fields) > 1 {
				current.Size = fields[1]
			}
		case "maxsize":
			if len(fields) > 1 {
				current.MaxSize = fields[1]
			}
		case "missingok":
			current.Missing = true
		default:
			current.Options = append(current.Options, line)
		}
	}
	return rules
}
