# C5 safe activation and recovery review

- Status: passed
- Date: 2026-09-05
- Branch: `patch/0.6.7`
- Scope: delivery-roadmap checkpoint C5

## Implemented boundary

`internal/deploy` now owns a health-gated transition from an immutable candidate release to the live
environment pointer. The Docker runtime owner accepts only the release's recorded local config digest or
repository plus manifest digest. Direct containers carry deployment/environment/release/run labels and
the frozen runtime variable scope. Compose uses a stable per-environment project, the exact ordered source
files, and a generated `0600` release override which replaces every service image with its immutable
identity and sets `pull_policy: never`. Compose never builds during activation and receives interpolation
values only through a temporary `0600` env file that is removed on success, failure and cancellation.

HTTP, TCP, Docker-health, no-shell command and public-route checks share a closed runner with bounded
attempts, per-attempt timeouts and intervals. Required non-passing checks gate activation; optional failures
become warnings. Disabled, unavailable, warning, passed and failed are separate API and UI values.
Evidence records addresses, status/state/exit and stable error codes. Command output is bounded to 64 KiB
and only its SHA-256 digest is retained; response bodies, raw transport errors and credential-bearing URL
components are not persisted.

Blue/green candidates are limited to eligible stateless HTTP runtimes without a fixed port, host network
or writable mount. Their dynamic ports are leased on loopback before Docker creation. Fixed ports,
Compose, games and exclusive writable storage remain `stop_first`: the prior runtime is stopped before the
candidate starts, and both the release and preflight explicitly report expected downtime. Explicit
fixed-port public binds are preserved for Docker while checks and proxy upstreams correctly use loopback
instead of wildcard addresses.

Deployment nginx cutover takes one serialized snapshot of the prior bytes, file mode and exact symlink
target. It validates, reloads and verifies the candidate before the release pointer moves. Any apply,
public-route, verification or pointer-commit failure stops the candidate, restores the exact snapshot,
reloads nginx and verifies bytes, mode, link target and absence cases before the engine may classify the
run as rolled back. Persisted evidence contains the prior digest and flags, not the snapshot bytes.

Shutdown uses the plan's signal and bounded grace before Docker escalation. Predecessor drain, candidate
compensation and stop-first restoration run in bounded safety contexts detached from request cancellation.
The engine calls deployment-owned cancellation cleanup while it still holds the lease: an uncommitted
candidate is removed and the stop-first predecessor restored, while a cancellation after committed
activation completes predecessor retirement. The cleanup result is persisted as a sequenced evidence
event. Container start/stop/removal is idempotent for already-running/stopped/removed states.

The operator actions now have distinct immutable semantics. Deploy and force-build use the current desired
revision, with force-build selecting no cache. Redeploy clones the live release's exact available artifacts
into a new candidate. Rollback does the same from a retained release with rollback priority and traverses
the same backup/check/cutover/retirement path under ordinary confirmation. Restart stops and starts the
same live runtime, rechecks it, and does not create a release. The normalized workspace exposes each action
and an immutable live/retained release list; health and expected downtime remain visible.

## Gate evidence

| C5 gate | Evidence |
| --- | --- |
| Five check runners with retry/timeout/evidence | `TestCheckRunnerCoversHTTPRetriesTCPDockerCommandAndClosedOutcomes`; timeout/secret-free configuration tests; live container matrix |
| Preactivation continuity | `TestFailedReadinessLeavesLiveRuntimeAndRouteUntouched` proves the old pointer/runtime remain and proxy apply/restore/verify are never called |
| Eligible blue/green candidate | `TestCandidatePortLeaseIsLoopbackScopedAndReleasedByToken`; activation strategy validation and runtime-owner tests |
| Honest stop-first/exclusive state | `TestStopFirstExecutorNeverStartsStatefulCandidateConcurrently` proves stop-before-start ordering for an exclusive writable volume |
| Exact proxy recovery | `TestDeploymentRouteReloadFailureRestoresExactPriorSpec` proves prior bytes, `0640` mode and exact symlink target after reload failure |
| Grace/drain/SIGKILL/cancellation bounds | live `SIGTERM_escalates_to_bounded_SIGKILL`; `TestCancellationAfterCandidateStartRestoresStopFirstRuntime`; `TestEngineCancellationInterruptsAdapterAndRecordsStepCleanup` |
| Closed health states | `TestFleetKeepsDisabledUnavailableWarningAndPassedHealthDistinct`; `HealthStatus`; fleet and workspace browser assertions |
| Immutable action semantics | `TestNormalizedActionsUseDistinctImmutableReleaseSemantics`; `TestRestartReusesLiveRuntimeWithoutCreatingARelease`; normalized action browser journey |
| Real runtime adapters | `TestLiveC5ActivationAdapters` covers exact image ownership, HTTP/TCP/Docker-health/command/public-route checks, graceful/forced stop, Compose pinning and env-file cleanup |

## Verification record

Passed from `backend/`:

```text
go build ./...
go vet ./...
go test ./... -count=1
go test -race ./internal/deploy ./internal/proxysvc ./internal/dockerx -count=1

JD_DEPLOY_LIVE=1 go test ./internal/deploy -run '^TestLiveC5ActivationAdapters$' -count=1 -v
immutable_container_and_all_C5_checks                   PASS
SIGTERM_escalates_to_bounded_SIGKILL                    PASS
Compose_uses_pinned_override_and_removes_private_env_file PASS
```

Passed from `frontend/`:

```text
bunx tsc --noEmit
bun run lint
bun run build
JD_BROWSER_BASE_URL=http://127.0.0.1:3310 bunx playwright test tests/browser/deploy-ui.spec.ts
9 passed (44.8s)
```

`git diff --check` passed. The full backend suite passed every package. The opt-in C5 matrix passed against
this host's real Docker Engine, Buildx plugin, image store and Compose installation.

## Explicit later-checkpoint work

C5 activates eligible plain HTTP proxy routes. Certificate issuance/selection, DNS and TLS feature joins,
firewall exposure reconciliation, mutable variable/reference workflows, storage ownership and the required
backup gate belong to C6; a configured HTTPS route or required backup therefore remains explicitly
unavailable rather than silently bypassed. Provider automation and notifications belong to C7, operational
cross-feature diagnosis belongs to C8, and game handshake/blueprint-specific lifecycle work belongs to C9.
