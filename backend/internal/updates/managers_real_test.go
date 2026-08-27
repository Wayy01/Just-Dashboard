package updates

import (
	"strings"
	"testing"
)

// Every fixture below is output copied from the tool itself, running in a
// container of the distribution it belongs to. A parser written against a
// remembered format is the commonest way this feature is wrong on a machine
// nobody developing it has.

// Rocky Linux 9, `dnf -q --cacheonly check-update`. Note the epoch on openssl
// and the noarch packages: both are ordinary and both have broken parsers.
const rockyCheckUpdate = `
coreutils-single.x86_64                8.32-41.el9_8                      baseos
curl-minimal.x86_64                    7.76.1-40.el9_8.5                  baseos
openssl.x86_64                         1:3.5.5-6.el9_8                    baseos
rocky-release.noarch                   9.8-1.2.el9                        baseos
systemd-libs.x86_64                    252-67.el9_8.4.rocky.0.1           baseos

Obsoleting Packages
grub2-tools.x86_64                     2.06-104.el9_8                     baseos
    grub2-tools.x86_64                 2.06-95.el9_6                      @baseos
`

func TestParseDNFCheckUpdateAgainstRockyOutput(t *testing.T) {
	packages := parseDNFCheckUpdate(rockyCheckUpdate)
	want := []string{"coreutils-single", "curl-minimal", "openssl", "rocky-release", "systemd-libs", "grub2-tools"}
	if len(packages) != len(want) {
		t.Fatalf("%d packages, want %d: %+v", len(packages), len(want), packages)
	}
	for i, name := range want {
		if packages[i].Name != name {
			t.Errorf("packages[%d].Name = %q, want %q", i, packages[i].Name, name)
		}
	}
	if packages[2].Candidate != "1:3.5.5-6.el9_8" {
		t.Errorf("epoch dropped from the candidate: %q", packages[2].Candidate)
	}
	if packages[3].Architecture != "noarch" {
		t.Errorf("arch = %q, want noarch", packages[3].Architecture)
	}
	// The indented line under "Obsoleting Packages" is the package being
	// replaced, which is going away rather than being upgraded.
	for _, p := range packages {
		if p.Origin == "@baseos" {
			t.Errorf("counted the obsoleted half of an obsoletion: %+v", p)
		}
	}
}

// Rocky Linux 9, `dnf -q --cacheonly updateinfo list --security`.
const rockySecurity = `RLSA-2026:28911 Moderate/Sec.  coreutils-single-8.32-41.el9_8.x86_64
RLSA-2026:55439 Important/Sec. curl-minimal-7.76.1-40.el9_8.5.x86_64
RLSA-2026:20597 Moderate/Sec.  glibc-common-2.34-270.el9_8.x86_64
RLSA-2026:52674 Moderate/Sec.  libarchive-3.5.3-11.el9_8.x86_64
`

func TestParseDNFSecurityAgainstRockyOutput(t *testing.T) {
	names := parseDNFSecurity(rockySecurity)
	for _, want := range []string{"coreutils-single", "curl-minimal", "glibc-common", "libarchive"} {
		if !names[want] {
			t.Errorf("%q not recovered from its NEVRA: %v", want, names)
		}
	}
}

// dnf5 rejects `--security` before the subcommand, in as many words, so the
// order here is not cosmetic: it is the difference between "install security
// updates" working and failing on every Fedora from 41 on.
func TestDNFUpgradePutsTheCommandFirst(t *testing.T) {
	_, args, _ := dnfManager{binary: "dnf"}.UpgradeCommand(true)
	if len(args) == 0 || args[0] != "upgrade" {
		t.Fatalf("args = %v, want the subcommand first", args)
	}
	if strings.Join(args, " ") != "upgrade -y --security" {
		t.Fatalf("args = %v", args)
	}
	_, plain, _ := dnfManager{binary: "dnf"}.UpgradeCommand(false)
	if strings.Join(plain, " ") != "upgrade -y" {
		t.Fatalf("args = %v", plain)
	}
}

