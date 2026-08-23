package netsec

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// Tuning a jail through fail2ban-client changes the running server and nothing
// else, so the setting is gone at the next restart.
//
// That is exactly the trap this codebase refuses elsewhere: a control whose
// effect quietly disappears is worse than no control, because the page goes on
// reporting the value somebody chose. The runtime call still happens first —
// it is what makes the change take effect now — and the same values are then
// written into a drop-in so they survive.
//
// fail2ban reads jail.conf, then jail.d/*.conf, then jail.local, then
// jail.d/*.local, and the last one wins. A .local file in jail.d is therefore
// the only place a write is guaranteed to beat whatever the distribution
// shipped, which is why the extension matters as much as the name.
const jailOverrideFile = "99-just-dashboard.local"

func jailOverridePath() string {
	for _, base := range []string{"/etc/fail2ban", "/host/etc/fail2ban"} {
		if st, err := os.Stat(base); err == nil && st.IsDir() {
			return filepath.Join(base, "jail.d", jailOverrideFile)
		}
	}
	return ""
}

// JailParamResult separates the two halves, because they fail independently:
// the running server can accept a value that cannot be written to disk, and a
// stopped fail2ban can be configured for its next start.
type JailParamResult struct {
	Applied   bool   `json:"applied"`
	Persisted bool   `json:"persisted"`
	File      string `json:"file,omitempty"`
	Output    string `json:"output,omitempty"`
	Warning   string `json:"warning,omitempty"`
}

// SetJailParams changes one or more of a jail's parameters, now and for good.
func (s *Service) SetJailParams(ctx context.Context, jail string, params map[string]int) (*JailParamResult, error) {
	if !jailNameRe.MatchString(jail) {
		return nil, fmt.Errorf("invalid jail name %q", jail)
	}
	if len(params) == 0 {
		return nil, fmt.Errorf("no parameters given")
	}
	clean := map[string]int{}
	for param, value := range params {
		param = strings.ToLower(strings.TrimSpace(param))
		bounds, ok := jailParams[param]
		if !ok {
			return nil, fmt.Errorf("%q is not a parameter this dashboard sets", param)
		}
		if value < bounds.min || value > bounds.max {
			return nil, fmt.Errorf("%s must be between %d and %d", param, bounds.min, bounds.max)
		}
		clean[param] = value
	}

	res := &JailParamResult{}
	var outputs []string
	for _, param := range sortedParams(clean) {
		out, err := run(ctx, "fail2ban-client", "set", jail, param, strconv.Itoa(clean[param]))
		if err != nil {
			// A stopped fail2ban refuses the runtime call, which is not a
			// reason to skip the persistent half — the value is exactly what
			// it should start with next time.
			res.Warning = "The running server did not accept the change: " + err.Error()
			break
		}
		outputs = append(outputs, strings.TrimSpace(out))
		res.Applied = true
	}
	res.Output = strings.Join(outputs, "\n")

	path := jailOverridePath()
	if path == "" {
		res.Warning = "No /etc/fail2ban directory, so the change applies to the running server only and will be lost on restart."
		return res, nil
	}
	existing, _ := os.ReadFile(path)
	merged := mergeJailOverrides(string(existing), jail, clean)
	if err := writeJailOverride(path, merged); err != nil {
		res.Warning = "Applied to the running server, but could not be written to disk: " + err.Error()
		return res, nil
	}
	res.Persisted, res.File = true, path
	return res, nil
}

func sortedParams(params map[string]int) []string {
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// mergeJailOverrides rewrites one jail's section, leaving every other section
// exactly as it was.
//
// It is a merge rather than a rewrite because this file holds every jail the
// operator has tuned, and regenerating it from the one jail being edited would
// silently drop the rest. Unknown keys inside a section this dashboard owns
// are kept too: somebody may have added a line by hand, and eating it would be
// the same mistake in miniature.
func mergeJailOverrides(existing, jail string, params map[string]int) string {
	header := "[" + jail + "]"
	var out []string
	wrote := false
	inSection := false
	written := map[string]bool{}

	flushSection := func() {
		for _, param := range sortedParams(params) {
			if !written[param] {
				out = append(out, param+" = "+strconv.Itoa(params[param]))
				written[param] = true
			}
		}
	}

	sc := bufio.NewScanner(strings.NewReader(existing))
	for sc.Scan() {
		line := sc.Text()
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			if inSection {
				flushSection()
				inSection = false
			}
			if trimmed == header {
				inSection, wrote = true, true
			}
			out = append(out, line)
			continue
		}
		if inSection {
			key, _, ok := strings.Cut(trimmed, "=")
			key = strings.ToLower(strings.TrimSpace(key))
			if ok {
				if value, managed := params[key]; managed {
					out = append(out, key+" = "+strconv.Itoa(value))
					written[key] = true
					continue
				}
			}
		}
		out = append(out, line)
	}
	if inSection {
		flushSection()
	}
	if !wrote {
		if len(out) > 0 && strings.TrimSpace(out[len(out)-1]) != "" {
			out = append(out, "")
		}
		out = append(out, header)
		for _, param := range sortedParams(params) {
			out = append(out, param+" = "+strconv.Itoa(params[param]))
		}
	}

	body := strings.Join(out, "\n")
	if !strings.Contains(body, "# Written by Just Dashboard") {
		body = "# Written by Just Dashboard.\n" +
			"# fail2ban reads jail.d/*.local last, so these override whatever the\n" +
			"# distribution's own jail.conf and jail.d/*.conf set.\n" + body
	}
	return strings.TrimRight(body, "\n") + "\n"
}

func writeJailOverride(path, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".jd-jail-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.WriteString(content); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
