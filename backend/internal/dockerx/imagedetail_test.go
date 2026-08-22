package dockerx

import (
	"errors"
	"testing"
)

// The update check is a claim about somebody's server, and the two ways it can
// be wrong are both bad: telling an operator their image is current when it is
// not, and telling them a registry wants credentials when the truth is that
// they built the image themselves.

func TestIsBareReference(t *testing.T) {
	// A bare name resolves to docker.io/library/<name>, a namespace nobody can
	// have a private image in — so a refusal there means "no such image".
	for _, bare := range []string{"my-app:1", "jdsmoke-built:test", "nginx:alpine", "postgres"} {
		if !isBareReference(bare) {
			t.Errorf("%q is a bare reference", bare)
		}
	}
	// These genuinely could be private, and must keep the credentials message.
	for _, hosted := range []string{"me/app:1", "ghcr.io/org/app:1", "registry:5000/app:v1"} {
		if isBareReference(hosted) {
			t.Errorf("%q is namespaced or hosted and could legitimately be private", hosted)
		}
	}
}

func TestRegistryDeniedOrMissing(t *testing.T) {
	// Docker Hub answers 401 for a repository that does not exist, so both
	// spellings have to count as "not here".
	for _, msg := range []string{
		"error response from daemon: unauthorized: authentication required",
		"manifest unknown",
		"repository does not exist",
	} {
		if !registryDeniedOrMissing(errors.New(msg)) {
			t.Errorf("%q means the registry does not have it", msg)
		}
	}
	for _, msg := range []string{"dial tcp: i/o timeout", "toomanyrequests: rate limit exceeded"} {
		if registryDeniedOrMissing(errors.New(msg)) {
			t.Errorf("%q is the registry being unreachable, not a missing image", msg)
		}
	}
}

func TestRegistryReasonIsActionable(t *testing.T) {
	cases := map[string]string{
		"unauthorized: authentication required": "credentials",
		"toomanyrequests: rate limit exceeded":  "rate-limiting",
		"dial tcp 1.2.3.4:443: i/o timeout":     "could not be reached",
		"manifest unknown: manifest unknown":    "no longer has this tag",
	}
	for in, want := range cases {
		if got := registryReason(errors.New(in)); !contains(got, want) {
			t.Errorf("registryReason(%q) = %q, want it to mention %q", in, got, want)
		}
	}
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (haystack == needle || indexOf(haystack, needle) >= 0)
}

func indexOf(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}
