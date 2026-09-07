# C1 persistent-engine review

- Review date: 2026-09-02
- Branch: `patch/0.6.7`
- Baseline: C0 frozen contracts and additive migration
- Result: C1 gate passed

## Implemented boundary

`internal/deploy.OrchestrationStore` is the authority for fresh run requests, complete ordered step
attempts, random-token queue leases, monotonically sequenced events, bounded transcript chunks, retries,
superseding, cancellation, and terminal compaction. A request, all of its steps, and its
`requested -> validating -> queued` evidence commit before an enqueue returns.

`internal/deploy.Engine` claims the highest-priority oldest eligible run subject to one lease per
environment and bounded heavy/light host slots. Heartbeats and every state/log/evidence mutation are
fenced by the random claim token. Startup reclaims expired leases with a new token before consulting the
injected feature-owner reconciler. Pure work may resume, proven side effects may be adopted, and unknown
non-idempotent work fails instead of replaying.

SQLite's one-writer rule is explicit at this boundary. Short orchestration writes are serialized within
the process, while database constraints and claim tokens remain the cross-process authority. The
cancel-versus-complete race consequently produces one valid domain outcome and does not leak
`SQLITE_BUSY_SNAPSHOT`.

The stream commits before publish, closes the commit/register gap, resumes after a sequence, emits a
snapshot/resync when retained text was compacted, caps one log at 64 KiB and one event at 256 KiB, buffers
128 live events, and disconnects a slow subscriber. Terminal compaction keeps the latest 10,000 log
events by default while retaining run, step, state, terminal, and evidence records.

Host commands used by the legacy adapter run in their own process group. Cancellation sends TERM to the
tree, waits a bounded grace, sends KILL to survivors, waits for the leader, and returns structured cleanup
evidence. Operator cancellation is distinguished from lost-lease cancellation so a stale worker cannot
mark work cancelled with a token it no longer owns.

## Compatibility and API proof

The old manual deploy, rollback, and HMAC hook endpoints now enqueue a persistent `legacy_pipeline` step
before returning `202`; the old response keys remain and additive `runId`/`state` fields identify the
durable run. The compatibility executor preserves the existing Git reset, sealed `.env`, stored
pre/post-task, and optional Compose behavior. It also maintains the old bounded `deploy_runs.log`, status,
commit, duration, metrics-join, and history projection while sequenced events are authoritative.

The C1 run create/get/cancel/retry/stream routes are capability protected. Idempotency keys join identical
requests and reject digest conflicts. The WebSocket sends snapshot, retained backlog and live batches,
then a final terminal snapshot; opening it writes `deploy.run.stream.open` to the audit log.

## Gate evidence

Focused proofs:

- `TestRunTransitionTableExhaustivelyAllowsOnlyFrozenEdges`
- `TestStepTransitionTableExhaustivelyAllowsOnlyFrozenEdges`
- `TestQueueEnforcesPriorityEnvironmentAndHostBudgets`
- `TestConcurrentClaimsCannotExceedHeavyBudget`
- `TestCancellationAndCompletionRaceConvergesToOneFencedOutcome` (`-count=100`)
- `TestSubscriptionReconnectIsSequencedWithoutCommitPublishDuplicates`
- `TestSlowSubscriberIsDroppedWithoutStallingAppend`
- `TestTerminalLogCompactionForcesResyncAndBoundsPayloads`
- `TestRestartAtEveryStepUsesEvidenceWithoutDuplicatingSideEffects` (all 15 frozen steps)
- `TestRunGroupTerminatesDescendantsAndRecordsForcedCleanup`
- `TestLegacyCompatibilityPipelineRunsThroughPersistentEngine`
- `TestDeploymentRunStreamSendsSnapshotBacklogLiveTerminalAndAuditsOpen`
- `TestDeploymentRunRoutesPersistBeforeAcceptedAndResumeByID`

Commands run from `backend/` on 2026-09-02:

```text
go test -race ./internal/deploy ./internal/hostexec -count=1
  PASS  internal/deploy     41.683s
  PASS  internal/hostexec    1.107s

go build ./...
  PASS

go vet ./...
  PASS

go test ./... -count=1
  PASS  all packages; internal/api 42.226s, internal/deploy 2.752s
```

The C1 gate does not claim feature-specific Docker image, candidate runtime, proxy cutover, or artifact
reconcilers exist yet. Those owners arrive with C4/C5 and plug into the exercised `StepReconciler`
boundary; the production fallback remains conservative and refuses to replay evidence it cannot prove.
