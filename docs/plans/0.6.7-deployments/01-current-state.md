# Current-state audit and reusable foundations

## What Deployments does today

The existing surface is intentionally narrow:

- A project points at an already-cloned absolute repository path inside `JD_DEPLOY_ROOTS`.
- It stores one branch, one Compose filename, one pre-command, one post-command, and a hook toggle.
- A run fetches `origin`, hard-resets the checkout to the selected remote branch or rollback SHA, writes
  sealed variables to `.env`, runs the optional pre-command, runs
  `docker compose up -d --build --remove-orphans` when the Compose file exists, then runs the optional
  post-command.
- A manual click or a GitHub-style HMAC webhook starts detached work.
- One run may execute per project. A second request receives a conflict; there is no queue.
- Runs record start/end, trigger, actor, from/to commit, status, and one accumulated log string.
- The UI lists project cards, opens run history in a side panel, permits commit rollback, manages sealed
  environment values, and rotates the webhook secret.

Authoritative implementation locations:

- `backend/internal/deploy/{model,deployer,store}.go`
- `backend/internal/api/handlers_deploy.go`
- `backend/internal/store/store.go`
- `frontend/src/app/(dashboard)/deploy/page.tsx`
- deployment types in `frontend/src/lib/types.ts`

## What is already good and must survive

| Existing property | Why it stays |
| --- | --- |
| Deploy roots and symlink-aware validation | A deploy path is a root-equivalent filesystem entry point. |
| Per-project concurrency exclusion | Two Compose operations in one working directory must not race. |
| `GIT_TERMINAL_PROMPT=0` and owner-aware Git | An unattended run must fail rather than hang, and must not leave root-owned files. |
| Deterministic hard reset for managed deploy checkouts | A deployment target is a reproducible artifact, unlike the dashboard's own operator-edited checkout. |
| Sealed variables, audited reveal, redacted logs | The current secret boundary is sound and user-visible. |
| Dashboard secret stripping from child environment | Repository-controlled build content must never inherit `JD_*` or `VPSD_*`. |
| HMAC over the raw webhook body | The unauthenticated CI entry point has an appropriate authentication primitive. |
| Detached operation and persistent run row | A dropped HTTP request does not terminate a deployment. |
| Rollback through the normal pipeline | Recovery is not a second, untested deployment mechanism. |
| Ordinary confirmation for rollback | Recovery must remain usable under pressure; it is recoverable by deploying forward. |

## Material gaps

### Entry and setup

- The user must manually clone a repository and know its absolute server path.
- Public/private Git URLs, connected GitHub repositories, images, Dockerfiles, pasted Compose, static
  sites, templates, uploads, and discovered running resources are not entry points.
- The form asks for implementation details before learning what the user is trying to deploy.
- There is no deterministic repository scan for Dockerfiles, Compose files, framework, monorepo roots,
  lockfiles, ports, `.env.example`, or health endpoints.
- Configuration cannot be saved as a draft or validated independently of deploying it.

### Execution and safety

- “Running” is held in process memory. A backend restart cannot reconcile a stranded run.
- There is no queued state, priority, superseding, deduplication, cancellation, or concurrency budget
  across projects.
- Logs are accumulated in memory and written only at completion; a live build is followed by polling a
  row that may still contain no useful output.
- The execution is a single opaque pipeline instead of named, timed, retry-aware steps.
- A successful `compose up` is called success without readiness, smoke, HTTP, TCP, container-health,
  domain, or TLS verification.
- Compose updates can replace the old container before the new release is known to work.
- There is no separate immutable release artifact; rollback depends on the checkout and a rebuild rather
  than a known image/configuration snapshot.
- Timeout is fixed and broad; no step-specific timeout or cancellation semantics exist.
- Webhooks validate only the signature. Provider event, repository, target branch, delivery replay, and
  changed paths are not checked.

### Operation after deployment

- Deployment does not own or even record its Docker containers, stack, image, volumes, ports, domain,
  certificate, database connections, backup jobs, or runtime health.
- The user has to navigate separately to Docker, Logs, Metrics, Proxy, Backups, Files, Git, or Terminal
  and rediscover the same resource.
- There is no “saved but not applied” indicator for variables or settings.
- There are no deployment notifications, recurring tasks, retention policy, automatic cleanup guard, or
  failed-deployment diagnosis.

### Coverage

- There are no deployment package tests or end-to-end deploy route tests beyond the global route-chain
  assertions. This is the largest test gap in the current feature.
- The frontend has no component test suite. The implementation plan therefore includes browser-level
  journey tests for this safety-critical workflow rather than relying only on a successful build.

## Existing foundations to compose

| Foundation | Reuse in 0.6.7 | Rule |
| --- | --- | --- |
| `internal/jobs` | Resumable bounded live output, cancellation, step commands | Extend with persistence/reconciliation; do not fork another job shape. |
| `internal/dockerx` | image build/pull, container spec, Compose validation/run, inspect, events, diagnosis, updates | Add deployment ownership labels and release-aware helpers centrally. |
| Docker `ContainerSpec` | image-based and blueprint runtime description | Keep one product-shaped spec and server-rendered preview. |
| Git and `ghx` | repository discovery, owner execution, auth status, provider integration | Share repository identity and credentials; do not store a second GitHub login. |
| Proxy site builder | generated domains, HTTPS, DNS checks, config preview/apply | Deployment links to a `SiteSpec`; proxy remains the renderer and safety authority. |
| Backups | scheduled/local/remote backups, restore, job history | Attach policies to deployment-owned state; backups remain the executor. |
| Metrics and events | host/container history and event annotations | Store deployment phase markers and correlate the release window. |
| Logs collector | one vocabulary across container/file/journal/PM2 | Preconfigure resource filters rather than invent a deployment log reader. |
| Files deep link | checkout, generated config, volume/bind content | Preserve `/files?path=` and scoped containment. |
| Terminal deep link | shell in checkout or runtime | Preserve `/terminal?cwd=`; container exec remains Docker's path. |
| Docker diagnosis | runtime findings and actions | Add deployment context to findings; do not duplicate rules in Deployments. |
| Security posture/firewall | exposure and port safety | Preflight public ports against the service catalogue and firewall state. |
| Audit and confirmations | action history and recovery boundaries | Maintain the existing typed/non-typed policy; add route coverage tests. |
| `Page`, `Panel`, state components | page structure and non-happy states | The redesign remains one product, not a competitor-themed island. |
| `useViewState` | stable list/detail tab and filter choices | Persist standing preferences, not half-filled creation forms or search. |

## Architectural discrepancies to resolve before implementation

1. `deploy.Project.Validate` contains its own root containment logic instead of using `files.Resolve`,
   while repository invariant 6 says all client-supplied paths go through `files.Resolve`. Checkpoint 0
   must decide the migration without silently retaining two security implementations.
2. The deployment runner uses `exec.CommandContext` directly for Git, Docker, and shell execution.
   `docs/internal/security/invariants.md` documents the shell exception but also requires host commands to go through `hostexec`.
   The current Git owner handling partly depends on a local command. Checkpoint 0 must document and test
   the exact host/container namespace for every new execution adapter.
3. `internal/jobs` is in-memory, while deployment history is persistent but not live. Reuse requires a
   deliberate persistent-run layer rather than simply replacing one in-memory mechanism with another.
4. The current local `main` branch was stale before this planning branch was created. `patch/0.6.7` was
   correctly based on fetched `origin/main` at merge commit `a436021`, which contains 0.6.6.

