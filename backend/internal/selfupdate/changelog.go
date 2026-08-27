// Package selfupdate is how a Just Dashboard install learns that a newer one
// exists, shows what changed in it, and moves itself onto it.
//
// Everything else in this product manages something on the server. This
// manages the product, and the awkwardness of that is the whole design
// problem: the process being asked to install an update is one of the
// containers the update replaces, so it cannot watch its own replacement
// happen. The answer taken here is that the backend never runs the upgrade
// itself — it starts a sibling container that does, and reads the outcome back
// off disk once it has been restarted into the new version. install.go is that
// half; this file is the other one, which is where "what changed" comes from.
//
// The changelog is a **data file, not prose**. Markdown would have been less
// work to write and impossible to use: the dashboard has to compare versions,
// select the entries between the installed one and the newest one, and render
// them as a list of changes with a kind against each — none of which is a
// thing you can do to a paragraph without writing a parser and then trusting
// it against release notes nobody validated. changelog.json is validated by
// the test suite and read by exactly the same code on both sides of the
// network: embedded here, so an install always knows its own history without
// asking anyone; and fetched from the repository, so it also knows the history
// it does not have yet. One shape, one parser, one thing to edit for a
// release.
package selfupdate

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

// changelogJSON is the release history compiled into this build.
//
// It is embedded rather than read from disk because the answer must not depend
// on which files happened to be copied into the image — an install that cannot
// say what version it is and what that version contains is exactly the install
// whose operator is trying to work out whether to upgrade.
//
//go:embed changelog.json
var changelogJSON []byte

// ManifestPath is where the same file lives in the repository, which is what
// the check fetches. It is a path within the tree rather than a release asset
// on purpose: publishing a release becomes a commit, so there is no second
// ceremony to forget, and an install started from a fork or a branch reads
// whatever that fork or branch actually says.
const ManifestPath = "backend/internal/selfupdate/changelog.json"

// Kind classifies one line of a release. The set is closed because the UI
// paints each one differently and a release inventing a seventh kind should
// fail the test suite rather than render as an unstyled grey row.
type Kind string

const (
	Added      Kind = "added"
	Changed    Kind = "changed"
	Fixed      Kind = "fixed"
	Removed    Kind = "removed"
	Security   Kind = "security"
	Deprecated Kind = "deprecated"
)

var kinds = map[Kind]bool{
	Added: true, Changed: true, Fixed: true,
	Removed: true, Security: true, Deprecated: true,
}

// Valid reports whether k is one of the six.
func (k Kind) Valid() bool { return kinds[k] }

// Change is one line of a release.
type Change struct {
	Kind Kind   `json:"kind"`
	Text string `json:"text"`
	// Detail is the sentence under the line, for a change whose consequence is
	// not obvious from its title. Optional, and most changes do not have one —
	// release notes that explain every entry are read as thoroughly as a
	// licence agreement.
	Detail string `json:"detail,omitempty"`
}

// Release is one version.
type Release struct {
	Version string `json:"version"`
	// Date is a plain calendar day (2006-01-02), not a timestamp. Releases are
	// cut on a day, and an operator comparing "when did I install this" with a
	// changelog does not care about the hour.
	Date string `json:"date"`
	// Title names the release in a few words — "In-app updates" — so the
	// notification in the sidebar can say what is in the version rather than
	// only its number.
	Title string `json:"title"`
	// Summary is the paragraph at the top of the entry.
	Summary string   `json:"summary,omitempty"`
	Changes []Change `json:"changes"`
	// Breaking marks a release that needs the operator to do something by
	// hand. The UI refuses to hide it behind a one-click install.
	Breaking bool `json:"breaking,omitempty"`
	// BreakingNote says what has to be done. Required when Breaking is set;
	// the alternative is a warning triangle with nothing behind it.
	BreakingNote string `json:"breakingNote,omitempty"`
}

// Day parses Date, or returns the zero time when it is missing. Callers render
// the zero time as nothing rather than as 1 January year 1.
func (r Release) Day() time.Time {
	t, err := time.Parse("2006-01-02", r.Date)
	if err != nil {
		return time.Time{}
	}
	return t.UTC()
}

// Manifest is the whole file: every release this project has cut, newest
// first.
type Manifest struct {
	// Latest is the version at the head of Releases, repeated as its own field
	// so a client that only wants to know whether it is behind does not have
	// to reach into an array to find out.
	Latest   string    `json:"latest"`
	Releases []Release `json:"releases"`
}

