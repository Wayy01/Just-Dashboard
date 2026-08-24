/**
 * The product version, in one place.
 *
 * Bumping a release means editing this line and the matching constant in
 * `backend/internal/version/version.go` — nothing else. Every place the
 * version is shown reads it from here, and `TestVersionMatchesFrontend`
 * fails the Go test run if the two sides ever drift apart, so a release is
 * two one-line edits rather than a hunt through the tree.
 *
 * It is a marketing version, not a semver contract: 0.5 is the dashboard as
 * a complete single-server panel, 1.0 is when the API stops moving under
 * people. Add features, bump to 0.6.
 */
export const VERSION = "0.5"
