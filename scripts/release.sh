#!/usr/bin/env bash
#
# Cut a release.
#
#   scripts/release.sh 0.6
#
# A version of Just Dashboard lives in four files, and every install in the
# world decides whether to offer itself an update by comparing two of them. So
# this exists to make bumping a release one action rather than four, and the Go
# test suite fails the build if any of them ever drift apart:
#
#   backend/internal/version/version.go          what the server logs and reports
#   frontend/src/lib/version.ts                  what the wordmark shows
#   frontend/package.json                        because npm demands the field
#   backend/internal/selfupdate/changelog.json   what the release *contains*
#
# The changelog is the one this cannot write for you, and it is deliberately
# first: a release with no release notes is a notification an operator cannot
# act on, and "write it afterwards" means never. Add the entry, then run this.
#
# CHANGELOG.md is generated from the same file, so nothing is written twice.

set -euo pipefail

cd "$(dirname "$0")/.."

if [ -t 1 ] && [ "$(tput colors 2>/dev/null || echo 0)" -ge 8 ]; then
	BOLD=$(tput bold); RESET=$(tput sgr0)
	GREEN=$(tput setaf 2); RED=$(tput setaf 1); DIM=$(tput dim)
else
	BOLD=""; RESET=""; GREEN=""; RED=""; DIM=""
fi
ok()  { printf '  %s✓%s %s\n' "$GREEN" "$RESET" "$*"; }
die() { printf '\n%serror:%s %s\n' "$RED" "$RESET" "$*" >&2; exit 1; }

VERSION="${1:-}"
[ -n "$VERSION" ] || die "usage: scripts/release.sh <version>   (e.g. scripts/release.sh 0.6)"
VERSION="${VERSION#v}"

# Two or three numeric components. The product version is a marketing version,
# not a semver contract — but it is compared numerically by every install, so
# anything this cannot compare must not ship.
[[ "$VERSION" =~ ^[0-9]+\.[0-9]+(\.[0-9]+)?$ ]] \
	|| die "\"$VERSION\" is not a version this scheme can compare; use 0.6 or 0.6.1"

CURRENT=$(sed -n 's/^const Version = "\(.*\)"$/\1/p' backend/internal/version/version.go)
[ -n "$CURRENT" ] || die "could not read the current version out of backend/internal/version/version.go"

printf '\n%sReleasing Just Dashboard %s → %s%s\n\n' "$BOLD" "$CURRENT" "$VERSION" "$RESET"

# The changelog is checked before anything is written, so a run that stops here
# leaves the tree exactly as it found it.
( cd backend && go run ./scripts/gen-changelog.go -expect "$VERSION" -out ../CHANGELOG.md ) \
	|| die "the changelog does not describe $VERSION yet — see above"
ok "CHANGELOG.md regenerated"

# npm wants three components and the product version usually has two. The
# release is what has to agree; the trailing zero is npm's problem.
NPM_VERSION="$VERSION"
[[ "$NPM_VERSION" =~ ^[0-9]+\.[0-9]+$ ]] && NPM_VERSION="$VERSION.0"

sed -i.bak "s/^const Version = \".*\"$/const Version = \"$VERSION\"/" \
	backend/internal/version/version.go
sed -i.bak "s/^export const VERSION = \".*\"$/export const VERSION = \"$VERSION\"/" \
	frontend/src/lib/version.ts
sed -i.bak "0,/\"version\": \".*\"/s//\"version\": \"$NPM_VERSION\"/" \
	frontend/package.json
rm -f backend/internal/version/version.go.bak frontend/src/lib/version.ts.bak frontend/package.json.bak
ok "version constants bumped"

# The tests are the enforcement, not this script: they are what fails on a
# release cut by hand, or by an agent that edited three files and forgot the
# fourth.
( cd backend && go test ./internal/version/ ./internal/selfupdate/ >/dev/null ) \
	|| die "the version tests do not pass; run: cd backend && go test ./internal/version/ ./internal/selfupdate/"
ok "version and changelog agree"

cat <<EOF

${BOLD}Released $VERSION locally.${RESET} What is left:

  ${DIM}# read the diff — four files and the generated changelog${RESET}
  git diff --stat

  ${DIM}# build both halves, as CONTRIBUTING requires${RESET}
  (cd backend && go build ./... && go test ./...)
  (cd frontend && bun run build)

  git commit -am "Release $VERSION"
  git push origin main

Every install polling this repository sees $VERSION within six hours of that
push, because the changelog on the tracked branch *is* the announcement.
EOF
