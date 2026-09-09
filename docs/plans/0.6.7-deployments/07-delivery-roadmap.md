# Delivery roadmap and checkpoint gates

This roadmap is ordered by risk and dependency, not by which screen is easiest to demo. A checkpoint is
closed only when its code, tests, documentation and recorded evidence are complete. Later checkpoints may
prototype against contracts, but they do not merge around an open prerequisite gate.

## Dependency graph

```text
C0 contracts and migration
 |
 +--> C1 persistent engine ----> C4 release artifacts ----> C5 safe activation
 |           |                          |                         |
 |           +--> C2 sources/build -----+                         |
 |           |                                                    |
 |           +--> C3 product UI ----------------------------------+
 |                                                                |
 +--> C6 configuration and feature joins -------------------------+
                 |                    |
                 +--> C7 automation --+--> C8 operations/diagnosis
                 |
                 +--> C9 blueprints and Minecraft

C1..C9 --> C10 hardening and failure drills --> C11 release/upgrade
```

## C0 — Freeze contracts, threats, and migration

### Build

- Write the final ADR for the orchestration boundary, execution namespace, canonical path resolver,
  persistent lease/reconciliation algorithm, activation strategy, and builder selection criteria.
- Freeze domain types, state transitions, step vocabulary, API schemas, stream events, capability map,
  audit action set, ownership modes, retention defaults and error codes.
- Produce additive SQLite DDL and the exact 0.6.6 migration mapping.
- Create recorded command transcripts/fakes for Git, Docker/Compose, proxy checks, HTTP/TCP checks and
  builder adapters before orchestration code depends on them.
- Decide whether Playwright is added for safety-critical frontend journeys; document dependency/license
  review and update `docs/internal/overview.md` if the project test workflow changes.
- Run the builder spike (Nixpacks vs Pack vs project recipes) and record the decision; do not install all
  candidates into production.

### Gate

- Architecture review proves every `docs/internal/security/invariants.md` invariant still has one implementation authority.
- A capability/confirmation matrix covers every proposed route and action.
- Migration test design contains populated legacy rows, sealed variables, hooks and run history.
- Failure-mode review covers restart during every non-idempotent step.
- No implementation table or route is left as “figure out later.”

## C1 — Persistent run engine, queue, logs, and reconciliation

### Build

- Add run/step/log/lease persistence and closed transition functions.
- Enqueue before returning `202`; support per-environment serialization and host concurrency budgets.
- Add sequence-based bounded stream with snapshot/resync behavior.
- Add cancellation, queued-run superseding and process-group cleanup.
- Reconcile expired leases on startup from both database state and Docker/Git/proxy evidence.
- Keep current legacy deploy handlers working through a compatibility adapter while the new UI is absent.

### Gate

- Unit tests exhaust allowed and forbidden transitions, lease races, queue order, superseding and limits.
- Killing/restarting the backend at each fixture step yields one correct terminal/recoverable outcome and no
  duplicate activation.
- A disconnected/reconnected subscriber receives every retained event exactly once by sequence.
- A slow subscriber cannot stall execution; retention bounds database and memory growth.
- Cancellation terminates descendants and records cleanup evidence.

## C2 — Sources, detection, drafts, and preflight

### Build

- Add resumable server-side drafts and normalized plan versions.
- Implement Git URL/provider/local source, image, Compose paste/upload/local source and import discovery.
- Add contained mirrors/worktrees, registry credential reference, Compose merge/validation and digest
  resolution.
- Implement bounded repository detection with evidence/confidence and deterministic output.
- Implement preflight findings for tools, path containment, variables, ports, storage, host headroom,
  domains, checks and advanced runtime authorization.
- Render a secret-free exact action plan.

### Gate

- Fixtures cover every source type, monorepo ambiguity, submodule/LFS choice, private auth failure,
  incompatible image architecture, Compose interpolation and unsupported import fields.
- Re-running detection against the same source revision is deterministic.
- Malicious Git refs, URLs, archive paths, symlinks, image names, Compose paths and variable references fail
  closed.
- Preflight never changes Docker, proxy, firewall, source checkout or backup state.
- Secret values are absent from previews, logs, snapshots and audit details.

## C3 — Fleet, creation wizard, project and run UI

### Build

