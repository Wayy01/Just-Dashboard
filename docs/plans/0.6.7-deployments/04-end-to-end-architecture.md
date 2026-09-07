# End-to-end architecture

## Architectural shape

Deployments becomes an orchestration layer over existing feature owners. It owns desired deployment
configuration, immutable releases, runs, steps, triggers, and the relationship between them. It does not
own Docker internals, proxy rendering, backup transfer, Git workbench behavior, metrics sampling, or log
parsing.

```text
request / provider event / schedule
                  |
                  v
          validate + authorize
                  |
                  v
        persistent run + queue
                  |
           host slot acquired
                  |
                  v
     source -> build -> release task
                  |
          candidate runtime
                  |
       readiness + smoke checks
                  |
      activate route / stop old
                  |
     release record + integrations
```

## Domain model

| Entity | Purpose | Key invariants |
| --- | --- | --- |
| Deployment | Stable user-facing application/service/game identity | Does not identify one checkout or container. |
| Environment | Production, staging, or preview configuration | Name and slug unique within deployment; production protection explicit. |
| Source | Git, local checkout, image, Compose, blueprint, or imported resource | Credentials referenced, not copied; source identity immutable in a release. |
| Build plan | Detector output plus operator overrides | Evidence and confidence stored; rendered preview available. |
| Runtime plan | Normalized services, commands, resources, ports, storage and checks | Server authorizes privileged fields from content. |
| Variable | Plain or secret value with build/runtime scope | Secrets sealed; values absent from audit and ordinary reads. |
| Dependency | Domain, certificate, database, volume, backup, network, container or scheduled job | Relationship includes ownership: managed, linked, or observed. |
| Release | Immutable source/build/runtime/configuration snapshot and produced artifacts | A release never mutates after creation. |
| Run | One requested operation against an environment | Persistent before enqueue; idempotency key; one terminal outcome. |
| Step | Ordered unit of execution with status, timing and evidence | State transitions append events; retries explicit. |
| Artifact | Image digest, static bundle, rendered Compose/spec, backup or diagnostic report | Digest/provenance retained; secret-free metadata. |
| Trigger | Manual, provider webhook, generic hook, API, schedule, preview event or rollback | Scope, filter, last delivery and enabled state visible. |
| Check | Readiness, liveness, smoke, TCP, HTTP, command, domain or TLS assertion | “Skipped/unavailable” is distinct from pass. |
| Blueprint | Versioned reviewed input schema and normalized plan generator | Immutable version, provenance, minimum dashboard version, update notes. |

The existing `deploy_projects`, `deploy_env`, and `deploy_runs` records are migration inputs and remain
addressable. A 0.6.6 project migrates to one deployment, one `production` environment, one local source,
one Compose build/runtime plan, and compatible legacy hook. Its history remains visible.

## Persistent state machines

### Run states

```text
requested -> validating -> queued -> preparing -> running -> verifying -> activating
                                                            |             |
                                                            v             v
                                                         failed        succeeded

queued/running/verifying -> cancelling -> cancelled
activating -> failed_activation -> restoring_previous -> failed | rolled_back
```

The UI may group detailed states into Queued, Deploying, Verifying, Live, Failed, Cancelled, and Rolled
back. The API retains the detail needed for diagnosis.

Allowed transitions are a closed server-side table and are tested in both directions. A terminal run is
never made running again. Retry creates a new run linked to its predecessor.

### Step states

`pending`, `blocked`, `running`, `passed`, `warning`, `failed`, `skipped`, `cancelled`, `unavailable`.

Default step graph:

1. Resolve source
2. Acquire revision or image
3. Analyze and validate plan
4. Prepare build context
5. Build or pull immutable artifact
6. Render and validate runtime configuration
7. Run pre-release task/migration
8. Start candidate
9. Verify container/process readiness
10. Verify public route or declared smoke checks
11. Activate candidate
12. Drain and retire previous release
13. Record dependencies, metrics marker, retention and notifications

Steps 4–7 collapse or skip honestly for image/Compose paths. `skipped` includes a reason; it is not a
green check.

## Queue, execution, and restart reconciliation

- A run row and all planned step rows are committed before the API returns `202`.
- A unique idempotency key deduplicates provider delivery IDs and client retries.
- Per-environment serialization is strict. The default host concurrency is one build and two lightweight
  pull/restart operations, configurable within bounded values.
- Queued runs for the same branch may be superseded by a newer webhook run before work begins. Manual,
  rollback, and production runs are never silently superseded.
- Queue order, reason, requested time, and estimated blocker are visible.
- Execution derives from a process-independent claim/lease with heartbeat. On server start, expired leases
  are reconciled from persisted state and external evidence.
