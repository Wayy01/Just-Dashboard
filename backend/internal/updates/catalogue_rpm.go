package updates

import (
	"bufio"
	"context"
	"strconv"
	"strings"
	"time"
)

// The RPM world: dnf, yum and zypper.
//
// They share a database and differ only in the front end, so the installed
// list and the file list are `rpm` for all three and only the repository
// half — search, info, install, remove — is per manager. Reading the installed
// set through dnf instead would mean a metadata cache has to be present to
// answer a question about the local disk, which on a host whose repositories
// have gone stale is an empty page rather than an answer.

// rpmListFormat asks rpm for the same columns dpkg is asked for. %{SIZE} is
// already bytes, unlike dpkg's kibibytes.
const rpmListFormat = "%{NAME}\t%{VERSION}-%{RELEASE}\t%{ARCH}\t%{SIZE}\t%{GROUP}\t%{SUMMARY}\n"

func rpmInstalled(ctx context.Context) ([]InstalledPackage, error) {
	out, err := run(ctx, 60*time.Second, "rpm", "-qa", "--qf", rpmListFormat)
	if err != nil && out == "" {
		return nil, err
	}
	return parseRPMList(out), nil
}

func parseRPMList(out string) []InstalledPackage {
	packages := []InstalledPackage{}
	sc := bufio.NewScanner(strings.NewReader(out))
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		fields := strings.Split(sc.Text(), "\t")
		if len(fields) < 6 || fields[0] == "" {
			continue
		}
		p := InstalledPackage{
			Name: fields[0], Version: fields[1], Architecture: fields[2],
			Section: fields[4], Summary: strings.TrimSpace(fields[5]),
		}
		if n, err := strconv.ParseInt(strings.TrimSpace(fields[3]), 10, 64); err == nil {
			p.Size = n
		}
		packages = append(packages, p)
	}
	return packages
}

func rpmFiles(ctx context.Context, name string) ([]string, error) {
	out, err := run(ctx, 30*time.Second, "rpm", "-ql", name)
	if err != nil && strings.TrimSpace(out) == "" {
		return nil, err
	}
	// rpm answers a package that owns nothing with a sentence rather than an
	// empty list, and treating that as a path puts "(contains no files)" in
	// the commands column.
	lines := splitLines(out)
	if len(lines) == 1 && !strings.HasPrefix(lines[0], "/") {
		return nil, nil
	}
	return lines, nil
}

// parseKeyedInfo reads the `Key : value` block dnf, dnf5 and zypper all print,
// where a continuation line is indented and repeats the colon.
//
// The three spell their keys differently — dnf's "URL" is zypper's "Upstream
// URL", dnf's "Architecture" is zypper's "Arch" — so the caller passes what it
// is looking for and this returns whatever was found, lowercased and with the
// spacing collapsed. Matching on the label rather than the position is what
// survives dnf5's rewrite of the same output.
func parseKeyedInfo(out string) map[string]string {
	fields := map[string]string{}
	current := ""
	sc := bufio.NewScanner(strings.NewReader(out))
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		line := sc.Text()
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "---") {
			continue
		}
		key, value, ok := strings.Cut(line, ":")
		indented := strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t")
		if ok && !(indented && strings.TrimSpace(key) == "") {
			// A wrapped description continues under an empty key.
			if k := strings.ToLower(strings.TrimSpace(key)); k != "" && !indented {
				current = k
				fields[current] = strings.TrimSpace(value)
				continue
			}
			if current != "" {
				fields[current] = strings.TrimSpace(fields[current] + "\n" + strings.TrimSpace(value))
			}
			continue
		}
		// zypper's description is indented prose with no colon at all.
		if current != "" && indented {
			fields[current] = strings.TrimSpace(fields[current] + "\n" + trimmed)
		}
	}
	return fields
}

// pick returns the first of the named keys that is present, since the same
// fact is labelled differently by each of the three.
func pick(fields map[string]string, keys ...string) string {
	for _, k := range keys {
		if v := fields[k]; v != "" {
			return v
		}
	}
	return ""
}

// parseHumanSize reads the "1.6 M" / "1.2 MiB" sizes these tools print instead
// of bytes. Returns 0 when it is not a size, which is the honest answer — an
// invented number in a size column is worse than a blank one.
func parseHumanSize(value string) int64 {
	fields := strings.Fields(value)
	if len(fields) == 0 {
		return 0
	}
	n, err := strconv.ParseFloat(strings.ReplaceAll(fields[0], ",", ""), 64)
	if err != nil {
		return 0
	}
	unit := ""
	if len(fields) > 1 {
		unit = strings.ToLower(fields[1])
	}
	switch {
	case strings.HasPrefix(unit, "k"):
		n *= 1024
	case strings.HasPrefix(unit, "m"):
		n *= 1024 * 1024
	case strings.HasPrefix(unit, "g"):
		n *= 1024 * 1024 * 1024
	}
	return int64(n)
}

