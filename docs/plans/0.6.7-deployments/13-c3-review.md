# C3 fleet, creation wizard, project, and run UI review

- Status: passed
- Date: 2026-09-04
- Branch: `patch/0.6.7`
- Scope: delivery-roadmap checkpoint C3

## Implemented boundary

The deployment surface now has permanent fleet, creation, project, and run routes backed by the C0-C2
contracts. The fleet read model exposes the active queue, deployment summaries, last release state, and
honest unavailable health/pressure values until the owning later checkpoints provide observations. Its UI
supports search and normalized status/type/environment/pending filters, a compact desktop table, and
single-column responsive cards.

`/deploy/new` is a resumable, server-owned five-stage flow. It covers public or connected Git, contained
local checkouts, immutable images, ordered Compose paste/upload/Git/local inputs, imports, and an honest
unavailable blueprint option. Detection review preserves ambiguity and lets the user select a candidate;
contextual configuration exposes every normalized field, including Git subdirectory/submodule/LFS,
Compose ordering, domains, storage, health checks, and advanced runtime authority. Invalid advanced JSON
is visible and blocks preflight. Minecraft requires an explicit EULA choice. Preflight presents findings
and the persisted exact release path before its idempotent save. URL restoration permits the next stage
after the latest persisted stage instead of sending a resumed user backward.

The project workspace keeps identity, environment, release, health, and pending state visible while its
tabs join to the owning feature pages or state honestly that a later checkpoint has not supplied a
capability. Release history links permanently to run evidence. Legacy projects retain deploy, masked
environment, hook, commit, and confirmation-based rollback access. Normalized execution is explicitly
disabled until the C4-C5 executor exists, and the API rejects attempts to route normalized plans through
the legacy executor.

The permanent run page renders the persisted snapshot, actual release path, selectable step evidence, and
a bounded, sequence-deduplicated transcript. WebSocket reconnects resume after the latest received
sequence, terminal streams stop reconnecting, and REST snapshots remain usable through cancellation.
Cancel and retry are keyboard-operable; state changes use a separate polite announcement while the log
transcript itself is not a live region.

## Gate evidence

| C3 gate | Evidence |
| --- | --- |
| Public-repository novice journey | `deploy-ui.spec.ts` creates a Git URL draft, accepts automatic detection, and reaches reviewed preflight without opening Advanced |
| Minecraft novice journey | browser journey selects Minecraft, proves EULA starts unchecked, accepts it, and reaches reviewed preflight without opening Advanced |
| Every normalized expert field | wizard configuration renders source-specific Git/Compose controls plus all normalized runtime, domain, storage, check, variable, dependency, and advanced fields |
| Reload and navigation states | browser journey reloads queued, running, failed, and succeeded permanent run URLs; cancel and retry continue from the restored state |
| Resumable live evidence | browser journey disconnects after sequence 16, verifies the reconnect query contains `after=16`, and accepts sequence 17 without duplication |
| Responsive review | browser assertions and visual inspection at 375, 768, 1024, and 1440 px found no horizontal viewport overflow or obscured primary/error controls |
| Keyboard and screen-reader smoke | semantic browser interactions cover wizard errors/focus, run navigation, cancel, retry, and confirmed rollback; status announcements and transcript roles are asserted |
| Reduced motion | browser emulation verifies animation/transition duration is suppressed for the run workspace |
| Design-system consistency | existing cards, buttons, badges, inputs, tokens, focus treatments, and layout conventions are reused; deployment-specific release-path/status primitives are centralized in `deployment-ui.tsx` |
| Honest ownership boundaries | fleet/project health and later feature tabs say unavailable or link to the owner; normalized execution cannot fall through to the legacy engine |

## Verification record

Passed from `frontend/` against a fresh production build/server:

```text
bunx tsc --noEmit
bun run lint
bun run build
bunx playwright test tests/browser/deploy-ui.spec.ts
8 passed (17.5s)
```

The browser suite covers fleet responsive states, wizard error focus/touch targets, both novice plans,
project pending/history/rollback behavior, permanent run evidence, WebSocket resume, all terminal/loading
states, keyboard cancel/retry, and reduced motion.

Passed from `backend/`:

```text
go test -race ./internal/deploy ./internal/api ./internal/dockerx ./internal/files ./internal/safepath -count=1
go build ./...
go vet ./...
go test ./...
git diff --check
```

The final full backend suite passed all packages. Focused API tests cover the fleet/project/run read models,
selected detection candidate validation, and refusal to execute a normalized plan with the legacy engine.

## Explicit later-checkpoint work

C3 presents persisted plans and execution evidence but does not fabricate build or runtime capability.
Automatic/Dockerfile/static/image/Compose release artifacts and normalized run execution begin at C4;
activation, readiness, cutover, and runtime health begin at C5. Metrics, configuration ownership, feature
joins, automation, and blueprint catalog data stay with C6-C9 and are linked or reported unavailable until
their implementation checkpoints.
