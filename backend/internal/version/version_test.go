package version

import (
	"encoding/json"
	"os"
	"regexp"
	"strings"
	"testing"
)

// The version is shown by the frontend and logged by the backend from two
// constants that nothing links at build time. This is the link: a release that
// bumps one and forgets the other fails here, rather than shipping a UI that
// claims a version the server has never heard of.
func TestVersionMatchesFrontend(t *testing.T) {
	const path = "../../../frontend/src/lib/version.ts"
	b, err := os.ReadFile(path)
	if err != nil {
		// The backend module builds on its own, and a checkout without the
		// frontend has nothing to disagree with.
		t.Skipf("no frontend to compare against: %v", err)
	}
	m := regexp.MustCompile(`VERSION\s*=\s*"([^"]+)"`).FindSubmatch(b)
	if m == nil {
		t.Fatalf("%s no longer declares VERSION as a string literal", path)
	}
	if got := string(m[1]); got != Version {
		t.Fatalf("frontend says %q, backend says %q — a release bumps both", got, Version)
	}
}

// package.json is the one other file carrying a version, because npm demands
// the field. It is checked loosely: the product version has two components and
// npm insists on three, so what must agree is the release, not the string.
func TestPackageVersionMatchesRelease(t *testing.T) {
	const path = "../../../frontend/package.json"
	b, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("no frontend to compare against: %v", err)
	}
	var pkg struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(b, &pkg); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	if pkg.Version != Version && !strings.HasPrefix(pkg.Version, Version+".") {
		t.Fatalf("package.json says %q, which is not a %s release", pkg.Version, Version)
	}
}
