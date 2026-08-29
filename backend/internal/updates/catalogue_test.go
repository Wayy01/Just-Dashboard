package updates

import (
	"strings"
	"testing"
)

// The fixtures below are copied from the tools themselves rather than written
// from memory, for the reason the firewall parsers' are: a host that happens
// to run one of these six is no way to check the other five, and the exact
// shape of the output — a status abbreviation, an architecture suffix, which
// column the summary is in — is the thing most likely to be wrong.

func TestParseDpkgListKeepsOnlyWhatIsInstalled(t *testing.T) {
	// `rc` is removed-but-configured and `un` is known-and-absent. Listing
	// either reports software the machine does not have, complete with a
	// version, which is the worst kind of wrong for an inventory.
	out := strings.Join([]string{
		"ii \tnginx\t1.24.0-2ubuntu7\tamd64\t1720\tno\thttpd\tsmall, powerful, scalable web/proxy server",
		"rc \told-thing\t1.0-1\tamd64\t40\tno\tmisc\tremoved but not purged",
		"un \tnever-here\t\t\t\tno\t\t",
		"ii \tbash\t5.2.21-2ubuntu4\tamd64\t1876\tyes\tshells\tGNU Bourne Again SHell",
	}, "\n")

	got := parseDpkgList(out)
	if len(got) != 2 {
		t.Fatalf("got %d packages, want 2: %+v", len(got), got)
	}
	if got[0].Name != "nginx" || got[0].Summary != "small, powerful, scalable web/proxy server" {
		t.Errorf("first package parsed wrong: %+v", got[0])
	}
	// dpkg reports kibibytes; a size column showing 1720 bytes for nginx is a
	// number nobody can act on.
	if got[0].Size != 1720*1024 {
		t.Errorf("size = %d, want %d", got[0].Size, 1720*1024)
	}
	if got[0].Essential {
		t.Error("nginx is not essential")
	}
	if !got[1].Essential {
		t.Error("bash is marked Essential: yes and must be refused a removal")
	}
}

func TestParseAptShowReadsOnlyTheFirstStanza(t *testing.T) {
	// apt-cache prints one stanza per available version, newest first.
	// Merging them produces a package whose homepage came from oldstable.
	out := `Package: htop
Version: 3.3.0-4build1
Installed-Size: 391
Depends: libc6 (>= 2.38), libncursesw6 (>= 6), libtinfo6 (>= 6)
Homepage: https://htop.dev/
Section: utils
Description: interactive processes viewer
 Htop is an ncursed-based process viewer similar to top, but
 it allows to scroll the list vertically and horizontally to
 see all processes and their full command lines.
 .
 Users can also interactively kill processes.

Package: htop
Version: 3.2.2-2
Homepage: https://old.example/
Description: an older one
`
	detail := parseAptShow(out)
	if detail == nil {
		t.Fatal("no detail parsed")
	}
	if detail.Version != "3.3.0-4build1" {
		t.Errorf("version = %q, want the first stanza's", detail.Version)
	}
	if detail.Homepage != "https://htop.dev/" {
		t.Errorf("homepage = %q — the second stanza leaked in", detail.Homepage)
	}
	if detail.Size != 391*1024 {
		t.Errorf("size = %d, want %d", detail.Size, 391*1024)
	}
	if detail.Summary != "interactive processes viewer" {
		t.Errorf("summary = %q", detail.Summary)
	}
	// A lone "." is a paragraph break in a Debian description, not a sentence.
	if strings.Contains(detail.Description, "\n.\n") {
		t.Errorf("the paragraph marker was kept as text: %q", detail.Description)
	}
	if !strings.Contains(detail.Description, "interactively kill processes") {
		t.Errorf("the description stopped early: %q", detail.Description)
	}
	// Version constraints are noise in a list somebody is reading.
	want := []string{"libc6", "libncursesw6", "libtinfo6"}
	if len(detail.Dependencies) != len(want) {
		t.Fatalf("dependencies = %v", detail.Dependencies)
	}
	for i, w := range want {
		if detail.Dependencies[i] != w {
			t.Errorf("dependency %d = %q, want %q", i, detail.Dependencies[i], w)
		}
	}
}

