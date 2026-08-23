package updates

import (
	"bufio"
	"context"
	"os"
	"strings"
	"time"

	"github.com/Wayy01/Just-Dashboard/backend/internal/hostexec"
)

// One interface per package manager, because "is this machine up to date" is
// the same question everywhere and the answer is spelled differently on every
// distribution.
//
// The dashboard used to answer it only on Debian and Ubuntu, and reported
// every other host as having no package manager at all — which reads as "there
// is nothing to update" rather than as "this was never checked". That is the
// worst possible failure for a security signal, and it is why the posture
// audit's pending-patch check was silently dead on half the servers people
// actually run.
//
// Each manager is a listing command whose output is parsed by a pure function,
// plus an upgrade argv. Nothing here shells through a shell; every argument is
// its own element, as everywhere else in this codebase.
type manager interface {
	// Name is what the UI shows: apt, dnf, apk, pacman, zypper.
	Name() string
	// Detect reports whether this host is run by this manager.
	Detect() bool
	// List enumerates what could be upgraded, without refreshing the index —
	// that hits the network and belongs to the host's own timer.
	List(ctx context.Context) ([]Package, error)
	// Upgrade applies them. securityOnly narrows to the security pocket where
	// the manager has one; managers that do not are documented on the method.
	Upgrade(ctx context.Context, securityOnly bool) (string, error)
	// SupportsSecurityOnly reports whether narrowing means anything here. On
	// Alpine and Arch it does not, and offering the switch would be a lie.
	SupportsSecurityOnly() bool
}

// managers are tried in order. The order matters on the handful of hosts that
// have two: a Debian derivative with dnf installed for a container build is
// still an apt machine.
func managers() []manager {
	return []manager{
		aptManager{}, dnfManager{binary: "dnf"}, dnfManager{binary: "yum"},
		zypperManager{}, pacmanManager{}, apkManager{},
	}
}

// detect returns the first manager that claims the host.
func detect() manager {
	for _, m := range managers() {
		if m.Detect() {
			return m
		}
	}
	return nil
}

// run is the shared invocation: on the host, bounded, with a predictable
// locale so the parsers are not reading a translated table.
func run(ctx context.Context, limit time.Duration, name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, limit)
	defer cancel()
	cmd := hostexec.CommandOnHost(ctx, name, args...)
	cmd.Env = append(os.Environ(), "LC_ALL=C", "DEBIAN_FRONTEND=noninteractive")
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// --- dnf and yum -------------------------------------------------------

type dnfManager struct{ binary string }

func (m dnfManager) Name() string               { return m.binary }
func (m dnfManager) Detect() bool               { return hostexec.AvailableOnHost(m.binary) }
func (m dnfManager) SupportsSecurityOnly() bool { return true }

// List reads `check-update`, which exits 100 when there is something to do.
//
// That exit code is the whole trap: treated as a failure, a host with updates
// waiting reports an error and a host with none reports success, which is
// exactly backwards from what an operator would guess when the page is empty.
func (m dnfManager) List(ctx context.Context) ([]Package, error) {
	out, err := run(ctx, 90*time.Second, m.binary, "-q", "--cacheonly", "check-update")
	if err != nil && !strings.Contains(out, "\n") {
		return nil, err
	}
	packages := parseDNFCheckUpdate(out)

	// Security metadata lives in a separate index, so a second call is the
	// only way to know which of these are advisories rather than version
	// bumps. A host with no updateinfo (a plain CentOS Stream mirror, most
	// container images) simply marks nothing, which is honest.
	if sec, err := run(ctx, 90*time.Second, m.binary, "-q", "--cacheonly", "updateinfo", "list", "--security"); err == nil {
		names := parseDNFSecurity(sec)
		for i := range packages {
			if names[packages[i].Name] {
				packages[i].Security = true
			}
		}
	}
	return packages, nil
}

func (m dnfManager) Upgrade(ctx context.Context, securityOnly bool) (string, error) {
	args := []string{"-y"}
	if securityOnly {
		args = append(args, "--security")
	}
	args = append(args, "upgrade")
	return run(ctx, 30*time.Minute, m.binary, args...)
}

