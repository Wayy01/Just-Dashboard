//go:build ignore

// Command gen-changelog renders CHANGELOG.md from the file the dashboard reads.
//
// There is one changelog in this repository and it is
// backend/internal/selfupdate/changelog.json: the copy compiled into the
// binary, the copy fetched over the network to find out whether a newer
// version exists, and — through this — the copy people read on GitHub. A
// hand-written CHANGELOG.md alongside it would be a second source of truth
// that drifts, and the one that drifts is always the one nobody is reading.
//
// It parses through internal/selfupdate rather than through its own JSON
// types, so the validation an operator's install applies to a published
// changelog is the same validation a release has to pass here first.
//
//	go run ./scripts/gen-changelog.go -expect 0.6 -out ../CHANGELOG.md
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/Wayy01/Just-Dashboard/backend/internal/selfupdate"
	"github.com/Wayy01/Just-Dashboard/backend/internal/version"
)

func main() {
	expect := flag.String("expect", "", "fail unless this is the newest release in the changelog")
	out := flag.String("out", "../CHANGELOG.md", "file to write")
	flag.Parse()

	m := selfupdate.Local()
	if *expect != "" && selfupdate.Compare(m.Latest, *expect) != 0 {
		fmt.Fprintf(os.Stderr,
			"The changelog's newest release is %s, but you are cutting %s.\n\n"+
				"Add the entry to %s first — newest anywhere in the array, it is sorted on read.\n"+
				"Something like:\n\n%s\n",
			m.Latest, *expect, selfupdate.ManifestPath, skeleton(*expect))
		os.Exit(1)
	}
	if err := os.WriteFile(*out, []byte(render(m)), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "write:", err)
		os.Exit(1)
	}
	fmt.Printf("wrote %s (%d releases, newest %s; this build is %s)\n",
		*out, len(m.Releases), m.Latest, version.Version)
}

// order is how the sections of a release are laid out. Security first,
// because it is the one that decides whether an operator upgrades today or
// next month; removals last, because they are the rarest.
var order = []struct {
	kind    selfupdate.Kind
	heading string
}{
	{selfupdate.Security, "Security"},
	{selfupdate.Added, "Added"},
	{selfupdate.Changed, "Changed"},
	{selfupdate.Fixed, "Fixed"},
	{selfupdate.Deprecated, "Deprecated"},
	{selfupdate.Removed, "Removed"},
}

func render(m selfupdate.Manifest) string {
	var b strings.Builder
	b.WriteString("# Changelog\n\n")
	b.WriteString("Every release of Just Dashboard, newest first.\n\n")
	b.WriteString("**This file is generated.** The source is [`" + selfupdate.ManifestPath + "`](" +
		selfupdate.ManifestPath + "), which is the same file the dashboard reads — both the copy " +
		"compiled into your build and the one it fetches to find out whether a newer version " +
		"exists. Edit that, then run `scripts/release.sh <version>`.\n\n")

	for _, r := range m.Releases {
		fmt.Fprintf(&b, "## %s — %s\n\n", r.Version, r.Day().Format("2 January 2006"))
		fmt.Fprintf(&b, "**%s**\n\n", r.Title)
		if s := strings.TrimSpace(r.Summary); s != "" {
			b.WriteString(s + "\n\n")
		}
		if r.Breaking {
			fmt.Fprintf(&b, "> **Needs attention before you upgrade.** %s\n\n",
				strings.TrimSpace(r.BreakingNote))
		}
		for _, section := range order {
			lines := []selfupdate.Change{}
			for _, c := range r.Changes {
				if c.Kind == section.kind {
					lines = append(lines, c)
				}
			}
			if len(lines) == 0 {
				continue
			}
			fmt.Fprintf(&b, "### %s\n\n", section.heading)
			for _, c := range lines {
				fmt.Fprintf(&b, "- %s\n", strings.TrimSpace(c.Text))
				if d := strings.TrimSpace(c.Detail); d != "" {
					fmt.Fprintf(&b, "  - %s\n", d)
				}
			}
			b.WriteString("\n")
		}
	}
	return b.String()
}

func skeleton(v string) string {
	return fmt.Sprintf(`    {
      "version": %q,
      "date": %q,
      "title": "Three or four words naming the release",
      "summary": "A sentence or two an operator can decide from.",
      "changes": [
        { "kind": "added", "text": "What they can now do", "detail": "Optional, where the consequence is not obvious." },
        { "kind": "fixed", "text": "What no longer goes wrong" }
      ]
    },`, v, time.Now().Format("2006-01-02"))
}