// detailFromKeyed turns one of those blocks into a PackageDetail.
func detailFromKeyed(fields map[string]string, fallbackName string) *PackageDetail {
	name := pick(fields, "name")
	if name == "" {
		name = fallbackName
	}
	version := pick(fields, "version")
	if release := pick(fields, "release"); release != "" && !strings.Contains(version, release) {
		version += "-" + release
	}
	return &PackageDetail{
		Name:         name,
		Version:      version,
		Architecture: pick(fields, "architecture", "arch"),
		Repository:   pick(fields, "repository", "repo", "from repo"),
		Summary:      pick(fields, "summary"),
		Description:  pick(fields, "description"),
		Homepage:     pick(fields, "url", "upstream url"),
		License:      pick(fields, "license"),
		Maintainer:   pick(fields, "vendor", "packager"),
		Section:      pick(fields, "group"),
		Size:         parseHumanSize(pick(fields, "size", "installed size", "download size")),
	}
}

// --- dnf and yum -------------------------------------------------------

func (m dnfManager) ListInstalled(ctx context.Context) ([]InstalledPackage, error) {
	packages, err := rpmInstalled(ctx)
	if err != nil {
		return nil, err
	}
	// repoquery is what dnf has instead of apt-mark: the set somebody asked
	// for rather than the set that came with it. It needs the metadata cache,
	// so a host whose repositories are unreachable simply reports nothing
	// explicit rather than failing the whole listing.
	if out, err := run(ctx, 60*time.Second, m.binary, "-q", "--cacheonly",
		"repoquery", "--userinstalled", "--qf", "%{name}"); err == nil {
		explicit := map[string]bool{}
		for _, line := range splitLines(out) {
			explicit[line] = true
		}
		for i := range packages {
			packages[i].Explicit = explicit[packages[i].Name]
		}
	}
	return packages, nil
}

func (m dnfManager) Search(ctx context.Context, query string) ([]CatalogEntry, error) {
	out, code, err := runCode(ctx, 45*time.Second, m.binary, "-q", "--cacheonly", "search", query)
	// dnf exits 1 when a search matches nothing, which is an answer rather
	// than a failure.
	if err != nil && code != 1 && strings.TrimSpace(out) == "" {
		return nil, dnfError(out, err)
	}
	entries := parseDNFSearch(out)
	// dnf's plain search covers name and summary; --all adds the description,
	// which is where "web server" lives.
	if len(entries) < thinSearch {
		if full, _, err := runCode(ctx, 45*time.Second, m.binary, "-q", "--cacheonly",
			"search", "--all", query); err == nil || strings.TrimSpace(full) != "" {
			entries = widen(entries, parseDNFSearch(full))
		}
	}
	return entries, nil
}

// parseDNFSearch reads the `name.arch : summary` rows under dnf's headings.
//
// The headings — "Name Exactly Matched", "Name & Summary Matched" — are dnf's
// own ranking, and they are dropped rather than used: rankResults orders every
// manager's answer by the same rule, so the results do not reorder themselves
// when an operator moves from a Fedora host to a Debian one.
func parseDNFSearch(out string) []CatalogEntry {
	entries := []CatalogEntry{}
	sc := bufio.NewScanner(strings.NewReader(out))
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "=") || strings.HasPrefix(line, "Last metadata") {
			continue
		}
		left, summary, ok := strings.Cut(line, " : ")
		if !ok {
			continue
		}
		name := strings.TrimSpace(left)
		// The architecture suffix is dnf's, not part of the name — installing
		// "nginx.x86_64" works but reads as a filename to everybody else. The
		// cut is at the *last* dot: python3.11.x86_64 is a real package whose
		// name contains one, and cutting at the first leaves python3.
		if base, arch, ok := cutLast(name, "."); ok && isArchSuffix(arch) {
			name = base
		}
		if name == "" {
			continue
		}
		entries = append(entries, CatalogEntry{Name: name, Summary: strings.TrimSpace(summary)})
	}
	return entries
}

// isArchSuffix keeps a dotted package name — python3.11, lib32-foo.i686 — from
// being cut at the wrong dot.
func isArchSuffix(s string) bool {
	switch s {
	case "x86_64", "i686", "i386", "noarch", "aarch64", "armv7hl", "ppc64le", "s390x", "src":
		return true
	}
	return false
}

func (m dnfManager) Info(ctx context.Context, name string) (*PackageDetail, error) {
	out, code, err := runCode(ctx, 45*time.Second, m.binary, "-q", "--cacheonly", "info", name)
	if strings.TrimSpace(out) == "" {
		if err != nil && code != 1 {
			return nil, dnfError(out, err)
		}
		return nil, ErrUnknownPackage
	}
	fields := parseKeyedInfo(out)
	if len(fields) == 0 {
		return nil, ErrUnknownPackage
	}
	return detailFromKeyed(fields, name), nil
}