- Reconciliation never blindly re-runs a side effect. It inspects source checkout, image digest,
  candidate container labels, proxy route, and recorded step evidence, then resumes an idempotent step or
  fails with a recovery action.
- Child processes run in their own process group so cancellation reaches descendants. Cancellation has a
  bounded cleanup phase and records what may remain.
- Structured log chunks append during execution and carry sequence, step, stream, timestamp and redaction
  metadata. Retention compacts old chunks without deleting the run summary or terminal reason.
- The existing `internal/jobs` emitter/subscription contract should be extracted or adapted so deployment
  streams resume by sequence. Persistence and queue leases belong to Deployments; generic short-lived
  jobs keep their current bounded in-memory store unless a separate project changes it.

## Source and build adapters

Every adapter implements a closed interface: validate, resolve immutable identity, materialize into a
contained working directory, render plan, and clean temporary content.

### Git

- supports provider connection, URL with deploy credential, or managed local checkout;
- records remote identity, ref, full commit, subdirectory, submodule/LFS choices, and changed files;
- uses a dashboard-owned bare mirror/cache plus a release worktree so builds never mutate the operator's
  Git workbench checkout;
- runs as the intended owner where the materialized path is user-owned;
- refuses interactive credential prompts and makes host-key policy visible;
- verifies provider event repository, branch/ref, delivery ID, and signature before enqueue.

### Docker image

- resolves a tag to digest before release creation;
- keeps registry credential references sealed and scoped;
- separates Pull latest, Redeploy recorded digest, and Restart existing runtime;
- records platform/architecture and refuses an incompatible image before replacing anything.

### Compose

- accepts Git/local/upload/paste and multiple config files with explicit ordering;
- validates with Docker Compose before storage and before execution;
- interpolates an environment snapshot without persisting plaintext rendered secrets;
- records normalized service names, images/build contexts, health checks, ports, mounts and dependencies;
- adds deployment/release ownership labels without overwriting operator labels;
- retains a rendered secret-free plan and a digest of the effective secret-bearing input.

### Automatic builder and static site

Checkpoint 2 runs a controlled spike comparing Nixpacks, Cloud Native Buildpacks/Pack, and a small
project-owned detector/recipe layer. Selection criteria are reproducibility, image size, offline behavior,
supported architectures, update ownership, licensing, build-secret handling, cache control, and ability
to preview exact commands. No builder is added as an unreviewed dependency.

The detector emits candidates with evidence and confidence. The selected builder always produces an OCI
image, including a small local static runtime for static output, so runtime and rollback stay uniform.

## Immutable releases and activation

A release snapshot contains:

- source identity and commit or image digest;
- build plan and builder version;
- built image digests;
- runtime plan and rendered-configuration digest;
- variable names, scopes and value digests, never plaintext;
- dependencies and ownership mode;
- checks and release strategy;
- blueprint id/version when applicable;
- actor, timestamps, predecessor and provenance.

### Eligible blue/green HTTP service

1. Build/pull without touching the running service.
2. Start candidate on an internal release network and unclaimed loopback port.
3. Run readiness and smoke checks against candidate.
4. Render/validate proxy route to the candidate.
5. Apply route atomically and verify the public hostname.
6. Mark the release live.
7. Send the configured graceful signal to the old release, wait the drain window, then remove it.

If any action before step 4 fails, the old release is untouched. If route verification fails, restore the
previous proxy spec and confirm the old route before declaring recovery. This flow requires a proxy-owned
HTTP route and no exclusive local volume.

### Stop-first service

Compose topologies with fixed host ports, game servers, and services with exclusive local storage usually
cannot run old and new together. Preflight labels the expected downtime. The flow optionally requires a
fresh backup, stops the old service gracefully, starts the candidate, verifies it, and restores the old
immutable release on failure when possible.

“Zero downtime” appears only when the plan proves eligibility. Advanced overrides require an explicit
warning and are recorded.

## Variables and secrets

- Variables are environment-scoped and have `plain` or `secret` sensitivity plus `build`, `runtime`,
  `release-task`, or combined scope.
- Bulk dotenv input parses locally for immediate feedback and on the server as authority. Comments and
  multiline values are supported; invalid names and duplicates are named.
- References are typed objects internally even if the UI renders `${{service.variable}}` syntax.
- Reference resolution is a directed graph with cycle detection and a preview that masks secret leaves.
- Generated secrets use the server CSPRNG, are shown exactly once, and can be rotated with a pending
  deployment indicator.
- Logs pass through exact-value redaction plus credential-pattern redaction before persistence and stream.
  Step runners never print full process environments or secret-bearing rendered files.
- Variable revision/digest participates in the release so the UI can say exactly which saved changes are
  pending.

