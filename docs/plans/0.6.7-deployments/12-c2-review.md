# C2 source, detection, draft, and preflight review

- Status: passed
- Date: 2026-09-02
- Branch: `patch/0.6.7`
- Scope: delivery-roadmap checkpoint C2

## Implemented boundary

`internal/deploy` now owns an owner-scoped, 30-day server draft with optimistic revisions, validated
step saves, server-produced detection/preflight steps, and one atomic/idempotent commit. Commit creates
the deployment, production environment, source/build/runtime revision, variables, dependencies, domains,
checks, and trigger without creating a run or changing runtime state.

The source boundary covers:

- public/private Git URL, GitHub/GitLab/Bitbucket/Gitea repository, and contained local checkout input;
- exact branch/tag resolution followed by a private dashboard-owned bare mirror and detached temporary
  worktree, with moved-ref detection, bounded output/time, non-interactive Git, scrubbed inherited Git and
  dashboard variables, host-scoped bearer configuration, mode `0600`, and cleanup;
- registry manifest inspection through the Docker Engine without pull, credential-to-registry scoping,
  immutable digest capture, platform inventory, explicit platform selection, and host compatibility;
- Compose paste, upload, Git, local, and ordered multi-file input with structural YAML bounds, stable
  digest/preview, path and credential checks, and final Compose-authority validation in private temporary
  files with an explicit empty env file and inert variables;
- observation-only existing checkout/container/stack import previews whose environment values and
  credential-shaped argv are absent and whose `wouldChange` list is empty;
- an honest unavailable blueprint-catalog result until C9 supplies the catalog.

Detection is bounded by files, bytes, per-file bytes, depth, and time. It does not follow symlinks,
returns stable candidate ids/evidence/confidence, keeps ambiguity visible, and detects Git submodule/LFS
requirements without fetching either during planning.

Preflight renders the frozen 15-step exact plan and findings across immutable source identity, detected
method, Git choices, Compose variables/features, tool availability, contained paths, host ports, image
architecture, domains/DNS/proxy/certbot, storage/backup policy, readiness, host headroom, strategy, public
binds, and advanced runtime authority. Its observer interface has read methods only. Raw import
observations are removed from the exact plan, and the store verifies the plan bytes/digest/revision before
persisting them.

## Gate evidence

| C2 gate | Evidence |
| --- | --- |
| Every source type | `TestLocalGitImageAndImportSourceAdapters`, `TestRemoteGitAdapterUsesContainedMirrorWorktreeAndEphemeralCredentialFile`, `TestComposeAnalysisAndAdapterPreserveMultiFileOrder`, and the blueprint unavailable assertion |
| Git provider/URL/local, mirror/worktree, exact ref | `TestRemoteGitAdapterUsesContainedMirrorWorktreeAndEphemeralCredentialFile`, `TestPlanningGitMirrorCreatesDetachedWorktreeAtExactRevision`, `TestProviderRemoteNormalization` |
| Monorepo ambiguity and deterministic bounded detection | `TestDetectorIsDeterministicBoundedAndDoesNotFollowSymlinks` |
| Submodule/LFS decision | detector test above and `TestPreflightCoversGitChoicesImageArchitectureAndDomains` |
| Private auth refusal and moved ref | `TestRemoteGitAdapterUsesContainedMirrorWorktreeAndEphemeralCredentialFile`; failed stderr cannot enter the returned error |
| Registry credential/digest/platform | `TestRegistryCredentialIsPassedOutOfBandAndNotReturned`, `TestPreflightCoversGitChoicesImageArchitectureAndDomains` |
| Compose merge/interpolation/path/security | `TestComposeAnalysisAndAdapterPreserveMultiFileOrder`, `TestValidateComposePlanUsesIsolatedPlaceholderEnvironment`, `TestSourceAndPlanValidationRejectsTraversalAndPlaintextCredentials`, `TestValidateComposeFilesUsesOrderedTemporaryInputsWithoutTouchingSource` |
| Unsupported import fields and no adoption side effect | `TestLocalGitImageAndImportSourceAdapters` |
| Malicious archive/symlink paths | `TestExtractRejectsMaliciousArchivePathsBeforeOutsideWrite`, `TestExtractRejectsAbsoluteArchiveSymlinkTargetBeforeFollowupWrite`, `TestJoinRefusesTraversal`, and source/Compose symlink assertions |
| Closed/tamper-resistant plans and references | `TestSourceAndPlanValidationRejectsTraversalAndPlaintextCredentials`, `TestDetectionResultValidationRejectsTamperedEvidence`, `TestDraftRevisionOwnershipExpiryAndAtomicIdempotentCommit` |
| Read-only stable preflight | `TestPreflightIsPureStableAndSecretFree`, `TestHostPreflightObserverDoesNotCreateOrChangeRequestedPaths`, `TestComposePreflightCoversVariablesPortsStorageAndAdvancedFields` |
| Signed-in route, capability, ownership, audit, and no-run journey | `TestDeploymentPlanningSignedInJourneyPersistsWithoutDeploying`, `TestDeploymentDraftRoutesRequireSessionsAndHideOtherOwners`, `TestDeploymentPlanningValidationUsesClosedSecretSafeErrors` |
| Secret-free preview/snapshot/audit/error boundary | detection redaction, Git/registry credential, import, preflight, and signed-in audit assertions above |

## Verification record

Passed from `backend/`:

```text
go vet ./internal/deploy ./internal/api ./internal/dockerx ./internal/files ./internal/safepath
git diff --check
go test ./internal/deploy ./internal/api ./internal/dockerx ./internal/files ./internal/safepath -count=1

go test -race ./internal/deploy ./internal/api ./internal/dockerx ./internal/files ./internal/safepath -count=1
ok internal/deploy (49.616s)
ok internal/api (153.303s)
ok internal/dockerx (29.804s)
ok internal/files
ok internal/safepath

go build ./...
go vet ./...
go test ./... -count=1
```

The final full backend command passed all packages. `docs/internal/deployments/implementation.md` records the mirror/worktree,
planning-Compose environment, draft, and read-only preflight authorities.

## Explicit later-checkpoint work

C2 resolves and previews inputs; it does not execute a deployment. Builder execution and immutable
release artifacts begin at C4, activation/check runners at C5, mutable configuration/reference graphs and
feature ownership joins at C6, and the real blueprint catalog at C9. Those deferrals do not weaken C2's
rule that draft commit itself creates no run and performs no runtime mutation.
