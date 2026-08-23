// Package updates reports the operating system patches a server is missing.
//
// "Is this machine up to date, and does it need a reboot?" is one of the most
// common reasons to open an SSH session, and it is a question the dashboard
// can answer without one. Everything here runs on the host: the package
// database that matters belongs to the server, not to this container's image.
package updates

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/Wayy01/Just-Dashboard/backend/internal/hostexec"
)

var ErrNotSupported = errors.New("no supported package manager on this host")

// Package is one upgradable package.
type Package struct {
	Name         string `json:"name"`
	Current      string `json:"current"`
	Candidate    string `json:"candidate"`
	Origin       string `json:"origin,omitempty"`
	Security     bool   `json:"security"`
	Architecture string `json:"arch,omitempty"`
}

// Report is the answer to "how far behind is this server".
type Report struct {
	Available     bool      `json:"available"`
	Manager       string    `json:"manager,omitempty"`
	Packages      []Package `json:"packages"`
	SecurityCount int       `json:"securityCount"`
	// SecurityFiltering reports whether this manager can tell a security
	// update from any other. Alpine and Arch cannot, and a UI that offered
	// the switch anyway would be promising something it does not do.
	SecurityFiltering bool      `json:"securityFiltering"`
	RebootRequired    bool      `json:"rebootRequired"`
	RebootPackages    []string  `json:"rebootPackages,omitempty"`
	LastChecked       time.Time `json:"lastChecked"`
	Error             string    `json:"error,omitempty"`
}

// Report is produced by whichever manager claims the host.
type Service struct{}

func New() *Service { return &Service{} }

// Available reports whether *any* supported package manager was found. It used
// to mean "apt is installed", which made every RPM, Alpine and Arch host look
// like a machine with nothing to update rather than one that was never
// checked — the worst possible failure for a security signal.
func (s *Service) Available() bool { return detect() != nil }

// Manager names what runs this host, so the UI can say apt or dnf rather than
// "packages".
func (s *Service) Manager() string {
	if m := detect(); m != nil {
		return m.Name()
	}
	return ""
}

// Check lists what could be upgraded right now.
//
// It reads the package database already on disk rather than refreshing it: a
// refresh hits the network and takes the request's whole budget, and keeping
// the index current is the host's own timer's job. pacman is the documented
// exception and says so at its Upgrade.
func (s *Service) Check(ctx context.Context) (*Report, error) {
	rep := &Report{Packages: []Package{}, LastChecked: time.Now().UTC()}
	m := detect()
	if m == nil {
		return rep, ErrNotSupported
	}
	rep.Available = true
	rep.Manager = m.Name()
	rep.SecurityFiltering = m.SupportsSecurityOnly()

	packages, err := m.List(ctx)
	if err != nil {
		rep.Error = strings.TrimSpace(err.Error())
		return rep, nil
	}
	for _, p := range packages {
		if p.Security {
			rep.SecurityCount++
		}
	}
	rep.Packages = packages
	rep.RebootRequired, rep.RebootPackages = s.rebootState(ctx)
	return rep, nil
}

// Upgrade applies pending updates.
//
// Only the non-interactive, no-new-packages form is offered: this runs
// unattended behind a web request, and a dist-upgrade that wants to remove
// something should be a decision somebody makes at a real terminal.
func (s *Service) Upgrade(ctx context.Context, securityOnly bool) (string, error) {
	m := detect()
	if m == nil {
		return "", ErrNotSupported
	}
	if err := guardSecurityOnly(m, securityOnly); err != nil {
		return "", err
	}
	return m.Upgrade(ctx, securityOnly)
}

// guardSecurityOnly refuses a narrowed upgrade where narrowing means nothing.
//
// Alpine and Arch publish no advisory data, so a manager that quietly applied
// everything would be doing the opposite of what the switch says. The UI hides
// the control for the same reason; this is the half that holds when somebody
// calls the API directly.
func guardSecurityOnly(m manager, securityOnly bool) error {
	if securityOnly && !m.SupportsSecurityOnly() {
		return fmt.Errorf("%s publishes no security metadata, so a security-only upgrade is not something it can do", m.Name())
	}
	return nil
}

// aptManager is Debian, Ubuntu and their derivatives.
type aptManager struct{}

func (aptManager) Name() string               { return "apt" }
func (aptManager) Detect() bool               { return hostexec.AvailableOnHost("apt-get") }
func (aptManager) SupportsSecurityOnly() bool { return true }