func (m dnfManager) Files(ctx context.Context, name string) ([]string, error) {
	return rpmFiles(ctx, name)
}

func (m dnfManager) InstallCommand(names []string) (string, []string, []string) {
	return m.binary, append([]string{"install", "-y"}, names...), []string{"LC_ALL=C"}
}

func (m dnfManager) RemoveCommand(names []string, _ bool) (string, []string, []string) {
	return m.binary, append([]string{"remove", "-y"}, names...), []string{"LC_ALL=C"}
}

// RPM leaves a modified configuration file behind as .rpmsave whatever the
// front end is asked to do, so there is no purge to offer and the UI hides the
// switch rather than showing one that does nothing.
func (dnfManager) SupportsPurge() bool { return false }

func (dnfManager) IndexAge() (time.Time, bool) {
	return newestMtime("/var/cache/dnf", "/var/cache/yum")
}

func (m dnfManager) RefreshCommand() (string, []string, []string, bool) {
	return m.binary, []string{"makecache"}, []string{"LC_ALL=C"}, true
}

// --- zypper ------------------------------------------------------------

func (zypperManager) ListInstalled(ctx context.Context) ([]InstalledPackage, error) {
	// zypper keeps the user-installed set inside libzypp's solver database
	// with no supported way to query it, so Explicit stays false here and the
	// "installed by hand" filter finds nothing rather than something wrong.
	return rpmInstalled(ctx)
}

func (zypperManager) Search(ctx context.Context, query string) ([]CatalogEntry, error) {
	out, code, err := runCode(ctx, 45*time.Second, "zypper", "--non-interactive", "--no-refresh",
		"--quiet", "search", query)
	if err != nil && !zypperInformational(code) && strings.TrimSpace(out) == "" {
		return nil, err
	}
	entries := parseZypperSearch(out)
	// -d searches the description as well as the name and summary.
	if len(entries) < thinSearch {
		if full, code, err := runCode(ctx, 45*time.Second, "zypper", "--non-interactive",
			"--no-refresh", "--quiet", "search", "-d", query); err == nil || zypperInformational(code) {
			entries = widen(entries, parseZypperSearch(full))
		}
	}
	return entries, nil
}

// parseZypperSearch reads the pipe table `S | Name | Summary | Type`, keeping
// only the rows whose type is a package — the same listing carries patterns,
// products and source packages, none of which is a thing to install here.
func parseZypperSearch(out string) []CatalogEntry {
	entries := []CatalogEntry{}
	sc := bufio.NewScanner(strings.NewReader(out))
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		line := sc.Text()
		if !strings.Contains(line, "|") || strings.HasPrefix(strings.TrimSpace(line), "--") {
			continue
		}
		cols := strings.Split(line, "|")
		if len(cols) < 4 {
			continue
		}
		for i := range cols {
			cols[i] = strings.TrimSpace(cols[i])
		}
		if cols[1] == "Name" || cols[1] == "" {
			continue
		}
		if cols[3] != "" && cols[3] != "package" {
			continue
		}
		entries = append(entries, CatalogEntry{Name: cols[1], Summary: cols[2]})
	}
	return entries
}

func (zypperManager) Info(ctx context.Context, name string) (*PackageDetail, error) {
	out, code, err := runCode(ctx, 45*time.Second, "zypper", "--non-interactive", "--no-refresh",
		"info", name)
	if err != nil && !zypperInformational(code) && strings.TrimSpace(out) == "" {
		return nil, err
	}
	fields := parseKeyedInfo(out)
	if fields["name"] == "" {
		return nil, ErrUnknownPackage
	}
	return detailFromKeyed(fields, name), nil
}

func (zypperManager) Files(ctx context.Context, name string) ([]string, error) {
	return rpmFiles(ctx, name)
}

func (zypperManager) InstallCommand(names []string) (string, []string, []string) {
	return "zypper", append([]string{"--non-interactive", "install"}, names...), []string{"LC_ALL=C"}
}

func (zypperManager) RemoveCommand(names []string, _ bool) (string, []string, []string) {
	// --clean-deps is zypper's --auto-remove: the dependencies that were only
	// pulled in for this go with it.
	return "zypper", append([]string{"--non-interactive", "remove", "--clean-deps"}, names...),
		[]string{"LC_ALL=C"}
}

func (zypperManager) SupportsPurge() bool { return false }

func (zypperManager) IndexAge() (time.Time, bool) {
	return newestMtime("/var/cache/zypp/raw", "/var/cache/zypp")
}

func (zypperManager) RefreshCommand() (string, []string, []string, bool) {
	return "zypper", []string{"--non-interactive", "refresh"}, []string{"LC_ALL=C"}, true
}
