package updates

import (
	"bufio"
	"context"
	"os"
	"strings"
	"time"
)

// pacman and apk: the two managers whose listings carry the least, and the two
// where the gap is filled by reading a file the tool keeps rather than by
// calling it again per package.

// --- pacman ------------------------------------------------------------

func (pacmanManager) ListInstalled(ctx context.Context) ([]InstalledPackage, error) {
	// -Qi in one call rather than -Q followed by a query per package: on a
	// desktop Arch install that is the difference between one subprocess and
	// twelve hundred.
	out, err := run(ctx, 60*time.Second, "pacman", "-Qi")
	if err != nil && strings.TrimSpace(out) == "" {
		return nil, err
	}
	return parsePacmanInfoBlocks(out), nil
}

// parsePacmanInfoBlocks reads the blank-line-separated stanzas `pacman -Qi`
// prints, one per package.
//
// "Install Reason" is pacman's apt-mark: "Explicitly installed" against
// "Installed as a dependency for another package". It is the field that makes
// the list readable, so it is matched on the first word rather than on the
// whole sentence, which pacman has reworded before.
func parsePacmanInfoBlocks(out string) []InstalledPackage {
	packages := []InstalledPackage{}
	for _, block := range splitBlocks(out) {
		fields := parseKeyedInfo(block)
		name := fields["name"]
		if name == "" {
			continue
		}
		packages = append(packages, InstalledPackage{
			Name:         name,
			Version:      fields["version"],
			Architecture: fields["architecture"],
			Summary:      fields["description"],
			Section:      fields["groups"],
			Size:         parseHumanSize(fields["installed size"]),
			Explicit:     strings.HasPrefix(strings.ToLower(fields["install reason"]), "explicit"),
		})
	}
	return packages
}

// splitBlocks cuts a listing into its blank-line-separated records.
func splitBlocks(out string) []string {
	blocks := []string{}
	current := []string{}
	sc := bufio.NewScanner(strings.NewReader(out))
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		line := sc.Text()
		if strings.TrimSpace(line) == "" {
			if len(current) > 0 {
				blocks = append(blocks, strings.Join(current, "\n"))
				current = current[:0]
			}
			continue
		}
		current = append(current, line)
	}
	if len(current) > 0 {
		blocks = append(blocks, strings.Join(current, "\n"))
	}
	return blocks
}

func (pacmanManager) Search(ctx context.Context, query string) ([]CatalogEntry, error) {
	// pacman -Ss exits 1 on no match, which is an answer.
	out, _ := run(ctx, 45*time.Second, "pacman", "-Ss", query)
	return parsePacmanSearch(out), nil
}

// parsePacmanSearch reads the two-line records `pacman -Ss` prints:
//
//	extra/nginx 1.27.0-1 [installed]
//	    Lightweight HTTP server and IMAP/POP3 proxy server
//
// The description is the indented line under the header, which is why this
// cannot be a line-at-a-time parser like every other search here.
func parsePacmanSearch(out string) []CatalogEntry {
	entries := []CatalogEntry{}
	sc := bufio.NewScanner(strings.NewReader(out))
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		line := sc.Text()
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t") {
			if n := len(entries); n > 0 && entries[n-1].Summary == "" {
				entries[n-1].Summary = strings.TrimSpace(line)
			}
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		repo, name, ok := strings.Cut(fields[0], "/")
		if !ok {
			continue
		}
		entries = append(entries, CatalogEntry{Name: name, Version: fields[1], Repository: repo})
	}
	return entries
}

func (pacmanManager) Info(ctx context.Context, name string) (*PackageDetail, error) {
	// -Si is the repository's copy; -Qi is the local one, and a package built
	// from the AUR exists only in the second.
	out, _ := run(ctx, 30*time.Second, "pacman", "-Si", name)
	if strings.TrimSpace(out) == "" {
		out, _ = run(ctx, 30*time.Second, "pacman", "-Qi", name)
	}
	fields := parseKeyedInfo(out)
	if fields["name"] == "" {
		return nil, ErrUnknownPackage
	}
	detail := &PackageDetail{
		Name:         fields["name"],
		Version:      fields["version"],
		Architecture: fields["architecture"],
		Repository:   pick(fields, "repository"),
		Summary:      fields["description"],
		Homepage:     pick(fields, "url"),
		License:      pick(fields, "licenses", "license"),
		Maintainer:   pick(fields, "packager"),
		Section:      pick(fields, "groups"),
		Size:         parseHumanSize(pick(fields, "installed size", "download size")),
	}
	if deps := pick(fields, "depends on"); deps != "" && deps != "None" {
		detail.Dependencies = splitDependencies(strings.ReplaceAll(deps, "  ", ","))
	}
	return detail, nil
}

