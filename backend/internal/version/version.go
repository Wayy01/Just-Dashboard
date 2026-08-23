// Package version carries the one string that says which build of the
// dashboard this is.
//
// It lives in its own package rather than in cmd/server because the answer is
// wanted in two unrelated places — the line the server logs at boot, and
// whatever later needs to identify this install to something else — and a
// constant in main is not reachable from either.
package version

// Version is the product version. Bumping a release means editing this line
// and the matching constant in frontend/src/lib/version.ts; version_test.go
// fails the run if the two ever drift, so there is no third place to remember
// and no way to ship a UI claiming a version the server does not.
//
// It is a marketing version, not a semver contract: 0.5 is the dashboard as a
// complete single-server panel, 1.0 is when the API stops moving under people.
const Version = "0.5"