// parseDNFCheckUpdate reads the three-column table check-update prints:
//
//	kernel.x86_64          5.14.0-503.el9    baseos
//
// Obsoleting sections and the blank-line-separated preamble are skipped: a
// line only counts when it has three fields and the first carries an
// architecture suffix.
func parseDNFCheckUpdate(out string) []Package {
	packages := []Package{}
	obsoleting := false
	sc := bufio.NewScanner(strings.NewReader(out))
	for sc.Scan() {
		line := strings.TrimRight(sc.Text(), " \t")
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "Obsoleting") {
			obsoleting = true
			continue
		}
		// Inside the Obsoleting section dnf prints the replacing package flush
		// left and the package it replaces indented beneath it. The first is a
		// real upgrade; the second is already going away and counting it would
		// report one change as two.
		if obsoleting && (strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t")) {
			continue
		}
		fields := strings.Fields(trimmed)
		if len(fields) != 3 {
			continue
		}
		name, arch, ok := strings.Cut(fields[0], ".")
		if !ok || name == "" {
			continue
		}
		packages = append(packages, Package{
			Name: name, Candidate: fields[1], Origin: fields[2], Architecture: arch,
		})
	}
	return packages
}

// parseDNFSecurity reads `updateinfo list --security`, whose rows are
//
//	RHSA-2024:1234 Important/Sec. openssl-3.0.7-27.el9.x86_64
//
// The package name has to be recovered from an NEVRA, which has no separator
// distinguishing the name from the version — the convention is that the
// version begins at the last dash followed by a digit.
func parseDNFSecurity(out string) map[string]bool {
	names := map[string]bool{}
	sc := bufio.NewScanner(strings.NewReader(out))
	for sc.Scan() {
		fields := strings.Fields(strings.TrimSpace(sc.Text()))
		if len(fields) < 3 {
			continue
		}
		if name := nameFromNEVRA(fields[len(fields)-1]); name != "" {
			names[name] = true
		}
	}
	return names
}

// nameFromNEVRA strips the version, release and architecture off an RPM
// identifier, leaving the package name.
func nameFromNEVRA(nevra string) string {
	// Drop the architecture, which is always the final dotted component.
	if i := strings.LastIndex(nevra, "."); i > 0 {
		nevra = nevra[:i]
	}
	for i := len(nevra) - 1; i > 0; i-- {
		if nevra[i] != '-' {
			continue
		}
		if i+1 < len(nevra) && nevra[i+1] >= '0' && nevra[i+1] <= '9' {
			// The release dash comes after the version dash, so keep going
			// until the earliest one that starts a digit run.
			candidate := nevra[:i]
			if j := strings.LastIndex(candidate, "-"); j > 0 &&
				j+1 < len(candidate) && candidate[j+1] >= '0' && candidate[j+1] <= '9' {
				return candidate[:j]
			}
			return candidate
		}
	}
	return nevra
}

// --- zypper ------------------------------------------------------------

type zypperManager struct{}

func (zypperManager) Name() string               { return "zypper" }
func (zypperManager) Detect() bool               { return hostexec.AvailableOnHost("zypper") }
func (zypperManager) SupportsSecurityOnly() bool { return true }

func (m zypperManager) List(ctx context.Context) ([]Package, error) {
	out, err := run(ctx, 90*time.Second, "zypper", "--non-interactive", "--no-refresh", "list-updates")
	if err != nil {
		return nil, err
	}
	packages := parseZypperUpdates(out)
	if sec, err := run(ctx, 90*time.Second, "zypper", "--non-interactive", "--no-refresh",
		"list-patches", "--category", "security"); err == nil {
		names := parseZypperPatches(sec)
		for i := range packages {
			if names[packages[i].Name] {
				packages[i].Security = true
			}
		}
	}
	return packages, nil
}

func (m zypperManager) Upgrade(ctx context.Context, securityOnly bool) (string, error) {
	if securityOnly {
		return run(ctx, 30*time.Minute, "zypper", "--non-interactive", "patch", "--category", "security")
	}
	return run(ctx, 30*time.Minute, "zypper", "--non-interactive", "update")
}

// parseZypperUpdates reads the pipe-delimited table:
//
//	v | Repository | Name | Current Version | Available Version | Arch
func parseZypperUpdates(out string) []Package {
	packages := []Package{}
	sc := bufio.NewScanner(strings.NewReader(out))
	for sc.Scan() {
		line := sc.Text()
		if !strings.Contains(line, "|") || strings.HasPrefix(strings.TrimSpace(line), "--") {
			continue
		}
		cols := strings.Split(line, "|")
		if len(cols) < 6 {
			continue
		}
		for i := range cols {
			cols[i] = strings.TrimSpace(cols[i])
		}
		// The heading row names its own columns; skipping by content rather
		// than by position survives zypper choosing a different column order.
		if cols[2] == "Name" || cols[0] != "v" {
			continue
		}
		packages = append(packages, Package{
			Name: cols[2], Current: cols[3], Candidate: cols[4],
			Origin: cols[1], Architecture: cols[5],
		})
	}
	return packages
}

