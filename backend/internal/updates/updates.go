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
	"os"
	"strings"
	"time"

	"github.com/Wayy01/vps-dashboard/backend/internal/hostexec"
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
	Available      bool      `json:"available"`
	Manager        string    `json:"manager,omitempty"`
	Packages       []Package `json:"packages"`
	SecurityCount  int       `json:"securityCount"`
	RebootRequired bool      `json:"rebootRequired"`
	RebootPackages []string  `json:"rebootPackages,omitempty"`
	LastChecked    time.Time `json:"lastChecked"`
	Error          string    `json:"error,omitempty"`
}

type Service struct{}

func New() *Service { return &Service{} }

func (s *Service) Available() bool { return hostexec.AvailableOnHost("apt-get") }

// Check lists what could be upgraded right now.
//
// It simulates rather than refreshing the package index: a simulation reads
// the database already on disk and takes milliseconds, while `apt-get update`
// hits the network and takes the request's whole budget. The index is refreshed
// by the host's own timer, which is where that job belongs.
func (s *Service) Check(ctx context.Context) (*Report, error) {
	rep := &Report{Packages: []Package{}, LastChecked: time.Now().UTC()}
	if !s.Available() {
		return rep, ErrNotSupported
	}
	rep.Available = true
	rep.Manager = "apt"

	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	// Debug::NoLocking lets this run while something else holds the apt lock,
	// which matters because it is only ever reading.
	cmd := hostexec.CommandOnHost(ctx, "apt-get", "-s", "-o", "Debug::NoLocking=1", "upgrade")
	cmd.Env = append(os.Environ(), "DEBIAN_FRONTEND=noninteractive", "LC_ALL=C")
	out, err := cmd.Output()
	if err != nil {
		rep.Error = strings.TrimSpace(err.Error())
		return rep, nil
	}

	sc := bufio.NewScanner(strings.NewReader(string(out)))
	for sc.Scan() {
		p, ok := parseInstLine(sc.Text())
		if !ok {
			continue
		}
		if p.Security {
			rep.SecurityCount++
		}
		rep.Packages = append(rep.Packages, p)
	}

	rep.RebootRequired, rep.RebootPackages = s.rebootState()
	return rep, nil
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

// rebootState reads the flag Debian and Ubuntu drop when an upgraded package
// cannot take effect until the machine restarts.
func (s *Service) rebootState() (bool, []string) {
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
	return false, nil
}

// Upgrade applies pending updates.
//
// Only the non-interactive, no-new-packages form is offered: this runs
// unattended behind a web request, and a dist-upgrade that wants to remove
// something should be a decision someone makes at a real terminal. security
// narrows it to the security pocket, which is the upgrade most operators
// actually want to apply without thinking about it.
func (s *Service) Upgrade(ctx context.Context, securityOnly bool) (string, error) {
	if !s.Available() {
		return "", ErrNotSupported
	}
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
