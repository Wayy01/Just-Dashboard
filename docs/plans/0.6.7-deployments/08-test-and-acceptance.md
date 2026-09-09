# Test, verification, and acceptance plan

Passing the current build is necessary but cannot prove this deployment system. Verification is layered
so pure logic stays fast, system boundaries use recorded and live fixtures, and user journeys prove the
contracts are actually connected.

## Test layers

### 1. Pure unit and property tests

New or expanded backend packages cover:

- state transition table, retry linkage, terminal states and evidence aggregation;
- queue ordering, host/project concurrency, leases, superseding and idempotency;
- log sequencing, retention, redaction and snapshot/resync;
- source/ref/image/variable/port/check validation;
- watch-path ordering and negation;
- builder detection evidence/confidence and deterministic plan rendering;
- variable reference parsing, scope, cycle detection and masked preview;
- blueprint schema/rendering and update diff;
- release comparison and activation eligibility;
- diagnosis claims and intentional non-findings;
- webhook provider event classification and delivery replay;
- schedule parsing, next-run calculation and chain validation.

Property/fuzz targets include branch/ref names, dotenv input, reference expressions, webhook JSON, Compose
metadata, archive/import layout, log redaction and blueprint documents. Bounds are asserted, not inferred
from tests merely finishing.

### 2. Store and migration tests

Create a version-controlled 0.6.6 schema fixture containing:

- two deploy projects (enabled/disabled hook), encrypted environment values and secrets;
- successful, failed and rollback histories;
- unrelated users/sessions/tokens/audit, backups, metrics and watched domains;
- one dirty local checkout relationship represented in fixture metadata.

Open it with the new store and prove:

- additive migration succeeds and is idempotent;
- each project maps to one production environment and compatible source/plan;
- secret plaintext still decrypts only through authorized paths;
- legacy hook ID/secret and enabled state work;
- run history/order/duration and metric event annotations remain correct;
- foreign keys and deletion/archive behavior match the ownership plan;
- new NOT NULL columns have defaults and a second open makes no changes.

Store race tests cover two workers claiming a run, lease expiry, cancellation versus completion and
webhook idempotency.

### 3. Recorded boundary transcripts

Hermetic tests drive adapters through recorded argv/stdin/stdout/stderr/exit transcripts for:

- Git success, auth refusal, moved ref, LFS/submodule missing, dirty managed checkout and malicious ref;
- Docker build/pull/inspect/health, tag-to-digest resolution and architecture mismatch;
- Compose validate/config/up/down with multiple files and partial failure;
- HTTP/TCP/Docker/command checks across timeout, retry and recovery;
- proxy render/parse/validate/apply/restore;
- provider webhook variants and version APIs;
- backup gate success/failure/timeout.

Tests assert exact argv, working directory, owner/environment filtering and that secret values do not
occur in captured process metadata.

### 4. Live Docker integration suites

Marked live suites skip with an explicit reason when Docker/BuildKit/Compose is unavailable. Release CI
or the documented release host must run them, not skip them.

Fixtures:

- tiny HTTP service with delayed readiness and graceful shutdown;
- deliberately failing build;
- service that exits 137 under a memory limit;
- HTTP candidate that starts but fails its public smoke test;
- fixed-port service that proves stop-first behavior;
- stateful service writing a sentinel into a named volume;
- two-service Compose stack with one missing/unhealthy service;
- static site;
- tag registry fixture where one tag moves between two digests;
- Minecraft Java/Bedrock minimal supported fixtures or a documented release-host matrix when image size
  makes them unsuitable for ordinary CI.

Prove build isolation, candidate health gating, cutover, rollback digest, graceful drain, volume
preservation, cleanup retention, ownership labels and import round-trip.

### 5. Signed-in API security tests

Extend whole-route tests with real admin, limited, readonly, API-token and unauthenticated principals.
For every route/action prove:

- network allowlist precedes authentication;
- partial 2FA session reaches no deployment route;
- correct capability is required server-side;
- advanced spec content receives stronger authorization;
- destructive routes use `s.destructive` and correct ordinary/typed confirmation;
- every mutation records a successful/failed audit entry without secrets;
- session-only routes reject API tokens where applicable;
- rate/destructive budgets apply;
- WebSocket origin and confirmation semantics are correct;
- webhook routes expose no project enumeration signal and verify bounded raw bodies.

Maintain a generated route-action matrix test so a new handler cannot be added without a declared
security disposition.

### 6. Browser journeys

If C0 approves Playwright, run it against a disposable real backend/Docker host. Otherwise implement an
equivalent repository-owned browser harness; manual-only coverage is not sufficient for core journeys.

Required journeys:

1. Public Git web app: paste URL -> detection -> configure domain/variables -> preflight -> deploy ->
   delayed readiness -> open live site.
2. Private Git authentication refusal and successful connected-repository selection.
3. Docker image and pasted multi-service Compose creation.
4. Import an existing container and stack; reject/acknowledge unsupported fields before adoption.
5. Save variables/domain and observe Pending deployment; deploy and see the diff clear.
6. Disconnect/reload mid-build and resume the transcript without starting a second run.
7. Cancel queued and running work; retry failed run.
8. Failed candidate leaves prior URL healthy; activate prior release through rollback.
9. Provider webhook burst, watch-path ignore, automatic run and preview open/update/close.
10. Stateful update blocked by failed backup, then succeeds after backup.
11. Minecraft new server with EULA, console, files, schedule, backup, update and software rollback.
12. Readonly/limited users see only actions they can perform; direct requests still fail server-side.
13. Docker/proxy/metrics/backup degraded-module states explain what is unavailable.