func TestParseAptPolicyTakesTheCandidate(t *testing.T) {
	out := `nginx:
  Installed: (none)
  Candidate: 1.24.0-2ubuntu7
  Version table:
     1.24.0-2ubuntu7 500
htop:
  Installed: 3.3.0-4build1
  Candidate: 3.3.0-4build1
never-packaged:
  Installed: (none)
  Candidate: (none)
`
	got := parseAptPolicy(out)
	if got["nginx"] != "1.24.0-2ubuntu7" {
		t.Errorf("nginx candidate = %q", got["nginx"])
	}
	if got["htop"] != "3.3.0-4build1" {
		t.Errorf("htop candidate = %q", got["htop"])
	}
	// "(none)" is not a version, and rendering it as one puts the string in a
	// version column.
	if v, ok := got["never-packaged"]; ok {
		t.Errorf("a candidate of (none) was kept as %q", v)
	}
}

func TestParseDNFSearchStripsTheArchButNotADottedName(t *testing.T) {
	// The suffix is dnf's, not part of the name: `dnf install nginx.x86_64`
	// works but reads as a filename everywhere else in this product. Cutting
	// at the last dot regardless would turn python3.11 into python3.
	out := `================== Name Exactly Matched: nginx ===================
nginx.x86_64 : A high performance web server and reverse proxy server
=============== Name & Summary Matched: nginx ====================
python3.11.x86_64 : Version 3.11 of the Python interpreter
nginx-mod-mail.x86_64 : Nginx mail modules
`
	got := parseDNFSearch(out)
	if len(got) != 3 {
		t.Fatalf("got %d entries: %+v", len(got), got)
	}
	if got[0].Name != "nginx" {
		t.Errorf("name = %q, want nginx", got[0].Name)
	}
	if got[1].Name != "python3.11" {
		t.Errorf("name = %q — a dotted package name was cut at the wrong dot", got[1].Name)
	}
	if got[0].Summary != "A high performance web server and reverse proxy server" {
		t.Errorf("summary = %q", got[0].Summary)
	}
}

func TestParseKeyedInfoReadsDNFAndZypperAlike(t *testing.T) {
	dnf := `Available Packages
Name         : nginx
Version      : 1.22.1
Release      : 4.el9
Architecture : x86_64
Size         : 39 k
Repository   : appstream
Summary      : A high performance web server
URL          : https://nginx.org
License      : BSD
Description  : Nginx is a web server and a reverse proxy server
             : for HTTP, SMTP, POP3 and IMAP protocols.
`
	fields := parseKeyedInfo(dnf)
	detail := detailFromKeyed(fields, "nginx")
	if detail.Version != "1.22.1-4.el9" {
		t.Errorf("version = %q, want the version and release joined", detail.Version)
	}
	if detail.Homepage != "https://nginx.org" {
		t.Errorf("dnf's URL did not become the homepage: %q", detail.Homepage)
	}
	if !strings.Contains(detail.Description, "IMAP protocols") {
		t.Errorf("the wrapped description line was dropped: %q", detail.Description)
	}
	if detail.Size != 39*1024 {
		t.Errorf("size = %d, want %d", detail.Size, 39*1024)
	}

	// zypper spells three of the same facts differently — Arch for
	// Architecture, Upstream URL for URL — which is exactly why the reader
	// matches on the label rather than the position.
	zypper := `Information for package nginx:
------------------------------
Repository     : Main Repository
Name           : nginx
Version        : 1.21.5-1.1
Arch           : x86_64
Installed Size : 1.2 MiB
Installed      : No
Upstream URL   : https://nginx.org/
Summary        : A HTTP server and reverse proxy
Description    :
    nginx is a web server.
`
	detail = detailFromKeyed(parseKeyedInfo(zypper), "nginx")
	if detail.Architecture != "x86_64" {
		t.Errorf("Arch was not read as the architecture: %q", detail.Architecture)
	}
	if detail.Homepage != "https://nginx.org/" {
		t.Errorf("Upstream URL was not read as the homepage: %q", detail.Homepage)
	}
	if detail.Size != 1258291 {
		t.Errorf("size = %d", detail.Size)
	}
}