func (pacmanManager) Files(ctx context.Context, name string) ([]string, error) {
	out, err := run(ctx, 30*time.Second, "pacman", "-Ql", name)
	if err != nil && strings.TrimSpace(out) == "" {
		return nil, err
	}
	// Every line is `name /path`; the name is the same on all of them.
	files := []string{}
	for _, line := range splitLines(out) {
		if _, path, ok := strings.Cut(line, " "); ok {
			files = append(files, strings.TrimSpace(path))
		}
	}
	return files, nil
}

func (pacmanManager) InstallCommand(names []string) (string, []string, []string) {
	// -Sy would refresh the database and then install against it, which is
	// the documented way to break an Arch system: a partial upgrade. -S alone
	// installs against what is already there.
	return "pacman", append([]string{"-S", "--noconfirm"}, names...), []string{"LC_ALL=C"}
}

func (pacmanManager) RemoveCommand(names []string, purge bool) (string, []string, []string) {
	flags := "-Rs"
	if purge {
		flags = "-Rns"
	}
	return "pacman", append([]string{flags, "--noconfirm"}, names...), []string{"LC_ALL=C"}
}

// -n is pacman's purge: the configuration files go with the package rather
// than being left behind as .pacsave.
func (pacmanManager) SupportsPurge() bool { return true }

func (pacmanManager) IndexAge() (time.Time, bool) {
	return newestMtime("/var/lib/pacman/sync")
}

// Arch gets no refresh button, and the reason is the same one that makes its
// upgrade a full -Syu: a database refreshed on its own is what turns the next
// `pacman -S` into a partial upgrade, which is the documented way to break an
// Arch system. Refreshing here is the upgrade, so the button that does it is
// the upgrade button.
func (pacmanManager) RefreshCommand() (string, []string, []string, bool) {
	return "", nil, nil, false
}

// --- apk (Alpine) ------------------------------------------------------

func (apkManager) ListInstalled(ctx context.Context) ([]InstalledPackage, error) {
	out, err := run(ctx, 45*time.Second, "apk", "list", "-I")
	if err != nil && strings.TrimSpace(out) == "" {
		return nil, err
	}
	packages := parseApkList(out)

	// apk's listing carries neither a description nor a size, and asking per
	// package would be two hundred subprocesses. Both answer for the whole
	// installed set in one call instead, so the cost is two rather than four
	// hundred — and a failure leaves the column blank rather than the page.
	if desc, err := run(ctx, 45*time.Second, "apk", "info", "-d"); err == nil {
		apply := parseApkBlocks(desc)
		for i := range packages {
			packages[i].Summary = firstLine(apply[packages[i].Name])
		}
	}
	if sizes, err := run(ctx, 45*time.Second, "apk", "info", "-s"); err == nil {
		apply := parseApkBlocks(sizes)
		for i := range packages {
			packages[i].Size = parseHumanSize(apply[packages[i].Name])
		}
	}

	// /etc/apk/world is the list of what somebody asked for; everything else
	// is a dependency apk worked out. It is a file rather than a command,
	// which is why apk is the one manager here whose explicit set costs
	// nothing.
	explicit := apkWorld()
	for i := range packages {
		packages[i].Explicit = explicit[packages[i].Name]
	}
	return packages, nil
}

// apkWorld reads the explicitly-installed set, from the host's copy when this
// is running in a container over it.
func apkWorld() map[string]bool {
	explicit := map[string]bool{}
	for _, base := range []string{"", "/host"} {
		b, err := os.ReadFile(base + "/etc/apk/world")
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(b), "\n") {
			// A world entry may carry a constraint: `nginx>1.24`.
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			if i := strings.IndexAny(line, "<>=~"); i > 0 {
				line = line[:i]
			}
			explicit[line] = true
		}
		break
	}
	return explicit
}

// parseApkList reads `nginx-1.24.0-r7 x86_64 {nginx} (BSD-2-Clause) [installed]`.
func parseApkList(out string) []InstalledPackage {
	packages := []InstalledPackage{}
	sc := bufio.NewScanner(strings.NewReader(out))
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		fields := strings.Fields(strings.TrimSpace(sc.Text()))
		if len(fields) < 2 {
			continue
		}
		name, version := splitApkVersion(fields[0])
		if name == "" {
			continue
		}
		p := InstalledPackage{Name: name, Version: version, Architecture: fields[1]}
		for _, f := range fields[2:] {
			if strings.HasPrefix(f, "{") {
				p.Section = strings.Trim(f, "{}")
			}
		}
		packages = append(packages, p)
	}
	return packages
}

