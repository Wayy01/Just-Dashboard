# C4 builders and immutable release artifacts review

- Status: passed
- Date: 2026-09-04
- Branch: `patch/0.6.7`
- Scope: delivery-roadmap checkpoint C4

## Implemented boundary

`internal/deploy` now prepares and produces OCI artifacts without touching the live runtime. The selected
automatic builder is the project-owned, versioned recipe layer from ADR 0003: Node accepts exactly one
supported lockfile, Go accepts exactly one main package, and Python requires a lock/pinned requirements
plus an explicit start command. Static output uses a pinned nginx runtime. Recipe base tags are resolved
first and generated Dockerfiles use their exact digests. Dockerfile, immutable-image, static, and mixed
Compose build/pull paths share the same artifact result and provenance contract. Compose build contexts
and Dockerfiles are resolved relative to their document and rejected if lexical or symlink resolution
escapes the materialized source.

BuildKit receives reviewed recipe secrets through generated environment-backed secret mounts. Secret
values do not enter argv, preview, metadata, log storage, image configuration, history, or saved layer
bytes. A custom Dockerfile cannot offer that guarantee, so scoped build secrets and obvious embedded
credential literals fail closed. Cache reuse is the default; plan `noCache` and the `force_build`
operation both produce the exact `--no-cache` invocation.

Every enqueue atomically freezes exact variable revision references and canonical dependency/check JSON,
including explicit headers for empty sets. Execution verifies those digests before decrypting only the
requested closed scope. Retry copies the original snapshots, while a later run observes later rotations.
Candidate creation persists the actual secret-free runtime snapshot bytes, rejects a mismatched supplied
digest, records every Compose service image, generated Dockerfile/argv/base provenance, variable and plan
input digests, strategy, expected downtime, source identity, actor, predecessor, and all immutable
artifact metadata. Release/source/build/runtime/variable/run-snapshot identity is append-only. A moving
image tag therefore produces a distinct release and comparison fingerprint, including non-primary
Compose services.

Named release tasks have a 1–3600 second timeout, a contained working directory, explicit
`release_task` variable allowlist, bounded/redacted line output, process-group cancellation, and
secret-free evidence. Interrupted non-idempotent tasks require operator review. Legacy Compose execution
is unchanged; the additive mapper creates an honest historical release and unavailable Compose artifact
for each successful 0.6.6 run while preserving ids, ciphertext, logs, and live selection.

Retention protects the current release plus five prior successful releases, candidates, pins,
retain-until policies, the seven-day failed diagnostic window, shared physical digests, and environments
with an active queue lease. Every decision carries a reason. Cleanup reserves rows transactionally,
accounts reclaimed bytes, and never deletes a tag after it has moved to a different image; it uses the
recorded local config digest or immutable artifact digest instead.

## Gate evidence

| C4 gate | Evidence |
| --- | --- |
| Selected automatic builder | `TestAutomaticRecipesAndExplicitAdaptersRenderPinnedPlans` covers Node service/static, Go, and locked Python recipes with exact digest-pinned base images |
| Dockerfile/static/image/Compose adapters | hermetic adapter tests plus all corresponding `TestLiveC4ArtifactAdapters` subtests |
| Contained Compose build contexts | `TestComposeAdapterBuildsContainedServiceContexts`; `TestComposeAdapterRejectsEscapingBuildSymlink` |
| Immutable artifacts and configuration snapshot | `TestImageAndComposeAdaptersPinResolvedDigests`; `TestCandidateReleaseIsAtomicIdempotentImmutableAndSecretFree` |
| Frozen variables/dependencies/checks and retry | `TestQueuedRunAndRetryKeepExactVariableRevisionsAcrossRotation` |
| Cache and force-no-cache | automatic/Compose preparation assertions and normalized executor `OperationForceBuild` selection |
| Named timed release tasks | `TestStoredReleaseTaskUsesExplicitScopeAndRedactsOutput`; cancellation and escaping/missing-directory tests |
| Failed build isolation | live `failed_build_preserves_live_runtime` plus `TestEngineFailureStopsBeforeLaterStepsAndPreservesTerminalEvidence`; no activation owner is called before the C5 steps |
| Moving tags and comparison | `TestMovingImageIdentityCreatesDistinctReleaseAndComparison`; `TestReleaseBuildFingerprintIncludesEveryComposeService`; cleanup tag-movement cases |
| Secret absence | `TestRecipeBuildSecretsUseNamedMountsAndAreRedacted`; live automatic recipe scans metadata/output, image config, history, and every byte of `docker save` |
| Retention/current+5/reasons/prune | `TestArtifactRetentionProtectsRollbackSetPinsSharedDigestsAndLeases` covers live, five predecessors, pin, shared digest, active lease, actual prune state, and reclaimed bytes |
| Legacy release conversion | `TestOpenMigratesPopulated066DeploymentsIdempotently`; `TestLegacyCompatibilityPipelineRunsThroughPersistentEngine` |

## Verification record

Passed from `backend/`:

```text
go build ./...
go vet ./...
go test ./... -count=1
go test -race ./internal/deploy ./internal/hostexec ./internal/store -count=1

JD_DEPLOY_LIVE=1 go test ./internal/deploy -run TestLiveC4ArtifactAdapters -count=1 -v
automatic_recipe_and_secret_layers  PASS
Dockerfile                         PASS
static                             PASS
immutable_image                    PASS
mixed_Compose                      PASS
failed_build_preserves_live_runtime PASS
```

Passed from `frontend/` after the advanced builder/task controls were added:

```text
bunx tsc --noEmit
bun run lint
bun run build
JD_BROWSER_BASE_URL=http://127.0.0.1:3310 bun run test:browser
9 passed (37.7s)
```

`git diff --check` also passed. The full backend suite passed every package; the opt-in live matrix passed
against this host's real Docker Engine, Buildx plugin, image store, and Compose installation.

## Explicit later-checkpoint work

C4 stops after producing and recording a candidate. It cannot cut over a proxy route or stop a live
service: readiness/check runners, candidate runtime creation, blue/green or stop-first activation,
graceful drain, restoration, and action semantics are C5. C6 owns mutable configuration/reference-graph
workflows, while release presentation and operator cleanup controls arrive with the C9 release UI. These
boundaries do not permit a failed C4 build to reach a live container or route.