func TestParseZypperSearchKeepsOnlyPackages(t *testing.T) {
	// The same listing carries patterns and source packages, none of which is
	// a thing to install from here.
	out := `S  | Name          | Summary                          | Type
---+---------------+----------------------------------+--------
i+ | nginx         | A HTTP server and reverse proxy   | package
   | nginx-source  | Source files for nginx            | srcpackage
   | devel_basis   | Basis package for development     | pattern
   | nginx-doc     | Documentation for nginx           | package
`
	got := parseZypperSearch(out)
	if len(got) != 2 {
		t.Fatalf("got %d entries: %+v", len(got), got)
	}
	if got[0].Name != "nginx" || got[1].Name != "nginx-doc" {
		t.Errorf("entries = %+v", got)
	}
}

func TestParsePacmanSearchReadsTheIndentedDescription(t *testing.T) {
	out := `extra/nginx 1.27.0-1 [installed]
    Lightweight HTTP server and IMAP/POP3 proxy server
extra/nginx-mainline 1.27.1-1
    Lightweight HTTP server, mainline branch
`
	got := parsePacmanSearch(out)
	if len(got) != 2 {
		t.Fatalf("got %d entries: %+v", len(got), got)
	}
	if got[0].Name != "nginx" || got[0].Repository != "extra" || got[0].Version != "1.27.0-1" {
		t.Errorf("entry = %+v", got[0])
	}
	if got[0].Summary != "Lightweight HTTP server and IMAP/POP3 proxy server" {
		t.Errorf("summary = %q", got[0].Summary)
	}
}

func TestParsePacmanInfoBlocksReadsTheInstallReason(t *testing.T) {
	// "Install Reason" is pacman's apt-mark, and it is what makes a list of
	// twelve hundred packages readable.
	out := `Name            : nginx
Version         : 1.27.0-1
Description     : Lightweight HTTP server
Architecture    : x86_64
Installed Size  : 2.13 MiB
Install Reason  : Explicitly installed

Name            : libxml2
Version         : 2.12.7-1
Description     : XML parsing library
Architecture    : x86_64
Installed Size  : 6.00 MiB
Install Reason  : Installed as a dependency for another package
`
	got := parsePacmanInfoBlocks(out)
	if len(got) != 2 {
		t.Fatalf("got %d packages: %+v", len(got), got)
	}
	if !got[0].Explicit {
		t.Error("nginx was explicitly installed")
	}
	if got[1].Explicit {
		t.Error("libxml2 came along as a dependency")
	}
	if got[0].Size != 2233466 {
		t.Errorf("size = %d", got[0].Size)
	}
}

func TestParseApkListAndSearch(t *testing.T) {
	list := `busybox-1.36.1-r29 x86_64 {busybox} (GPL-2.0-only) [installed]
nginx-1.26.2-r0 x86_64 {nginx} (BSD-2-Clause) [installed]
`
	got := parseApkList(list)
	if len(got) != 2 {
		t.Fatalf("got %d packages: %+v", len(got), got)
	}
	// The version begins at the first dash followed by a digit; splitting on
	// the last one leaves "nginx-1.26.2" as the name.
	if got[1].Name != "nginx" || got[1].Version != "1.26.2-r0" {
		t.Errorf("entry = %+v", got[1])
	}
	if got[0].Section != "busybox" {
		t.Errorf("origin = %q", got[0].Section)
	}

	entries := parseApkSearch("nginx-1.26.2-r0 - High performance web server\nnginx-doc-1.26.2-r0 - Documentation\n")
	if len(entries) != 2 || entries[0].Name != "nginx" || entries[0].Summary != "High performance web server" {
		t.Errorf("search entries = %+v", entries)
	}
}

func TestParseApkBlocksKeysByPackageName(t *testing.T) {
	out := `busybox-1.36.1-r29 description:
Size optimized toolbox of many common UNIX utilities

nginx-1.26.2-r0 description:
HTTP and reverse proxy server

`
	got := parseApkBlocks(out)
	if got["nginx"] != "HTTP and reverse proxy server" {
		t.Errorf("nginx description = %q", got["nginx"])
	}
	if got["busybox"] == "" {
		t.Error("busybox description was dropped")
	}
}