func (aptManager) List(ctx context.Context) ([]Package, error) {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	// Debug::NoLocking lets this run while something else holds the apt lock,
	// which matters because it is only ever reading.
	cmd := hostexec.CommandOnHost(ctx, "apt-get", "-s", "-o", "Debug::NoLocking=1", "upgrade")
	cmd.Env = append(os.Environ(), "DEBIAN_FRONTEND=noninteractive", "LC_ALL=C")
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	packages := []Package{}
	sc := bufio.NewScanner(strings.NewReader(string(out)))
	for sc.Scan() {
		if p, ok := parseInstLine(sc.Text()); ok {
			packages = append(packages, p)
		}
	}
	return packages, nil
}

func (aptManager) Upgrade(ctx context.Context, securityOnly bool) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Minute)
	defer cancel()
	args := []string{"-y", "--no-install-recommends", "-o", "Dpkg::Options::=--force-confold", "upgrade"}
	if securityOnly {
		// Restricting to the security pocket keeps a routine patch run from
		// pulling in every unrelated version bump.
		args = append([]string{"-t", detectSecuritySuite()}, args...)
	}
	cmd := hostexec.CommandOnHost(ctx, "apt-get", args...)
	cmd.Env = append(os.Environ(),
		"DEBIAN_FRONTEND=noninteractive",
		"LC_ALL=C",
		"NEEDRESTART_MODE=a",
	)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// parseInstLine reads one simulated-install line, which looks like:
//
//	Inst libssl3 [3.0.13-0ubuntu3.4] (3.0.13-0ubuntu3.5 Ubuntu:24.04/noble-security [amd64])
//
// The origin in parentheses is what marks a security update; matching on it is
// more reliable than guessing from the version string.
func parseInstLine(line string) (Package, bool) {
	if !strings.HasPrefix(line, "Inst ") {
		return Package{}, false
	}
	rest := strings.TrimPrefix(line, "Inst ")
	fields := strings.Fields(rest)
	if len(fields) == 0 {
		return Package{}, false
	}
	p := Package{Name: fields[0]}
	// The installed version, when there is one, sits in brackets before the
	// candidate's parenthesis. Searching the whole line instead would pick up
	// the architecture out of the origin on a package being newly installed,
	// which has no current version at all.
	head := rest
	if open := strings.Index(rest, "("); open >= 0 {
		head = rest[:open]
	}
	if i, j := strings.Index(head, "["), strings.Index(head, "]"); i >= 0 && j > i {
		p.Current = head[i+1 : j]
	}
	if i, j := strings.Index(rest, "("), strings.LastIndex(rest, ")"); i >= 0 && j > i {
		inner := strings.Fields(rest[i+1 : j])
		if len(inner) > 0 {
			p.Candidate = inner[0]
		}
		if len(inner) > 1 {
			p.Origin = strings.Join(inner[1:], " ")
		}
		lower := strings.ToLower(p.Origin)
		p.Security = strings.Contains(lower, "-security") || strings.Contains(lower, "securityppa")
		if k := strings.LastIndex(p.Origin, "["); k >= 0 {
			p.Architecture = strings.Trim(p.Origin[k:], "[]")
		}
	}
	return p, true
}

// rebootState answers whether the running kernel and libraries are still the
// ones on disk.
//
// Debian drops a flag file; the RPM world has a tool that answers directly and
// nothing to read otherwise. Where neither exists the answer is "cannot tell",
// reported as false — an invented "no reboot needed" and a real one look the
// same, so the check that can answer is tried first.
func (s *Service) rebootState(ctx context.Context) (bool, []string) {
	// The container sees the host root at /host; check both so this works
	// whether or not it is containerised.
	for _, base := range []string{"", "/host"} {
		if _, err := os.Stat(base + "/var/run/reboot-required"); err != nil {
			continue
		}
		pkgs := []string{}
		if b, err := os.ReadFile(base + "/var/run/reboot-required.pkgs"); err == nil {
			seen := map[string]bool{}
			for _, l := range strings.Split(string(b), "\n") {
				if l = strings.TrimSpace(l); l != "" && !seen[l] {
					seen[l] = true
					pkgs = append(pkgs, l)
				}
			}
		}
		return true, pkgs
	}
	// dnf's needs-restarting exits 1 when a reboot is required, which is the
	// RPM world's equivalent of the flag file above.
	if hostexec.AvailableOnHost("needs-restarting") {
		runCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		if err := hostexec.CommandOnHost(runCtx, "needs-restarting", "-r").Run(); err != nil {
			return true, nil
		}
	}
	return false, nil
}

// detectSecuritySuite guesses the security pocket name from the host's release.
func detectSecuritySuite() string {
	for _, base := range []string{"", "/host"} {
		b, err := os.ReadFile(base + "/etc/os-release")
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(b), "\n") {
			if v, ok := strings.CutPrefix(line, "VERSION_CODENAME="); ok {
				return strings.Trim(strings.TrimSpace(v), `"`) + "-security"
			}
		}
	}
	return "stable-security"
}