### 7. Accessibility and responsive verification

Automated accessibility scans run on fleet, every wizard step, project overview, run in each state, a
validation failure, a destructive confirmation and Minecraft console/schedule views.

Manual checks in dark/light and reduced-motion modes:

- keyboard order, visible focus and no focus loss after live updates;
- modal/sheet focus trap and return;
- screen-reader names/states for release path, queue, tabs, status, errors, icon actions and console;
- phase change announcements without reading every log line;
- 200% text zoom and browser zoom;
- 375, 768, 1024 and 1440 px, long content and empty/loading/error/unavailable states;
- touch targets and no hover-only actions;
- contrast and non-color status distinctions.

### 8. Failure drills

Each drill records expected state, external evidence and recovery:

| Injection | Required result |
| --- | --- |
| Browser/tunnel disconnect | Run continues; reconnect resumes by sequence. |
| Backend restart while queued | Queue order and idempotency survive. |
| Backend restart during build | Lease reconciles; build resumes safely or fails with cleanup, never activates twice. |
| Restart during proxy cutover | External route evidence selects/repairs exactly one live release. |
| Git/provider outage | Old release remains; retry guidance names the source failure. |
| Registry outage | Pull/build fails before replacement; retained digest rollback still works. |
| DNS/certbot unavailable | Internal service may run; public route stays unverified and never claims healthy. |
| Readiness never passes | Timeout, candidate cleanup and old-release continuity. |
| Public smoke check fails | Proxy restored and previous URL verified. |
| Release migration fails | Candidate never activates; old release remains. |
| Graceful stop hangs | Bounded escalation and evidence; no infinite run. |
| Required backup fails | Deployment stays blocked before stop/change. |
| Disk fills during build/logging | Bounded failure, diagnostic, old release and database integrity. |
| Log floods/line exceeds limit | Truncation is explicit; process and dashboard memory remain bounded. |
| Cleanup races a deploy | Lease prevents artifact/cache deletion needed by active run. |
| Secret appears in process output | Redaction test fails release; stored/streamed copies contain mask only. |
| Provider delivery replay | One accepted run, replay recorded/rejected. |
| Malicious archive/Compose path | No write/read outside allowed root. |
| Minecraft update fails | Prior software returns against unchanged world; no automatic data restore. |

## Performance and capacity budgets

Reference scale: 100 deployments, 3 environments each, 10,000 retained runs, 100 active/queued steps,
50 containers, 1,000,000 retained log lines after compaction, and a repository with 100,000 files bounded
by detection limits.

- Fleet API p95 under 500 ms on the reference host without N+1 container inspect or subprocess calls.
- Fleet first useful render under 1.5 s on local/private-network conditions after shell load; loading state
  appears without layout shift.
- Run-event append-to-visible p95 under 250 ms for ordinary output; batching prevents render per line.
- Reconnect from a recent sequence under 1 s for 5,000 retained lines.
- Detection default wall time under 5 s, with explicit truncated/bounded result when limits are reached.
- Preflight default wall time under 10 s excluding optional network checks; each slower check streams its
  status and has an individual timeout.
- A 10,000-line transcript scrolls smoothly through the chosen rendering strategy and browser find/search
  remains available.
- Memory and database growth conform to documented line/run/artifact retention; load tests assert caps.

Budgets are measured on a documented reference host and are not silently relaxed to make CI green.

## Feature acceptance matrix

Each entry type must prove Configure, Preflight, Deploy, Observe, Change, Fail, Recover, Archive, and Import
or Export where applicable:

| Entry type | Mandatory recovery proof |
| --- | --- |
| Auto-detected Git app | Failed next build leaves current release; known image rollback. |
| Dockerfile | No-cache rebuild and recorded digest rollback. |
| Static site | Bad output/public check leaves prior site. |
| Docker image | Moving tag creates new release; rollback uses old digest. |
| Compose | Partial service failure is not success; stop-first recovery retains volumes. |
| Blueprint database/tool | Generated secrets sealed; storage/backup explicit; export portable. |
| Imported workload | Adoption changes nothing before confirmation; archive ownership correct. |
| Minecraft | Backup-gated update; software rollback preserves world. |

## Release command gates

Baseline repository gates:

```bash
cd backend
go build ./...
go vet ./...
go test ./...

cd ../frontend
bun run lint
bun run build
bun run test:browser:install # once per machine/cache
bun run test:browser
```

The implementation adds named commands/scripts for migration fixture, adapter integration, live Docker,
browser and blueprint validation suites. C0 must place those commands in `docs/internal/overview.md` and CONTRIBUTING so a
green release cannot mean the new suites were never run.

The release evidence packet records command, commit, environment/tool versions, pass/fail/skip counts,
browser matrix, live Docker/Compose/BuildKit versions, and failure-drill results. A skipped required live
suite is an incomplete release, not a pass.