func TestRankResultsPutsWhatWasTypedFirst(t *testing.T) {
	// A repository search is a substring match over thousands of names, so
	// the exact one lands somewhere in the middle. Without this the search
	// box for "git" answers with git-annex-remote-rclone.
	entries := []CatalogEntry{
		{Name: "git-annex"}, {Name: "libgit2-dev"}, {Name: "git"}, {Name: "gitk"},
		{Name: "tig", Summary: "text interface for git"},
	}
	rankResults(entries, "git")
	want := []string{"git", "gitk", "git-annex", "libgit2-dev", "tig"}
	for i, w := range want {
		if entries[i].Name != w {
			t.Fatalf("position %d = %q, want %q (full order %+v)", i, entries[i].Name, w, entries)
		}
	}
}

// The description bucket needs its own tie-break: everything in it matched a
// summary rather than a name, so ordering on name length puts the shortest
// unrelated package at the top of a search for what the software *does*.
func TestRankResultsOrdersDescriptionMatchesByWhatTheySay(t *testing.T) {
	entries := []CatalogEntry{
		{Name: "nd", Summary: "small command line tool for WebDAV"},
		{Name: "h2o", Summary: "optimized HTTP/1, HTTP/2 server"},
		{Name: "nginx", Summary: "small, powerful, scalable web/proxy server"},
		{Name: "lighttpd", Summary: "fast webserver with minimal memory footprint"},
	}
	rankResults(entries, "web server")
	// nginx and lighttpd carry both words; h2o carries only "server" and nd
	// only "web", and neither is what somebody typing this wanted.
	if entries[0].Name != "nginx" || entries[1].Name != "lighttpd" {
		t.Errorf("order = %s, %s, %s, %s",
			entries[0].Name, entries[1].Name, entries[2].Name, entries[3].Name)
	}
}

func TestValidateNameRefusesWhatIsNotAPackageName(t *testing.T) {
	// These go into an argv and never through a shell, so quoting is not the
	// worry — being read as a flag or a path to a package file is.
	for _, bad := range []string{
		"", "--reinstall", "-y", "/tmp/evil.deb", "./thing", "nginx foo",
		"nginx;reboot", "nginx\nrm", strings.Repeat("a", 200),
	} {
		if err := validateName(bad); err == nil {
			t.Errorf("%q was accepted as a package name", bad)
		}
	}
	for _, good := range []string{"nginx", "python3.11", "lib32-glibc", "g++", "libc6:i386", "gcc-13"} {
		if err := validateName(good); err != nil {
			t.Errorf("%q was refused: %v", good, err)
		}
	}
}

