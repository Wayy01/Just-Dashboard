package updates

import (
	"context"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/Wayy01/Just-Dashboard/backend/internal/hostexec"
)

// "It is installed. Now what?"
//
// That is the question every package page in this class leaves the operator
// with: a version, a size, a list of dependencies, and no answer to the only
// thing they wanted, which is what the software they just installed is called
// and how to start it. The answer is already on the machine — a package's own
// file list says which commands it puts on the path, which manual pages it
// ships, which service it registers and which file in /etc configures it — and
// nothing here has to be fetched, guessed or maintained as a table of
// hand-written blurbs that goes stale.
//
// **Nothing in here executes the package's own binaries**, and that is a rule
// rather than an omission. `foo --help` is the obvious way to get a usage
// summary and it means this route runs an arbitrary host binary as root on
// behalf of whoever asked — a route that only needs `read`. Manual pages and
// documentation are text files; rendering one costs nothing and cannot be
// turned into anything else.

// ManPage is one entry in the host's manual.
type ManPage struct {
	Name string `json:"name"`
	// Section is the manual's own volume number: 1 is a command, 5 is a file
	// format, 8 is a thing only root runs. It is the difference between the
	// page about the `passwd` command and the page about the passwd *file*.
	Section string `json:"section"`
	Path    string `json:"path"`
}

// Usage is what this package gives you and how to reach it.
type Usage struct {
	Package string `json:"package"`
	// Commands are the executables it puts on the path, which is the answer to
	// "what do I type" and is very often not the package's own name.
	Commands []string  `json:"commands,omitempty"`
	ManPages []ManPage `json:"manPages,omitempty"`
	// Services are the systemd units it registers, so "and how do I start it"
	// has an answer for the packages that are daemons.
	Services []string `json:"services,omitempty"`
	// ConfigFiles are what it put in /etc, which is where the next question
	// after "how do I start it" leads.
	ConfigFiles []string `json:"configFiles,omitempty"`
	// Docs are README and example files worth opening in the file manager.
	Docs []string `json:"docs,omitempty"`
	// Manual is the rendered text of the page below, when one could be read.
	Manual    string `json:"manual,omitempty"`
	ManualFor string `json:"manualFor,omitempty"`
	// ManUnavailable explains an empty Manual where a page exists: the `man`
	// binary is absent, which minimal cloud images and most containers are.
	// Saying so is the difference between "this package documents nothing" and
	// "nothing here could read it".
	ManUnavailable string `json:"manUnavailable,omitempty"`
	// Truncated says the manual was cut, so the page can point at the terminal
	// rather than implying the documentation simply ends there.
	Truncated bool `json:"truncated,omitempty"`
	// Empty is the honest answer for a library: no commands, no pages, no
	// units. The UI says "this is a library other packages use" rather than
	// rendering four empty headings.
	Empty bool `json:"empty"`
}

const (
	// maxManual bounds what one manual page can put in a JSON response. Real
	// pages run to a few tens of kilobytes; bash(1) is closer to four hundred,
	// and reading all of it in a web page is not what anybody is here for.
	maxManual = 96 * 1024
	// maxUsageList bounds each of the lists. A package like texlive-full owns
	// a hundred thousand files, and a UI listing every one of them is a list
	// nobody can read attached to a response nobody wants to download.
	maxUsageList = 60
)

// binDirs are the directories whose contents are on the path. A file counts as
// a command only when its *parent* is one of these — /usr/bin itself appears
// in every file list and is a directory, not a command.
var binDirs = map[string]bool{
	"/bin": true, "/sbin": true, "/usr/bin": true, "/usr/sbin": true,
	"/usr/local/bin": true, "/usr/local/sbin": true, "/usr/games": true,
}