// Alpine 3.19, `apk version -l '<'`. The header is two columns and every row
// glues the version onto the name.
const alpineVersions = `Installed:                                Available:
busybox-1.36.1-r20                      < 1.36.1-r21 
musl-1.2.4_git20230717-r5               < 1.2.4_git20230717-r6 
ssl_client-1.36.1-r20                   < 1.36.1-r21 
`

func TestParseApkAgainstAlpineOutput(t *testing.T) {
	packages := parseApkVersions(alpineVersions)
	if len(packages) != 3 {
		t.Fatalf("%d packages, want 3: %+v", len(packages), packages)
	}
	if packages[1].Name != "musl" || packages[1].Current != "1.2.4_git20230717-r5" {
		t.Errorf("a version with an underscore and a date split wrongly: %+v", packages[1])
	}
	if packages[2].Name != "ssl_client" {
		t.Errorf("a name with an underscore split wrongly: %+v", packages[2])
	}
}

// Arch, `pacman -Qu`.
const archUpdates = `iana-etc 20260530-1 -> 20260617-1
libcap-ng 0.9.3-1 -> 0.9.5-1
openssl 3.6.3-1 -> 3.6.4-1
`

func TestParsePacmanAgainstArchOutput(t *testing.T) {
	packages := parsePacmanUpdates(archUpdates)
	if len(packages) != 3 {
		t.Fatalf("%d packages, want 3: %+v", len(packages), packages)
	}
	if packages[2].Name != "openssl" || packages[2].Candidate != "3.6.4-1" {
		t.Errorf("row misread: %+v", packages[2])
	}
}

// openSUSE Leap, `zypper list-updates`. The column order is named by the
// heading row rather than assumed, and the leading "v" is what marks a row.
const zypperUpdates = `S  | Repository          | Name       | Current Version | Available Version | Arch
---+---------------------+------------+-----------------+-------------------+-------
v  | Main Update Repository | libopenssl1_1 | 1.1.1l-150500.17.34.1 | 1.1.1l-150500.17.37.1 | x86_64
v  | Main Update Repository | vim        | 9.0.1234-150500.1 | 9.0.2103-150500.1 | x86_64
`

func TestParseZypperAgainstLeapOutput(t *testing.T) {
	packages := parseZypperUpdates(zypperUpdates)
	if len(packages) != 2 {
		t.Fatalf("%d packages, want 2: %+v", len(packages), packages)
	}
	if packages[0].Name != "libopenssl1_1" || packages[0].Candidate != "1.1.1l-150500.17.37.1" {
		t.Errorf("row misread: %+v", packages[0])
	}
	if packages[1].Architecture != "x86_64" {
		t.Errorf("arch = %q", packages[1].Architecture)
	}
}

// zypper's own manual calls 100 through 106 informational — an update is
// available, a repository was skipped. Read as failures they report an error
// and no packages on a host with one stale repository, which is most of them.
func TestZypperInformationalExits(t *testing.T) {
	for _, code := range []int{100, 103, 106} {
		if !zypperInformational(code) {
			t.Errorf("%d read as a failure", code)
		}
	}
	for _, code := range []int{1, 4, 99, 107} {
		if zypperInformational(code) {
			t.Errorf("%d read as informational", code)
		}
	}
}

// A real dnf failure — the message a host whose metadata cache has been
// cleared prints — must not be mistaken for an empty package list.
func TestDNFErrorSurfacesTheToolsOwnWords(t *testing.T) {
	err := dnfError("Error: Cache-only enabled but no cache for 'baseos'\n", errStub{})
	if !strings.Contains(err.Error(), "no cache for 'baseos'") {
		t.Fatalf("err = %v, want dnf's own sentence", err)
	}
}

type errStub struct{}

func (errStub) Error() string { return "exit status 1" }
