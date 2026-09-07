# ADR 0001: Deployment orchestration and module ownership

- Status: accepted
- Date: 2026-09-02
- Release: 0.6.7

## Context

The 0.6.6 deployer is a request-detached goroutine around one mutable checkout. Its in-memory lock,
completion-only transcript, and implicit `git reset`/Compose pipeline cannot support a persistent queue,
candidate verification, restart recovery, or immutable rollback. The rest of Just Dashboard already owns
the operations a release must compose: Docker, Git, Proxy, Backups, Metrics, Logs, Files, Terminal, audit,
and resumable job output.

The orchestration layer must coordinate those owners without creating a second Docker client, proxy
renderer, backup runner, log parser, or path-containment implementation.

## Decision

`internal/deploy` owns desired deployment configuration, immutable releases, runs, steps, the persistent
queue, leases, event sequencing, triggers, and dependency relationships. Feature packages keep ownership
of their operations and validation. Deployment adapters call narrow interfaces supplied by those packages;
they do not duplicate their parsing or command construction.

The compatibility `Deployer` remains callable while the new engine is introduced. A legacy request is
persisted as a new run and executed through a compatibility adapter; the old API shape remains readable
until the new frontend and migration path are proven.

### Persistence before work

A run and its complete planned step list are committed before an enqueue endpoint returns `202`. Queue
claims use one SQLite transaction:

1. discard terminal, cancelled, and superseded candidates;
2. select the highest-priority oldest queued run whose environment has no active lease;
3. prove a host slot of the requested class is available;
4. create a random claim token and lease expiry;
5. transition the run from `queued` to `preparing` and commit.

The worker heartbeats with the claim token. A stale worker cannot extend, mutate, or finish a lease it no
longer owns. Environment serialization is strict. Host slots are split into one heavy build slot and two
lightweight pull/restart slots by default; both limits are bounded configuration, not arbitrary integers.

### Restart reconciliation

Startup expires stale claims and reconciles the claimed run from persisted step evidence and feature-owner
evidence. It never restarts an unknown side effect merely because its row says `running`.

| Step class | Evidence and recovery |
| --- | --- |
| Resolve/analyse/render/check | Pure or read-only; rerun from the immutable input snapshot. |
| Materialize source | Worktree marker + revision; keep a matching complete tree, otherwise replace the contained temporary tree. |
| Build/pull | Recorded artifact digest plus Docker image label; reuse when both match, otherwise fail/retry the step without touching live runtime. |
| Release task | Completion token written only after exit; an interrupted non-idempotent task fails with a manual recovery action and is never replayed automatically. |
| Start candidate | Deployment/environment/release labels and recorded runtime id; adopt exactly one matching candidate or clean bounded duplicates before retry. |
| Verify | Read-only; resume attempts until the stored deadline. |
| Proxy cutover | Read the feature owner's parsed live spec and probe both candidate and prior routes; converge to one recorded release, restoring the prior spec when evidence is ambiguous. |
| Stop/retire | Inspect the old runtime id; an already-stopped/removed runtime is complete, otherwise continue the bounded drain. |
| Backup/notification | Adopt the linked owner run/delivery id; never create a second one for the same step attempt. |

If evidence cannot prove which side effect happened, the run stops with `failed_activation` or `failed`
and a concrete recovery action. Guessing is not recovery.

### Activation

The engine exposes only two release strategies in 0.6.7:

- `blue_green`: one proxy-owned HTTP service, no exclusive local storage or fixed public port. Build and
  verify a candidate, validate and atomically apply the proxy route, verify the public route, then drain
  the previous release.
- `stop_first`: Compose topologies, fixed ports, game servers, and exclusive local storage. Complete every
  pre-stop gate (including a required backup), stop gracefully, start and verify the candidate, and restore
  the retained prior artifact on failure when possible.

`rolling` and `canary` are not aliases for one of these. They remain unavailable until a normalized plan
proves multiple stateless replicas and traffic splitting on this one host.

### Output and subscribers

Deployment events are appended to SQLite with one monotonically increasing sequence per run before they
are published. The subscription shape is adapted from `internal/jobs`: snapshot plus events after a
sequence, a bounded live channel, and slow-subscriber dropping. Retention may compact old log payloads but
never removes the run summary, step outcome, terminal reason, or artifact evidence. If the requested
sequence was compacted, the stream emits `resync` and a current snapshot.

### Shell boundary

The sole shell boundary remains operator-authored deployment commands stored under admin authorization.
Legacy pre/post hooks and the new release task share one implementation. Request bodies, webhook fields,
blueprints, detector output, checks, and generated plans cannot introduce shell text. Every other command
is an explicit argv. This is an extension of the existing named boundary, not a second boundary.

### Lifecycle

`Server.Start` starts the queue/reconciliation worker after the metrics and feature modules are available.
`Shutdown` stops claims and subscribers but does not kill an activation mid-cutover. Each command still has
a deadline and process-group cancellation. A new process reconciles unfinished work at boot.

## Consequences

- A browser, request, or backend process no longer owns a deployment.
- Old releases remain live until candidate evidence permits replacement.
- The schema and state machine become more complex, but each side effect has one owner and one recovery
  rule.
- Generic in-memory jobs remain appropriate for their current scope; deployment persistence does not turn
  every dashboard job into a database record.