// buildUsage reads a package's file list into the answer.
func buildUsage(ctx context.Context, name string, files []string) *Usage {
	u := &Usage{Package: name}
	commands := map[string]bool{}
	for _, file := range files {
		dir, base := path.Split(strings.TrimRight(file, "/"))
		dir = strings.TrimSuffix(dir, "/")
		if base == "" {
			continue
		}
		switch {
		case binDirs[dir]:
			commands[base] = true
		case isManPage(file):
			if page, ok := manPageOf(file); ok {
				u.ManPages = append(u.ManPages, page)
			}
		case strings.HasSuffix(file, ".service") || strings.HasSuffix(file, ".socket") ||
			strings.HasSuffix(file, ".timer"):
			if strings.Contains(dir, "/systemd/system") {
				u.Services = append(u.Services, base)
			}
		case strings.HasPrefix(file, "/etc/") && !strings.HasSuffix(file, "/"):
			u.ConfigFiles = append(u.ConfigFiles, file)
		case strings.HasPrefix(file, "/usr/share/doc/") && isDoc(base):
			u.Docs = append(u.Docs, file)
		}
	}
	for c := range commands {
		u.Commands = append(u.Commands, c)
	}
	sort.Strings(u.Commands)
	sort.Strings(u.Services)
	sort.Strings(u.ConfigFiles)
	u.ConfigFiles = dropDirectories(u.ConfigFiles)
	sort.Strings(u.Docs)
	sort.Slice(u.ManPages, func(i, j int) bool {
		if u.ManPages[i].Section != u.ManPages[j].Section {
			return u.ManPages[i].Section < u.ManPages[j].Section
		}
		return u.ManPages[i].Name < u.ManPages[j].Name
	})

	u.Commands = capList(u.Commands)
	u.Services = capList(u.Services)
	u.ConfigFiles = capList(u.ConfigFiles)
	u.Docs = capList(u.Docs)
	if len(u.ManPages) > maxUsageList {
		u.ManPages = u.ManPages[:maxUsageList]
	}

	u.Empty = len(u.Commands) == 0 && len(u.ManPages) == 0 &&
		len(u.Services) == 0 && len(u.ConfigFiles) == 0 && len(u.Docs) == 0

	if page, ok := primaryManPage(name, u); ok {
		u.ManualFor = page.Name
		if !hostexec.AvailableOnHost("man") {
			u.ManUnavailable = "the `man` command is not installed on this host, so the page below could not be rendered"
		} else {
			text, truncated := renderMan(ctx, page)
			u.Manual, u.Truncated = text, truncated
		}
	}
	return u
}

// dropDirectories removes the entries that are only there because something
// else is inside them.
//
// A package's file list names its directories the same way it names its files —
// `dpkg -L openssh-client` lists /etc/ssh, /etc/ssh/ssh_config and
// /etc/ssh/ssh_config.d — and "open /etc/ssh in the file manager" under a
// heading that says "configuration files" is a row that does not do what it
// says. A path that is the parent of the next one is a directory; the list is
// sorted, so that is a single pass.
func dropDirectories(paths []string) []string {
	out := paths[:0]
	for i, p := range paths {
		if i+1 < len(paths) && strings.HasPrefix(paths[i+1], p+"/") {
			continue
		}
		out = append(out, p)
	}
	return out
}

func capList(list []string) []string {
	if len(list) > maxUsageList {
		return list[:maxUsageList]
	}
	return list
}

// isManPage recognises a path inside the manual tree.
//
// It looks for a `manN` *component* rather than for the literal "/man/man",
// because a translated page lives under /usr/share/man/de/man1 and the
// straightforward test misses every one of them — which on a host with any
// locale support installed is most of the manual.
func isManPage(file string) bool {
	if strings.HasSuffix(file, "/") {
		return false
	}
	for _, part := range strings.Split(file, "/") {
		if section, ok := strings.CutPrefix(part, "man"); ok && len(section) == 1 {
			if section[0] >= '0' && section[0] <= '9' || section == "n" {
				return true
			}
		}
	}
	return false
}

// manPageOf reads /usr/share/man/man8/nginx.8.gz into its name and section.
//
// The compression suffix is stripped because `man nginx` is what somebody
// types; the section comes from the *filename* rather than the directory, so a
// page filed under man1 but named foo.8 keeps the section it declares.
func manPageOf(file string) (ManPage, bool) {
	base := path.Base(file)
	for _, ext := range []string{".gz", ".xz", ".bz2", ".zst", ".lzma"} {
		base = strings.TrimSuffix(base, ext)
	}
	name, section, ok := cutLast(base, ".")
	if !ok || name == "" || section == "" {
		return ManPage{}, false
	}
	// Localised pages live under /usr/share/man/<lang>/man1; the dashboard
	// renders the untranslated one, so a page whose path names a locale is
	// listed but never chosen as the primary.
	return ManPage{Name: name, Section: section, Path: file}, true
}

