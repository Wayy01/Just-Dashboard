package netsec

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Wayy01/Just-Dashboard/backend/internal/hostexec"
)

// Applying an SSH change from a web page is the most dangerous thing this
// dashboard does, because getting it wrong removes the way back in.
//
// So the order is the same one the proxy editor uses, for the same reason:
// refuse the changes that are certainly a lockout, write, validate with the
// daemon's own parser, restore on failure, and only then reload. sshd's `-t`
// is the equivalent of `nginx -t` and it is the difference between a typo
// costing a reload and a typo costing the machine.

var ErrSSHInvalid = fmt.Errorf("sshd rejected the configuration")

// SSHApplyResult reports what happened, in the order it happened, so a
// partial success reads as one.
type SSHApplyResult struct {
	Written  bool   `json:"written"`
	File     string `json:"file"`
	Valid    bool   `json:"valid"`
	Output   string `json:"output,omitempty"`
	Reloaded bool   `json:"reloaded"`
	// ReloadError is set when the file is good but the daemon would not pick
	// it up. That is a real state and pretending otherwise would leave the
	// operator believing a setting is in force when it is not.
	ReloadError string   `json:"reloadError,omitempty"`
	Applied     []string `json:"applied"`
}

var sshValueRe = regexp.MustCompile(`^[A-Za-z0-9._:/@,\-]{1,64}$`)

// ApplySSHSettings writes a closed set of directives and reloads sshd.
func (s *Service) ApplySSHSettings(ctx context.Context, changes map[string]string) (*SSHApplyResult, error) {
	if len(changes) == 0 {
		return nil, fmt.Errorf("no settings given")
	}
	clean := map[string]string{}
	for key, value := range changes {
		def, ok := sshDirectiveFor(key)
		if !ok {
			return nil, fmt.Errorf("%q is not a setting this dashboard manages", key)
		}
		value = strings.ToLower(strings.TrimSpace(value))
		if !sshValueRe.MatchString(value) {
			return nil, fmt.Errorf("invalid value for %s", def.Label)
		}
		if err := validateSSHValue(def, value); err != nil {
			return nil, err
		}
		clean[def.Key] = value
	}

	current := s.SSHDStatus(ctx)
	if !current.Available {
		return nil, fmt.Errorf("no SSH server configuration found on this host")
	}
	if err := guardSSHLockout(current, clean); err != nil {
		return nil, err
	}

	target := current.ManagedFile
	original, existed := readFileOrEmpty(target)
	updated := applyDirectives(original, clean, filepath.Base(target) == managedDropIn)
	if err := writeSSHFile(target, updated); err != nil {
		return nil, err
	}

	res := &SSHApplyResult{Written: true, File: target, Applied: sortedKeys(clean)}
	valid, output := sshdTest(ctx)
	res.Valid, res.Output = valid, output
	if !valid {
		restoreSSHFile(target, original, existed)
		return res, ErrSSHInvalid
	}
	if err := reloadSSH(ctx); err != nil {
		res.ReloadError = err.Error()
		return res, nil
	}
	res.Reloaded = true
	return res, nil
}

func sshDirectiveFor(key string) (sshDirective, bool) {
	key = strings.ToLower(strings.TrimSpace(key))
	for _, d := range sshDirectives {
		if d.Key == key {
			return d, true
		}
	}
	return sshDirective{}, false
}

// validateSSHValue keeps a directive to the values its own control offers. A
// number is bounded generously — the recommendation is stricter than what is
// allowed, because refusing a legal setting because we would not have chosen
// it is not this endpoint's job.
func validateSSHValue(def sshDirective, value string) error {
	if def.Kind == "number" {
		n, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("%s must be a number", def.Label)
		}
		if n < 0 || n > 86400 {
			return fmt.Errorf("%s is out of range", def.Label)
		}
		return nil
	}
	for _, opt := range def.Options {
		if value == opt {
			return nil
		}
	}
	return fmt.Errorf("%s must be one of %s", def.Label, strings.Join(def.Options, ", "))
}

// guardSSHLockout refuses the changes that leave nobody a way in.
//
// The same reasoning as the firewall's guard, and the same narrowness: only
// the certain cases are refused. Disabling passwords on a host where somebody
// has an authorized key is the correct thing to do and must not be blocked;
// doing it on a host where nobody has one is the single commonest way people
// lose a server they were in the middle of securing.
func guardSSHLockout(current *SSHDConfig, changes map[string]string) error {
	effective := func(key string) string {
		if v, ok := changes[key]; ok {
			return v
		}
		for _, s := range current.Settings {
			if s.Key == key {
				return strings.ToLower(s.Value)
			}
		}
		return ""
	}
	password := effective("passwordauthentication")
	pubkey := effective("pubkeyauthentication")

	if password == "no" && pubkey == "no" {
		return fmt.Errorf("%w: with both passwords and keys refused, nothing could authenticate", ErrLockout)
	}
	if password == "no" && len(current.KeyedAccounts) == 0 {
		return fmt.Errorf("%w: no account on this host has an authorized SSH key, so turning off password authentication would leave no way to log in", ErrLockout)
	}
	if effective("permitrootlogin") == "no" && onlyRootIsKeyed(current.KeyedAccounts) && password == "no" {
		return fmt.Errorf("%w: root is the only account with an authorized key, and passwords are off", ErrLockout)
	}
	return nil
}