## Proposed additive storage

Exact SQL is a Checkpoint 0 deliverable, but the schema must model these tables without renaming or
dropping shipped data:

```text
deploy_environments
deploy_sources
deploy_build_plans
deploy_runtime_plans
deploy_releases
deploy_release_artifacts
deploy_steps
deploy_log_chunks
deploy_dependencies
deploy_checks
deploy_triggers
deploy_webhook_deliveries
deploy_variable_revisions
deploy_blueprint_installs
deploy_port_leases
deploy_queue_leases
```

Existing tables receive only additive columns through `store.addedColumns`, each with a default, when a
foreign-key link or new summary state is needed. Migration is transactional and idempotent. Tests open a
real 0.6.6-shaped populated SQLite database, upgrade twice, and prove projects, variables, secrets,
history, hooks, and metrics event joins remain intact.

## API shape

All routes remain under `/api/v1`. This is a directional contract; Checkpoint 0 freezes request/response
types and capability mapping before handlers are written.

```text
GET    /deploy/                                  fleet + active work
POST   /deploy/drafts                            create resumable draft
PUT    /deploy/drafts/{id}                       save a wizard step
POST   /deploy/drafts/{id}/detect                bounded source analysis
POST   /deploy/drafts/{id}/preflight             render findings and plan
POST   /deploy/drafts/{id}/commit                create deployment/environment

GET    /deploy/{id}
PATCH  /deploy/{id}
GET    /deploy/{id}/environments
POST   /deploy/{id}/environments
GET    /deploy/{id}/dependencies
GET    /deploy/{id}/pending

POST   /deploy/{id}/environments/{env}/runs      deploy/redeploy/restart/force
GET    /deploy/{id}/runs
GET    /deploy/{id}/runs/{run}
GET    /deploy/{id}/runs/{run}/stream?after=N    resumable WebSocket events
POST   /deploy/{id}/runs/{run}/cancel
POST   /deploy/{id}/runs/{run}/retry
POST   /deploy/{id}/releases/{release}/activate  rollback/roll-forward

GET/PUT/DELETE  scoped variables, domains, storage, checks, triggers
POST            trigger test, watch-path simulation, check test, import preview

POST   /hooks/deploy/{hookID}                    legacy HMAC-compatible hook
POST   /hooks/providers/{provider}/{hookID}       verified provider events
POST   /deploy/{id}/hooks/{triggerID}             generic scoped deployment hook
```

Reads use `read`. Routine deploy/restart/cancel actions use `service.control`. Source credentials,
secrets, public exposure, blueprint management, and advanced runtime fields require `system.admin` or a
content-dependent server check. Rollback remains destructive with ordinary confirmation. Archiving a
deployment leaves workloads/data unless the user selects a separately confirmed removal plan.

## Stream event contract

```json
{
  "seq": 184,
  "type": "step.log",
  "runId": 84,
  "stepId": 7,
  "ts": "2026-09-02T12:03:46Z",
  "data": { "stream": "stdout", "text": "200 OK · 34 ms" }
}
```

Other closed types: `run.state`, `step.state`, `step.evidence`, `queue.position`, `finding`,
`artifact`, `heartbeat`, and `resync`. A reconnect supplies the last sequence. If retention has removed
that segment, the server sends `resync` plus the current run/step snapshot instead of silently skipping.

WebSocket origin checks, serialized writes, ping/pong, read limits, and auditing follow `internal/wsx`.

## Security and failure invariants

- Webhook endpoints stay behind the network allowlist as the product is architected today. The UI warns
  that public Git providers cannot reach a private-only endpoint and offers an authenticated CI/API path
  appropriate to the operator's network.
- Provider signatures are verified on the raw bounded body; event type, repository, installation,
  target branch, and replay/delivery ID are verified separately.
- No URL credential, token, webhook secret, private key, build secret, environment value, or generated
  config secret enters an audit detail, command argv, stored log, or ordinary project response.
- Archive and upload inputs use `safepath`; build contexts and bind sources use the canonical containment
  service; symlinks cannot escape configured roots.
- A blueprint cannot request privileged mode, host network, devices, capabilities, Docker socket, or a
  host bind without an explicit reviewed declaration and runtime authorization. Built-ins default to no
  new privileges and loopback exposure.
- Image tags are recorded as resolved digests. A later tag move creates a new release; rollback never
  claims to use the old image while pulling the new digest.
- Cleanup never runs during a build/activation lease and does not remove an artifact retained by the
  release policy. Volumes are excluded unless a separate destructive plan names them.
- Automatic rollback is bounded. If both candidate and previous release fail verification, the run stops
  and reports both states; it does not loop.