func cutLast(s, sep string) (string, string, bool) {
	i := strings.LastIndex(s, sep)
	if i < 0 {
		return s, "", false
	}
	return s[:i], s[i+len(sep):], true
}

// isDoc keeps the documentation worth opening and drops the packaging
// paperwork every Debian package carries — a changelog and a copyright file
// are not "how to use this".
func isDoc(base string) bool {
	lower := strings.ToLower(base)
	for _, prefix := range []string{"readme", "usage", "quickstart", "getting", "example", "tutorial", "howto"} {
		if strings.HasPrefix(lower, prefix) {
			return true
		}
	}
	return false
}

// primaryManPage picks the page to render.
//
// Ranked rather than "the first one", because the alphabetically first page a
// package owns is almost never the one to read: openssh-client ships scp
// before ssh, and coreutils ships `[` before everything. So a page named after
// the package wins, then one whose name is inside the package's (which is what
// makes ssh beat scp for openssh-client), then any page that is actually one
// of the commands on the path, then anything in section 1.
//
// Only untranslated pages are eligible: a localised copy under
// /usr/share/man/de renders in German for an operator who did not ask for it.
func primaryManPage(pkg string, u *Usage) (ManPage, bool) {
	commands := map[string]bool{}
	for _, c := range u.Commands {
		commands[c] = true
	}
	score := func(p ManPage) int {
		switch {
		case p.Name == pkg:
			return 0
		case len(p.Name) >= 3 && strings.Contains(pkg, p.Name):
			return 1
		// `[` is a command coreutils really does install, and a manual page
		// titled TEST(1) is not what somebody opening "how do I use coreutils"
		// is looking for.
		case commands[p.Name] && len(p.Name) >= 2 && isNameStart(p.Name[0]):
			return 2
		case strings.HasPrefix(p.Section, "1"):
			return 3
		default:
			return 4
		}
	}
	best, bestScore := ManPage{}, 99
	for _, p := range u.ManPages {
		if !strings.HasPrefix(p.Path, "/usr/share/man/man") {
			continue
		}
		if s := score(p); s < bestScore {
			best, bestScore = p, s
		}
	}
	return best, best.Name != ""
}

// renderMan formats one page as plain text.
//
// MANWIDTH pins the wrap so the output does not depend on whatever terminal
// width the container reports — 80, the width a man page's own two-column
// layout was designed against, because the panel it lands in is a side sheet
// rather than a terminal. `-P cat` keeps man from trying to page something
// with no terminal attached, which on a host whose PAGER is `less` is a
// command that never returns.
func renderMan(ctx context.Context, page ManPage) (string, bool) {
	runCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	cmd := hostexec.CommandOnHost(runCtx, "man", "-P", "cat", "--", page.Section, page.Name)
	cmd.Env = append(cmd.Environ(), "LC_ALL=C", "MANWIDTH=80", "MAN_KEEP_FORMATTING=", "PAGER=cat", "GROFF_NO_SGR=1")
	out, err := cmd.Output()
	if err != nil && len(out) == 0 {
		return "", false
	}
	text := stripOverstrike(string(out))
	if len(text) > maxManual {
		return text[:maxManual], true
	}
	return text, false
}

// stripOverstrike undoes nroff's bold and underline, which it produces by
// printing a character, backspacing over it and printing it again.
//
// The alternative is `col -b`, which lives in util-linux-extra and is exactly
// the package a minimal cloud image leaves out — the same absence that already
// costs this product the login records. Doing it here is four lines and works
// everywhere.
func stripOverstrike(s string) string {
	if !strings.ContainsRune(s, '\b') {
		return s
	}
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		if s[i] == '\b' {
			if len(out) > 0 {
				out = out[:len(out)-1]
			}
			continue
		}
		out = append(out, s[i])
	}
	return string(out)
}