- Add `/deploy`, `/deploy/new`, `/deploy/{id}` and `/deploy/{id}/runs/{run}` against C0 contracts.
- Implement outcome-first source selection, detection review, contextual configuration and release-path
  preflight.
- Implement active queue strip, compact fleet table/cards, project tabs, pending-change markers, deep links
  and live resumable run output.
- Retain familiar terminology and legacy project access during migration.
- Apply error summary/inline errors, status announcements, keyboard support, focus restoration, touch targets,
  responsive transformation and reduced motion.

### Gate

- Guided novice journey reaches a safe deployable plan for a public web repository and Minecraft without
  opening Advanced.
- Expert journeys can configure every normalized plan field without leaving the flow.
- Browser reload/navigation during queued, running, failed and successful runs restores the right state.
- 375, 768, 1024 and 1440 px reviews have no viewport overflow or obscured primary/error controls.
- Keyboard-only and screen-reader smoke journeys cover wizard, run, cancel, retry and rollback.
- Existing design-system components/tokens are used; feature-specific primitives are justified centrally.

## C4 — Builders and immutable release artifacts

### Build

- Implement selected automatic builder plus Dockerfile, static, image and Compose adapters.
- Produce immutable image digests/config snapshots with provenance and variable digests.
- Add build cache policy, force-no-cache action, artifact retention and cleanup reference protection.
- Implement release/pre-deploy tasks as named timed gates with explicit variable scope.
- Convert a legacy Compose project run into the new release record without changing its deployed behavior.

### Gate

- Each adapter has hermetic unit fixtures and a live Docker build/deploy fixture.
- A failed source build cannot change live containers or routes.
- A tag moving between deployments creates distinct releases and rollback uses the recorded digest.
- Build/release-task secrets remain absent from layers, argv, output and release metadata where the chosen
  builder can guarantee it; unsupported guarantees block or warn explicitly.
- Artifact pruning keeps every configured rollback release and reports why space is retained.

## C5 — Readiness, activation, graceful shutdown, and rollback

### Build

- Add HTTP, TCP, Docker-health, command and public-route check runners with retries/timeouts/evidence.
- Implement health-gated candidate start and proxy cutover for eligible HTTP services.
- Implement honest stop-first orchestration for exclusive volume/fixed-port/game/Compose cases.
- Add graceful stop/drain and bounded automatic restoration of the previous release.
- Expose deployment strategy eligibility and expected downtime in preflight.
- Implement rollback/redeploy/restart/force-build action semantics over immutable releases.

### Gate

- Failure before activation leaves the old release and route unchanged.
- Failure during/after cutover restores the prior proxy spec and verifies it before reporting recovery.
- Stateful fixture is never run concurrently against an exclusive local volume by default.
- Health disabled, health unavailable, health warning and health passed remain distinct in API and UI.
- SIGTERM/drain/SIGKILL timing and cancellation are testable and bounded.
- Rollback under pressure takes ordinary confirmation and completes through the same activation machinery.

## C6 — Variables, domains, storage, backups, databases, and security joins

### Build

- Add scoped variables, bulk dotenv, typed references, generated secrets, rotation and pending-state diff.
- Link deployment domains to Proxy/DNS/TLS and ports to Docker/firewall inventory.
- Link persistent data to Docker storage, Backups and optional Databases entries.
- Add backup-before-deploy gate and restore-evidence display.
- Add ownership-aware import/adoption/archive/removal plans.
- Add security posture observations and audit/capability coverage.

### Gate

- Reference graph detects missing references and cycles; secret leaves stay masked.
- Saving any configuration changes a plan revision but not the live release; deploying clears only the
  applied diff.
- Domain conflict, DNS failure, certificate unavailability, port collision, public bind and firewall
  mismatch are individually visible and correctly gated.
- Required backup failure prevents the stateful deploy while leaving live runtime unchanged.
- Archiving does not delete runtime/data; removal preview names every managed target and confirmation type.
- Full signed-in route tests prove mutations cannot bypass capabilities, destructive budget, confirmation
  or audit.

## C7 — Provider automation, hooks, schedules, previews, and notifications

### Build

- Implement provider-specific raw-body verification and event/repository/ref/delivery validation.
- Preserve legacy HMAC hooks and add scoped generic deploy hooks/API idempotency.
- Add watch-path rules with simulator and webhook-only semantics.
- Add scheduled actions with next-run preview, history, timeout and chaining.
- Add preview environments with quotas, generated domains, inherited configuration rules, updates and
  cleanup on PR close.