// parseZypperPatches collects the package names a security patch touches. The
// table names the patch rather than the packages, so the patch name is used —
// zypper's patch names are package-shaped for the common single-package case.
func parseZypperPatches(out string) map[string]bool {
	names := map[string]bool{}
	sc := bufio.NewScanner(strings.NewReader(out))
	for sc.Scan() {
		cols := strings.Split(sc.Text(), "|")
		if len(cols) < 3 {
			continue
		}
		name := strings.TrimSpace(cols[1])
		if name == "" || name == "Name" {
			continue
		}
		// A patch is named like openSUSE-SLE-15.5-2024-1234 or
		// openssl-3.0.8-1; only the second shape names a package.
		if base, _, ok := strings.Cut(name, "-"); ok && base != "" {
			names[base] = true
		}
		names[name] = true
	}
	return names
}

// --- pacman ------------------------------------------------------------

type pacmanManager struct{}

func (pacmanManager) Name() string { return "pacman" }
func (pacmanManager) Detect() bool { return hostexec.AvailableOnHost("pacman") }

// Arch publishes no security metadata in the package database — advisories
// live on a separate site — so offering a security-only upgrade would be a
// switch that quietly does the same thing as the other one.
func (pacmanManager) SupportsSecurityOnly() bool { return false }

func (m pacmanManager) List(ctx context.Context) ([]Package, error) {
	// -Qu lists upgradable packages from the database already on disk. Its
	// exit status is 1 when there is nothing to do, which is not an error.
	out, _ := run(ctx, 60*time.Second, "pacman", "-Qu")
	return parsePacmanUpdates(out), nil
}

// Upgrade is the full -Syu. Arch does not support partial upgrades: applying
// packages against a stale database is the documented way to break the system,
// so refreshing here is correct even though every other manager reads on-disk
// state.
func (m pacmanManager) Upgrade(ctx context.Context, _ bool) (string, error) {
	return run(ctx, 30*time.Minute, "pacman", "-Syu", "--noconfirm")
}

// parsePacmanUpdates reads `name 1.0-1 -> 1.1-1`.
func parsePacmanUpdates(out string) []Package {
	packages := []Package{}
	sc := bufio.NewScanner(strings.NewReader(out))
	for sc.Scan() {
		fields := strings.Fields(strings.TrimSpace(sc.Text()))
		if len(fields) != 4 || fields[2] != "->" {
			continue
		}
		packages = append(packages, Package{
			Name: fields[0], Current: fields[1], Candidate: fields[3],
		})
	}
	return packages
}

// --- apk (Alpine) ------------------------------------------------------

type apkManager struct{}

func (apkManager) Name() string { return "apk" }
func (apkManager) Detect() bool { return hostexec.AvailableOnHost("apk") }

// Alpine's index carries no advisory data either.
func (apkManager) SupportsSecurityOnly() bool { return false }

func (m apkManager) List(ctx context.Context) ([]Package, error) {
	out, err := run(ctx, 60*time.Second, "apk", "version", "-l", "<")
	if err != nil {
		return nil, err
	}
	return parseApkVersions(out), nil
}

func (m apkManager) Upgrade(ctx context.Context, _ bool) (string, error) {
	// apk resolves against the index it has, and Alpine's is usually stale in
	// a long-running container. --no-cache fetches a fresh one for this run
	// without leaving it on disk.
	return run(ctx, 30*time.Minute, "apk", "upgrade", "--no-cache")
}

// parseApkVersions reads `apk version -l '<'`:
//
//	busybox-1.36.1-r5    <  1.36.1-r7
//
// The left column is an Alpine package identifier with the version glued on,
// which is separated the same way an RPM's is: at the last dash before a digit.
func parseApkVersions(out string) []Package {
	packages := []Package{}
	sc := bufio.NewScanner(strings.NewReader(out))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "Installed:") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 3 || fields[1] != "<" {
			continue
		}
		name, current := splitApkVersion(fields[0])
		if name == "" {
			continue
		}
		packages = append(packages, Package{Name: name, Current: current, Candidate: fields[2]})
	}
	return packages
}

// splitApkVersion separates busybox-1.36.1-r5 into its name and version. The
// version starts at the first dash followed by a digit, scanning from the
// left, because a package name may itself contain dashes and digits but never
// a component that begins with one.
func splitApkVersion(id string) (string, string) {
	for i := 0; i < len(id)-1; i++ {
		if id[i] == '-' && id[i+1] >= '0' && id[i+1] <= '9' {
			return id[:i], id[i+1:]
		}
	}
	return id, ""
}