// parseApkBlocks reads the `<name>-<version> <label>:` records `apk info`
// prints for a whole set at once, keyed by package name.
func parseApkBlocks(out string) map[string]string {
	values := map[string]string{}
	for _, block := range splitBlocks(out) {
		header, body, ok := strings.Cut(block, "\n")
		if !ok {
			continue
		}
		id, _, ok := strings.Cut(strings.TrimSpace(header), " ")
		if !ok || !strings.HasSuffix(strings.TrimSpace(header), ":") {
			continue
		}
		if name, _ := splitApkVersion(id); name != "" {
			values[name] = strings.TrimSpace(body)
		}
	}
	return values
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return strings.TrimSpace(line)
}

func (apkManager) Search(ctx context.Context, query string) ([]CatalogEntry, error) {
	out, err := run(ctx, 45*time.Second, "apk", "search", "-v", query)
	if err != nil && strings.TrimSpace(out) == "" {
		return nil, err
	}
	entries := parseApkSearch(out)
	// -d matches the description too.
	if len(entries) < thinSearch {
		if full, err := run(ctx, 45*time.Second, "apk", "search", "-v", "-d", query); err == nil {
			entries = widen(entries, parseApkSearch(full))
		}
	}
	return entries, nil
}

// parseApkSearch reads `nginx-1.24.0-r7 - High performance web server`, and
// tolerates the builds that print the two without the dash between them.
func parseApkSearch(out string) []CatalogEntry {
	entries := []CatalogEntry{}
	sc := bufio.NewScanner(strings.NewReader(out))
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		id, summary, ok := strings.Cut(line, " - ")
		if !ok {
			id, summary, _ = strings.Cut(line, " ")
		}
		name, version := splitApkVersion(strings.TrimSpace(id))
		if name == "" {
			continue
		}
		entries = append(entries, CatalogEntry{
			Name: name, Version: version, Summary: strings.TrimSpace(summary),
		})
	}
	return entries
}

func (apkManager) Info(ctx context.Context, name string) (*PackageDetail, error) {
	out, err := run(ctx, 30*time.Second, "apk", "info", "-a", name)
	if err != nil && strings.TrimSpace(out) == "" {
		return nil, err
	}
	if strings.TrimSpace(out) == "" {
		return nil, ErrUnknownPackage
	}
	detail := &PackageDetail{Name: name}
	for _, block := range splitBlocks(out) {
		header, body, ok := strings.Cut(block, "\n")
		if !ok {
			continue
		}
		header = strings.TrimSpace(header)
		id, label, ok := strings.Cut(header, " ")
		if !ok {
			continue
		}
		if n, v := splitApkVersion(id); n != "" {
			detail.Name, detail.Version = n, v
		}
		body = strings.TrimSpace(body)
		switch {
		case strings.HasPrefix(label, "description"):
			detail.Summary = firstLine(body)
			detail.Description = body
		case strings.HasPrefix(label, "webpage"):
			detail.Homepage = firstLine(body)
		case strings.HasPrefix(label, "installed size"):
			detail.Size = parseHumanSize(firstLine(body))
		case strings.HasPrefix(label, "depends on"):
			detail.Dependencies = strings.Fields(body)
		case strings.HasPrefix(label, "license"):
			detail.License = firstLine(body)
		}
	}
	return detail, nil
}

func (apkManager) Files(ctx context.Context, name string) ([]string, error) {
	out, err := run(ctx, 30*time.Second, "apk", "info", "-L", name)
	if err != nil && strings.TrimSpace(out) == "" {
		return nil, err
	}
	files := []string{}
	for _, line := range splitLines(out) {
		// apk prints paths with no leading slash, under a `<pkg> contains:`
		// heading — so a path is anything that is not the heading.
		if strings.HasSuffix(line, ":") {
			continue
		}
		files = append(files, "/"+strings.TrimPrefix(line, "/"))
	}
	return files, nil
}

func (apkManager) InstallCommand(names []string) (string, []string, []string) {
	return "apk", append([]string{"add"}, names...), []string{"LC_ALL=C"}
}

func (apkManager) RemoveCommand(names []string, purge bool) (string, []string, []string) {
	args := []string{"del"}
	if purge {
		args = append(args, "--purge")
	}
	return "apk", append(args, names...), []string{"LC_ALL=C"}
}

func (apkManager) SupportsPurge() bool { return true }

func (apkManager) IndexAge() (time.Time, bool) {
	return newestMtime("/var/cache/apk", "/etc/apk/cache")
}

func (apkManager) RefreshCommand() (string, []string, []string, bool) {
	return "apk", []string{"update"}, []string{"LC_ALL=C"}, true
}