- Add signed outbound notification webhooks and delivery history.

### Gate

- Replay, wrong repository, wrong branch, wrong event, wrong signature and oversized payload never enqueue.
- Duplicate provider delivery and client retry create one run.
- A burst of pushes supersedes only eligible queued runs; manual/rollback/production protection holds.
- Preview opens, updates, closes and is garbage-collected without touching production dependencies.
- Schedule timezone/cron next-run fixtures cover DST and invalid expressions.
- Notification failure cannot change deployment outcome and never leaks response or request secrets.

## C8 — Operational workspace and cross-feature diagnosis

### Build

- Add deployment-filtered runtime services, Logs, metrics comparison, Docker findings, domain/TLS, storage
  and backup summaries with direct deep links.
- Add conservative cross-feature diagnosis and recovery actions.
- Add release comparison covering source, image, commands, variables by name/digest, runtime plan,
  dependencies and checks.
- Add update-available and retained-artifact status.

### Gate

- Each summary reads from the feature owner; no second parser/renderer is introduced.
- Missing optional modules render unavailable evidence, never empty success.
- Diagnosis claims and intentional silences are pinned by pure-function tests.
- A release regression journey correlates activation with container restart/memory/host pressure evidence.
- Every failure card offers a valid next action or explicitly says which external action is required.

## C9 — Versioned blueprints and Minecraft

### Build

- Implement blueprint schema, parser, deterministic renderer, provenance, update diff and CI validation.
- Migrate existing Docker starting points into the one blueprint foundation.
- Ship the reviewed initial set and full Minecraft Java/Bedrock creation/import path.
- Add game console, supported player actions, safe known-property editing, schedules and backup/update
  workflow by composing existing modules.
- Add upstream version adapters with cache, checksum/provenance and honest unavailable states.

### Gate

- Every blueprint fixture renders and deploys its expected safe plan on a test host.
- No built-in exposes a database publicly, omits required persistent storage, or contains default secrets.
- Minecraft new/import/update/rollback journeys pass for the selected supported matrix.
- EULA acceptance is explicit and audited; no server starts before it.
- Backup failure prevents update; software rollback leaves world data untouched.
- Console cannot escape into a host shell and unsupported player protocol controls do not render.

## C10 — Hardening, performance, accessibility, and failure drills

### Build

- Complete threat-model review, fuzz/property tests, race tests and resource-bound tests.
- Run every documented failure drill, including process/host restart, disk full, registry/provider/DNS
  outage, proxy validation failure, secret redaction, log flood, slow client and cleanup race.
- Profile fleet listing, detection, stream, retention and metrics joins at realistic scale.
- Complete accessibility and responsive review in both themes and reduced-motion mode.
- Update the affected `docs/internal/` guides, README, API documentation, operator recovery docs and blueprint maintenance guide.

### Gate

- No Critical/High security finding remains; accepted lower risks have owner and rationale.
- Performance budgets in `08-test-and-acceptance.md` pass at reference scale.
- Automated and manual accessibility gates pass; no keyboard-only blocker remains.
- Upgrade and rollback rehearsals have captured evidence, not only unit-test assertions.
- All required checklist rows through C10 carry evidence.

## C11 — 0.6.7 release and upgrade proof

### Build

- Write user-facing changelog data first, including migration/recovery notes and any builder dependency.
- Run backend, frontend, integration, browser and live-host release gates.
- Exercise install, update from 0.6.6 with populated data, fresh install and module-degraded install.
- Generate `CHANGELOG.md` and bump version with `scripts/release.sh 0.6.7` only after the changelog entry
  exists.
- Commit the release on `patch/0.6.7`; no tag substitutes for the tracked release branch.

### Gate

- `go build ./...`, `go vet ./...`, `go test ./...`, `bun run lint` and `bun run build` pass with the
  required live/browser suites documented separately.
- Version files and changelog head agree through repository tests.
- Existing 0.6.6 project can deploy, reveal/rotate variables, receive its legacy hook and read history
  after upgrade.
- Fresh guided web and Minecraft deployments complete and recover from an injected failure.
- Release notes state what changed, what the operator must review, and the honest limits of zero downtime.