// Local is the history this build was compiled with. It never fails: the file
// is embedded and the test suite parses it, so a manifest that does not parse
// cannot be committed.
func Local() Manifest {
	m, err := ParseManifest(changelogJSON)
	if err != nil {
		// Unreachable while TestLocalManifestIsValid passes, which is the
		// point of that test. Degrading to an empty history is still better
		// than refusing to boot the dashboard over its own release notes.
		return Manifest{Releases: []Release{}}
	}
	return m
}

// ParseManifest reads a changelog file and checks it makes sense.
//
// The validation is not defensive dressing: this same function parses the file
// fetched from the repository over the network, so "the manifest is
// well-formed" cannot be assumed from the fact that it was committed. A
// malformed remote file has to become an error the operator can read, not a
// version banner offering to install "".
func ParseManifest(b []byte) (Manifest, error) {
	var m Manifest
	dec := json.NewDecoder(strings.NewReader(string(b)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&m); err != nil {
		return Manifest{}, fmt.Errorf("changelog is not valid JSON: %w", err)
	}
	if len(m.Releases) == 0 {
		return Manifest{}, fmt.Errorf("changelog lists no releases")
	}
	seen := map[string]bool{}
	for i, r := range m.Releases {
		if !ValidVersion(r.Version) {
			return Manifest{}, fmt.Errorf("release %d: %q is not a version", i, r.Version)
		}
		if seen[r.Version] {
			return Manifest{}, fmt.Errorf("release %s appears twice", r.Version)
		}
		seen[r.Version] = true
		if strings.TrimSpace(r.Title) == "" {
			return Manifest{}, fmt.Errorf("release %s has no title", r.Version)
		}
		if _, err := time.Parse("2006-01-02", r.Date); err != nil {
			return Manifest{}, fmt.Errorf("release %s: date %q is not YYYY-MM-DD", r.Version, r.Date)
		}
		if len(r.Changes) == 0 {
			return Manifest{}, fmt.Errorf("release %s lists no changes", r.Version)
		}
		for j, c := range r.Changes {
			if !c.Kind.Valid() {
				return Manifest{}, fmt.Errorf("release %s change %d: %q is not one of added/changed/fixed/removed/security/deprecated", r.Version, j, c.Kind)
			}
			if strings.TrimSpace(c.Text) == "" {
				return Manifest{}, fmt.Errorf("release %s change %d has no text", r.Version, j)
			}
		}
		// A warning triangle with nothing behind it teaches the operator to
		// ignore warning triangles.
		if r.Breaking && strings.TrimSpace(r.BreakingNote) == "" {
			return Manifest{}, fmt.Errorf("release %s is marked breaking but says nothing about what to do", r.Version)
		}
	}
	// Sorting rather than demanding the file be sorted: the order of the array
	// is a detail of how it was edited, and a release inserted in the wrong
	// place should not change which version an install thinks is newest.
	sort.SliceStable(m.Releases, func(i, j int) bool {
		return Compare(m.Releases[i].Version, m.Releases[j].Version) > 0
	})
	head := m.Releases[0].Version
	if m.Latest == "" {
		m.Latest = head
	} else if Compare(m.Latest, head) != 0 {
		return Manifest{}, fmt.Errorf("changelog says latest is %s but its newest release is %s", m.Latest, head)
	}
	return m, nil
}

// Since returns the releases strictly newer than version, newest first.
//
// This is the list the "what you would be installing" panel shows, and it is
// deliberately every intervening release rather than only the newest one: an
// install three versions behind is upgrading past three sets of changes, and
// showing only the last would hide the two that explain what happened to it.
func (m Manifest) Since(version string) []Release {
	out := []Release{}
	for _, r := range m.Releases {
		if Compare(r.Version, version) > 0 {
			out = append(out, r)
		}
	}
	return out
}

// Find returns one release by version.
func (m Manifest) Find(version string) (Release, bool) {
	for _, r := range m.Releases {
		if Compare(r.Version, version) == 0 {
			return r, true
		}
	}
	return Release{}, false
}

// HasBreaking reports whether any release in the list needs manual work, which
// is what turns a one-click install into a "read this first".
func HasBreaking(releases []Release) bool {
	for _, r := range releases {
		if r.Breaking {
			return true
		}
	}
	return false
}
