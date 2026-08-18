package updates

import "testing"

// The origin in parentheses is what marks a security update, so the parse has
// to survive real apt output rather than a tidied version of it.
func TestParseInstLine(t *testing.T) {
	cases := []struct {
		line     string
		name     string
		current  string
		cand     string
		security bool
	}{
		{
			line:     "Inst libssl3 [3.0.13-0ubuntu3.4] (3.0.13-0ubuntu3.5 Ubuntu:24.04/noble-security [amd64])",
			name:     "libssl3",
			current:  "3.0.13-0ubuntu3.4",
			cand:     "3.0.13-0ubuntu3.5",
			security: true,
		},
		{
			line:     "Inst docker-ce-cli [5:29.2.1-1~ubuntu.24.04~noble] (5:29.7.2-1~ubuntu.24.04~noble Docker CE:noble [amd64])",
			name:     "docker-ce-cli",
			current:  "5:29.2.1-1~ubuntu.24.04~noble",
			cand:     "5:29.7.2-1~ubuntu.24.04~noble",
			security: false,
		},
		{
			// A newly installed dependency has no current version.
			line:    "Inst newpkg (1.0 Ubuntu:24.04/noble [amd64])",
			name:    "newpkg",
			current: "",
			cand:    "1.0",
		},
	}
	for _, c := range cases {
		p, ok := parseInstLine(c.line)
		if !ok {
			t.Errorf("parseInstLine(%q) returned not-ok", c.line)
			continue
		}
		if p.Name != c.name || p.Current != c.current || p.Candidate != c.cand {
			t.Errorf("parseInstLine(%q) = %+v, want name=%q current=%q candidate=%q",
				c.line, p, c.name, c.current, c.cand)
		}
		if p.Security != c.security {
			t.Errorf("parseInstLine(%q) security = %v, want %v", c.line, p.Security, c.security)
		}
	}
}

func TestParseInstLineIgnoresOtherLines(t *testing.T) {
	for _, line := range []string{
		"", "Reading package lists...", "Conf libssl3 (3.0.13-0ubuntu3.5 Ubuntu:24.04/noble-security [amd64])",
		"The following packages will be upgraded:",
	} {
		if _, ok := parseInstLine(line); ok {
			t.Errorf("parseInstLine(%q) treated a non-Inst line as a package", line)
		}
	}
}
