# C0 review and evidence packet

- Gate: passed
- Date: 2026-09-02
- Baseline commit: `a436021` (0.6.6)
- Branch: `patch/0.6.7`
- Toolchain: Go 1.25.7 (`GOTOOLCHAIN=auto`), bun 1.4.0, Playwright 1.62.1
- Live tools observed, not exercised by this gate: Docker 29.7.2, Compose 5.5.0

This packet closes contracts and the upgrade foundation only. It does not claim that the persistent
engine, product UI, builders, activation, integrations, automation, blueprints, or release acceptance are
implemented. Their checkpoint gates remain open in `CHECKPOINTS.md`.

## Architecture and security authority review

| Repository invariant / decision | Single authority after C0 | Evidence |
| --- | --- | --- |
| Network allowlist before auth; mandatory 2FA | Existing route chain and auth service; no C0 route changed | `internal/api/routes.go`, full API suite |
| Capability, destructive budget, confirmation and audit | Frozen per proposed route/action; existing middleware remains the executor | `09-frozen-contracts.md` API/security matrix |
| Deployment path containment | `files.Service` scoped to non-empty `JD_DEPLOY_ROOTS` | `deploy/model.go`, `config.go`, symlink/empty-root tests |
| Host commands and working directory | `hostexec.CommandInDir`; explicit argv | ADR 0002, `hostexec.go`, local/host argv tests, legacy Git/Compose callers migrated |
| Shell exception | One immutable admin-authored release-task boundary, including migrated pre/post commands | ADR 0001/0002; transcript test rejects any other shell adapter |
| Feature ownership | Deploy orchestrates; Docker/Git/Proxy/Backups/etc. retain validation and execution | ADR 0001 and the recovery-responsibility matrix in `09-frozen-contracts.md` |
| Additive SQLite upgrades | `store.go` schema plus permanent `addedColumns`; transactional idempotent compatibility mapper | populated `0.6.6.sql` migration test, including foreign-key check and reopen |
| Browser safety journeys | Repository Playwright runner; Chromium required, other engines opt-in | ADR 0004, `playwright.config.ts`, harness test |

The route matrix contains no generic “admin later” cells: every read, mutation, hook, reveal, destructive
action and content-dependent privileged runtime field has a capability/session rule, confirmation policy,
and audit action. Closed Go vocabularies exhaust every run and step transition pair. Unknown writes are
therefore rejectable rather than silently becoming a new state.

## Restart and side-effect review

ADR 0001 assigns evidence and recovery for every step class before a worker exists: pure analysis,
source materialization, build/pull, non-idempotent release tasks, candidate start, checks, proxy cutover,
retirement, backups and notifications. The rule is fail/recover from owner evidence when ambiguity remains;
no interrupted non-idempotent operation is automatically replayed. The recorded fixtures cover Git,
Docker build, Compose activation, proxy application, HTTP readiness and TCP failure with exact boundaries,
argv/events and terminal evidence. C1 must replay these fakes while proving restart at each step.

## Upgrade fixture assertions

`TestOpenMigratesPopulated066DeploymentsIdempotently` starts from the shipped 0.6.6 table shapes populated
with accounts, audit, backup, metrics, two projects, sealed hook secrets, sealed variables, successful,
failed, rollback and running history. It proves:

- project/run ids and all legacy rows remain addressable;
- production environment, source and plan revision mapping is exact;
- ciphertext is copied byte-for-byte and still decrypts with the fixture master key;
- the legacy HMAC signature still verifies;
- successful history becomes ordered immutable release lineage and the prior rollback is live;
- legacy transcripts become sequenced events without deleting `deploy_runs.log`;
- unrelated feature rows and all foreign keys survive;
- reopening performs no duplicate or destructive work.

## Commands run

```text
cd backend
go build ./...
go vet ./...
go test ./... -count=1
PASS (all packages; live database families retained their documented skips)

cd ../frontend
bun run lint
bun run build
bun run test:browser
PASS (Chromium: 1/1 C0 harness test)

git diff --check
PASS
```

The C0 browser case intentionally proves runner discovery, frontend startup, Chromium wiring and
accessibility-facing locators only. It is not counted as a core deployment journey. C3 adds the required
live API/Docker journeys; C11 treats any required skip as an incomplete release.
