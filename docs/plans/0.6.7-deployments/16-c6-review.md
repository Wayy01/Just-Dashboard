# C6 configuration and feature joins review

- Status: passed
- Date: 2026-09-06
- Branch: `patch/0.6.7`
- Scope: delivery-roadmap checkpoint C6

## Implemented boundary

Deployment configuration is desired state. Saving a variable or environment configuration atomically
creates the next complete source/build/runtime plan revision and never moves the live release pointer.
Pending state compares the current desired inputs with the live release's immutable plan, variable,
dependency and check snapshots using names and digests only. A deploy applies its enqueued revision, while
an edit saved after enqueue remains pending. The workspace therefore offers **Deploy changes** separately
from **Redeploy live**: the former applies desired state and the latter replays only the exact live release.

Variables are encrypted immutable revisions with closed build/runtime/release-task scopes. Server-authority
dotenv parsing supports comments, quoting, multiline values and empty values while naming malformed lines
and duplicates. Generated values use the server CSPRNG, are shown once, and rotations produce a new desired
revision. Ordinary reads return a fixed mask; reveal is a separate session-only admin route and audit event.
Typed references use a closed kind/target parser and directed graph with missing-node and cycle detection.
Execution resolves credential and database secrets through their encrypted owner stores, domains through
the frozen dependency snapshot, and Compose service host/port references through the frozen plan evidence.
Missing and ambiguous owner targets fail closed; external secret leaves are classified secret and literal
reference syntax is never passed to the workload. All execution values feed exact-value output redaction.

Domains are typed managed/linked Proxy dependencies. Preflight reports a separate finding for existing
route conflict, linked-route absence, DNS mismatch/unavailability and matching certificate availability.
HTTPS activation asks Proxy to resolve one existing certificate/key pair covering every configured domain;
no deployment path fabricates certificate locations or issues a certificate. Proxy writes a TLS route with
HTTP redirect and its existing exact snapshot/reload/verification recovery boundary.

Fixed runtime and Compose-published ports are compared with host listeners and Docker inventory. Public
binds, firewall availability and firewall mismatch remain distinct evidence. A fresh observation runs from
the frozen configuration in `analyze_plan`, before build or backup work, so a conflict appearing after the
wizard cannot slip through. The observer excludes only the environment's exact managed proxy site and
recognises only its exact live container/Compose runtime as a reusable port owner; unrelated routes and
listeners remain blocked.

Persistent runtime mounts and dependency rows link to Docker volumes/binds, Backup jobs and Database
connections without duplicating those features' state. Their owner adapters return availability, status,
freshness, restore-test state and deep links. A required backup policy runs through Backups before candidate
start, verifies that the job covers every persistent source, and persists only bounded non-secret evidence.
Failure, staleness, missing coverage or required restore evidence ends the run before runtime creation or a
live-pointer change. Restore-tested remains unavailable until Backups owns real evidence for it.

Import remains observation-only until the dedicated adoption commit is ordinarily confirmed. Adoption
re-observes the resource, requires exact acknowledgement of unsupported fields, records observed ownership
and never starts, stops or claims the external runtime. Archive disables triggers and hides the deployment
without deleting runtime/data. Removal is a separate digest-bound preview/execute workflow containing only
managed resources; linked and observed resources cannot enter its target set. Data targets demand their
exact resource name, and every action delegates to Proxy, Docker, Files, Backups or Databases rather than
deleting foreign rows directly.

The normalized workspace adds variable, network, storage/security and lifecycle surfaces using the
existing design system. It keeps secret input transient, exposes owner deep links and explicit posture,
shows exact managed removal targets before confirmation, and remains usable at narrow portrait and
landscape viewports in light/dark and reduced-motion modes.

## Gate evidence

| C6 gate | Evidence |
| --- | --- |
| Reference graph, scopes and masking | `TestVariableReferenceGraphDetectsMissingAndCycles`; `TestScopedVariablesResolveFeatureOwnerReferencesAndFreezeRunDomains`; configuration route/browser masking assertions |
| Desired save versus live state | `TestVariableMutationAdvancesDesiredWithoutChangingLiveAndFreezesRuns`; `TestSavingEnvironmentConfigurationCreatesDesiredRevisionOnly`; `TestPendingStateClearsAppliedRevisionButKeepsChangeSavedAfterEnqueue`; normalized pending browser journey |
| Domain/DNS/TLS visibility and gating | `TestC6PreflightSurfacesNetworkFirewallAndDependencyGates`; `TestDeploymentRouteRendersExistingTLSCertificateAndRedirect`; `TestResolveDeploymentCertificateRequiresOneExistingPairCoveringEveryDomain`; `TestHTTPSActivationWithoutExistingCertificateStopsCandidateBeforeCutover` |
| Port/public-bind/firewall visibility and gating | `TestExecutionPreflightBlocksFrozenPortConflictBeforeBuild`; `TestExecutionObservationExcludesOnlyItsOwnManagedProxySiteFromConflicts`; `TestExecutionObservationRecognizesPortHeldByItsLiveContainer`; C6 preflight matrix |
| Storage/Backup/Database owner joins | `TestDeploymentBackupGateUsesBackupsOwnerAndReturnsOnlySafeEvidence` covers real artifact creation, freshness, coverage, database/bind observations and honest missing Docker inventory |
| Backup-before-deploy continuity | `TestRequiredBackupGateRunsBeforeStatefulChangeAndPersistsSafeEvidence`; `TestRequiredBackupFailureBlocksBeforeCandidateStart`; `TestRequiredBackupFailureStopsRunBeforeRuntimeAndLeavesLivePointerUntouched` |
| Ownership-aware lifecycle | `TestArchivePreservesResourcesAndRemovalPlanNamesOnlyManagedTargets`; `TestDeploymentImportAdoptionUsesDedicatedConfirmedDraftPathWithoutRuntimeSideEffects`; configuration lifecycle route test |
| Capabilities, session, confirmation, budget and audit | `TestDeploymentConfigurationRoutesEnforceSessionCapabilityConfirmationAndAudit`; `TestDeploymentLifecycleRoutesUseSharedDestructiveBudget`; planning signed-in route journey |
| Operator workflow and responsive contract | `normalized configuration joins keep secrets masked and saved changes pending until deployment`; distinct immutable-action journey; 375x900 and 667x375 light/dark/reduced-motion assertions |

## Verification record

Passed from `backend/`:

```text
go test ./... -count=1
go build ./...
go vet ./...
go test -race ./internal/deploy ./internal/api ./internal/proxysvc ./internal/backups ./internal/store -count=1
```

The full suite passed every backend package. The race gate passed deployment, API, Proxy, Backups and store
packages, including the feature-owner adapters and signed-in route matrix.

Passed from `frontend/`:

```text
bunx tsc --noEmit
bun run lint
bun run build
JD_BROWSER_BASE_URL=http://127.0.0.1:3107 bun run test:browser
11 passed (45.7s)
```

The browser suite includes permanent run links, normalized release actions, the complete C6
save/pending/deploy/live journey, masked and one-time secret behavior, managed-removal preview, responsive
layouts, dark mode and reduced motion. `git diff --check` is the final repository hygiene gate.

## Explicit later-checkpoint work

C7 has not started. Provider-specific webhooks, generic scoped hooks, watch-path rules, schedules, preview
environments and outbound notifications remain C7 work. Cross-feature operational diagnosis remains C8,
and game-specific blueprints, handshake checks and backup/restore drills remain C9. C6 adds no provider
automation, preview lifecycle, notification delivery or other later-checkpoint behavior.
