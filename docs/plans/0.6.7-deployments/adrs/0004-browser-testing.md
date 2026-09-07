# ADR 0004: Browser journey test harness

- Status: accepted
- Date: 2026-09-02
- Release: 0.6.7

## Context

The current frontend gate is lint plus a production build. That cannot prove a multi-step form, reload of
a queued run, focus restoration, capability-specific controls, responsive transformations, or the
candidate/rollback safety journey. Manual-only coverage is explicitly insufficient in the acceptance plan.

The frontend already declares Playwright 1.62 as an Apache-2.0 development dependency but has no harness.

## Decision

Use the Playwright test runner at the already-pinned Playwright version. Tests live under
`frontend/tests/browser`, configuration lives at `frontend/playwright.config.ts`, and the named gate is
`bun run test:browser`. A separate `test:browser:install` command installs the Chromium binary for local
and CI environments that do not cache it.

The harness starts a disposable backend and frontend against a temporary SQLite directory and a dedicated
Docker fixture namespace. Tests may mock external providers and DNS at their network boundary, but core
deploy journeys use the real API, store, queue, and Docker daemon. Every fixture carries a unique prefix
and cleanup is ownership-label based; volumes are never swept globally.

Projects are split into `chromium` for the required release gate and opt-in Firefox/WebKit projects for
cross-browser evidence. Responsive cases use explicit 375, 768, 1024, and 1440 pixel viewports. Axe-based
scanning may be added after its separate dependency review; the initial harness combines semantic
assertions with Playwright's accessibility-facing locators and the manual screen-reader checklist.

Traces and screenshots are retained on failure. Browser binaries and result artifacts remain untracked.
Required live journeys skip only when the command is explicitly invoked in a non-live diagnostic mode;
the release evidence command treats any required skip as failure.

## Consequences

- `CLAUDE.md` and `CONTRIBUTING.md` list the browser gate separately from lint/build.
- Browser tests are slower and require a browser plus Docker for the full suite, so pure logic stays in Go
  and TypeScript-level tests rather than being moved into Playwright.
- The release gate gains evidence for navigation/reconnect and accessibility behaviors that compilation
  cannot cover.