func TestCleanNamesDeduplicatesAndBounds(t *testing.T) {
	got, err := cleanNames([]string{"nginx", "htop", "nginx"})
	if err != nil {
		t.Fatalf("cleanNames: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("got %v, want the duplicate dropped", got)
	}
	if _, err := cleanNames(nil); err == nil {
		t.Error("an empty removal was accepted")
	}
	many := make([]string, 60)
	for i := range many {
		many[i] = "pkg"
	}
	if _, err := cleanNames(many); err == nil {
		t.Error("an unbounded batch was accepted")
	}
}

func TestProtectedRefusesOnlyTheCertainOnes(t *testing.T) {
	// Narrow on purpose, in the spirit of guardSSHLockout: a guard that
	// second-guessed every risky removal is one nobody can work with.
	for _, name := range []string{"apt", "dpkg", "systemd", "bash", "libc6", "openssh-server", "docker-ce", "linux-image-6.8.0-45-generic"} {
		if protectedReason(name, false) == "" {
			t.Errorf("%s can be removed, which ends the machine or the way into it", name)
		}
	}
	for _, name := range []string{"nginx", "htop", "postgresql-16", "vim", "curl"} {
		if reason := protectedReason(name, false); reason != "" {
			t.Errorf("%s was refused (%s) — removing it is an ordinary thing to want", name, reason)
		}
	}
	// dpkg's own Essential flag is authority enough on its own.
	if protectedReason("something-obscure", true) == "" {
		t.Error("a package the database marks essential must still be refused")
	}
}

func TestBuildUsageReadsAFileListIntoAnAnswer(t *testing.T) {
	// The point of the whole feature: the command is very often not the
	// package's name, and this is where that is answered.
	files := []string{
		"/usr", "/usr/bin", "/usr/bin/psql", "/usr/bin/pg_dump",
		"/usr/lib/postgresql/16/bin/initdb",
		"/usr/share/man/man1/psql.1.gz",
		"/usr/share/man/man1/pg_dump.1.gz",
		"/usr/share/man/de/man1/psql.1.gz",
		"/lib/systemd/system/postgresql.service",
		"/etc/postgresql-common/createcluster.conf",
		"/usr/share/doc/postgresql-client-16/README.Debian",
		"/usr/share/doc/postgresql-client-16/changelog.gz",
	}
	u := buildUsage(t.Context(), "postgresql-client-16", files)

	// /usr/bin is a directory in the same list; only its children are
	// commands, and a binary buried in /usr/lib is not on anybody's path.
	if len(u.Commands) != 2 || u.Commands[0] != "pg_dump" || u.Commands[1] != "psql" {
		t.Errorf("commands = %v", u.Commands)
	}
	if len(u.Services) != 1 || u.Services[0] != "postgresql.service" {
		t.Errorf("services = %v", u.Services)
	}
	if len(u.ConfigFiles) != 1 {
		t.Errorf("config files = %v", u.ConfigFiles)
	}
	// A package's file list names its directories exactly as it names its
	// files, and "/etc/ssh" under a heading saying "configuration files" is a
	// row that does not do what it says.
	dirs := buildUsage(t.Context(), "openssh-client", []string{
		"/etc/ssh", "/etc/ssh/ssh_config", "/etc/ssh/ssh_config.d", "/etc/ssh/ssh_config.d/50-cloud.conf",
	})
	if len(dirs.ConfigFiles) != 2 {
		t.Errorf("config files = %v, want the two directories dropped", dirs.ConfigFiles)
	}
	// A changelog is packaging paperwork, not "how to use this".
	if len(u.Docs) != 1 || !strings.HasSuffix(u.Docs[0], "README.Debian") {
		t.Errorf("docs = %v", u.Docs)
	}
	if len(u.ManPages) != 3 {
		t.Fatalf("man pages = %+v", u.ManPages)
	}
	if u.ManPages[0].Name != "pg_dump" || u.ManPages[0].Section != "1" {
		t.Errorf("man page = %+v", u.ManPages[0])
	}
	if u.Empty {
		t.Error("a package with commands and a unit is not empty")
	}

	// A library ships none of it, and saying so is better than four empty
	// headings.
	lib := buildUsage(t.Context(), "libssl3", []string{"/usr/lib/x86_64-linux-gnu/libssl.so.3"})
	if !lib.Empty {
		t.Errorf("a library reported usage: %+v", lib)
	}
}

func TestPrimaryManPagePrefersTheUntranslatedPageForTheCommand(t *testing.T) {
	u := &Usage{
		Commands: []string{"psql"},
		ManPages: []ManPage{
			{Name: "psql", Section: "1", Path: "/usr/share/man/de/man1/psql.1.gz"},
			{Name: "pg_dumpall", Section: "1", Path: "/usr/share/man/man1/pg_dumpall.1.gz"},
			{Name: "psql", Section: "1", Path: "/usr/share/man/man1/psql.1.gz"},
		},
	}
	page, ok := primaryManPage("postgresql-client-16", u)
	if !ok {
		t.Fatal("no page chosen")
	}
	if page.Path != "/usr/share/man/man1/psql.1.gz" {
		t.Errorf("chose %q — the localised page or the wrong command", page.Path)
	}
}

func TestStripOverstrikeUndoesNroffBold(t *testing.T) {
	// nroff writes bold by printing a character, backspacing and printing it
	// again; without this every heading reads as "NNAAMMEE".
	if got := stripOverstrike("N\bNA\bAM\bME\bE"); got != "NAME" {
		t.Errorf("got %q, want NAME", got)
	}
	// Underline is the same trick with an underscore in front.
	if got := stripOverstrike("_\bf_\bi_\bl_\be"); got != "file" {
		t.Errorf("got %q, want file", got)
	}
	if got := stripOverstrike("plain text"); got != "plain text" {
		t.Errorf("untouched text was changed to %q", got)
	}
}

func TestParseHumanSizeRefusesToInventANumber(t *testing.T) {
	cases := map[string]int64{
		"39 k":     39 * 1024,
		"1.2 MiB":  1258291,
		"2.13 MiB": 2233466,
		"1024":     1024,
		"":         0,
		"unknown":  0,
	}
	for in, want := range cases {
		if got := parseHumanSize(in); got != want {
			t.Errorf("parseHumanSize(%q) = %d, want %d", in, got, want)
		}
	}
}