func onlyRootIsKeyed(accounts []KeyedAccount) bool {
	if len(accounts) != 1 {
		return false
	}
	return accounts[0].User == "root"
}

// applyDirectives rewrites configuration text so each directive is set once.
//
// The first value wins in sshd, so a directive already present is replaced
// where it stands rather than appended — appending would write a line the
// daemon reads and discards, which looks identical to a setting that took
// effect. Later duplicates are commented out with a marker rather than
// deleted, so an operator reading the file afterwards can see what was there.
func applyDirectives(content string, changes map[string]string, header bool) string {
	seen := map[string]bool{}
	var out []string
	if content != "" {
		sc := bufio.NewScanner(strings.NewReader(content))
		sc.Buffer(make([]byte, 0, 8192), 1<<20)
		for sc.Scan() {
			line := sc.Text()
			trimmed := strings.TrimSpace(line)
			if trimmed == "" || strings.HasPrefix(trimmed, "#") {
				out = append(out, line)
				continue
			}
			key, _, _ := strings.Cut(trimmed, " ")
			key = strings.ToLower(key)
			value, managed := changes[key]
			if !managed {
				out = append(out, line)
				continue
			}
			if seen[key] {
				out = append(out, "# "+line+"  # superseded, set above by Just Dashboard")
				continue
			}
			seen[key] = true
			out = append(out, canonicalDirective(key)+" "+value)
		}
	}
	missing := []string{}
	for key := range changes {
		if !seen[key] {
			missing = append(missing, key)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		if len(out) > 0 && strings.TrimSpace(out[len(out)-1]) != "" {
			out = append(out, "")
		}
		if header && content == "" {
			out = append(out,
				"# Written by Just Dashboard. Included from sshd_config, which reads the",
				"# first value it finds for a keyword — so these override the settings",
				"# further down in sshd_config rather than being overridden by them.")
		} else {
			out = append(out, "# Set by Just Dashboard")
		}
		for _, key := range missing {
			out = append(out, canonicalDirective(key)+" "+changes[key])
		}
	}
	return strings.Join(out, "\n") + "\n"
}

// canonicalDirective restores sshd's own capitalisation. sshd does not care,
// and a file where half the lines are lowercase reads as damaged.
func canonicalDirective(key string) string {
	for _, name := range []string{
		"PermitRootLogin", "PasswordAuthentication", "PubkeyAuthentication",
		"PermitEmptyPasswords", "X11Forwarding", "MaxAuthTries", "LoginGraceTime",
		"ClientAliveInterval", "ClientAliveCountMax", "Port",
	} {
		if strings.EqualFold(name, key) {
			return name
		}
	}
	return key
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func readFileOrEmpty(path string) (string, bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	return string(b), true
}

// writeSSHFile writes through a temporary file in the same directory, keeping
// the previous content as .bak.
//
// An existing file keeps the mode it had: sshd_config is 0644 on most
// distributions and quietly tightening a system file to 0600 is a change
// nobody asked for. A file being created is 0600, which is the safe default
// for something sshd refuses to start behind if it is group-writable.
func writeSSHFile(path, content string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	mode := os.FileMode(0o600)
	if st, err := os.Stat(path); err == nil {
		mode = st.Mode().Perm()
	}
	if prev, err := os.ReadFile(path); err == nil {
		os.WriteFile(path+".bak", prev, mode)
	}
	tmp, err := os.CreateTemp(dir, ".jd-sshd-*")
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
	if err := os.Chmod(tmp.Name(), mode); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

func restoreSSHFile(path, original string, existed bool) {
	if existed {
		mode := os.FileMode(0o600)
		if st, err := os.Stat(path); err == nil {
			mode = st.Mode().Perm()
		}
		os.WriteFile(path, []byte(original), mode)
		return
	}
	os.Remove(path)
}

func sshdTest(ctx context.Context) (bool, string) {
	bin, ok := sshdBinary()
	if !ok {
		return false, "sshd binary not found, so the configuration could not be tested"
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	out, err := hostexec.CommandOnHost(ctx, bin, "-t").CombinedOutput()
	return err == nil, strings.TrimSpace(string(out))
}

// reloadSSH asks the daemon to re-read its configuration. The unit is called
// ssh on Debian-family systems and sshd on the others, and there is no way to
// know which without trying — so both are tried, and the socket-activated
// arrangement on recent Ubuntu is why ssh.socket is in the list too.
func reloadSSH(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	var last string
	for _, unit := range []string{"ssh", "sshd", "ssh.socket"} {
		out, err := hostexec.CommandOnHost(ctx, "systemctl", "reload-or-restart", unit).CombinedOutput()
		if err == nil {
			return nil
		}
		last = strings.TrimSpace(string(out))
	}
	if last == "" {
		last = "no ssh unit responded to a reload"
	}
	return fmt.Errorf("%s", last)
}
