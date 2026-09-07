# Integration with the rest of Just Dashboard

The deployment redesign succeeds only if it makes the existing product feel like one system. This file
defines ownership and handoff boundaries so implementation does not clone a thinner Docker, Git, Proxy,
Backups, Logs, or Metrics surface inside Deployments.

## Resource relationship model

Every dependency edge has one of three ownership modes:

- **Managed**: created from this deployment plan; deletion or rollback may alter it according to an
  explicit plan.
- **Linked**: an existing dashboard resource selected by the operator; Deployments may read and use it but
  does not delete it.
- **Observed**: inferred from Docker labels, ports, mounts, proxy targets, environment references, or
  runtime state; the operator can confirm or correct it.

Ownership is visible wherever removal, rollback, or import could be misread. “Delete deployment” archives
the record by default. “Remove managed resources” is a separately previewed destructive operation.

## Docker

### Reuse

- `ContainerSpec` for image and blueprint runtime configuration;
- server-side command and Compose preview;
- BuildKit-backed builds and image pull/update checks;
- Compose validation, stack discovery and action streaming;
- container stats/history, events, raw inspect, routes, logs and exec;
- diagnosis findings, resource limits, mounts, networks, ports and safe recreation.

### Add

- stable labels for deployment, environment, release, service and ownership;
- release-aware list/filter helpers that inspect each container once;
- candidate naming/networking that cannot collide with the live release;
- digest retention/reference counting so cleanup preserves rollback artifacts;
- a safe activation helper for eligible services and explicit stop-first orchestration otherwise;
- import/adoption preview that translates an existing container back through `SpecOf` or a Compose stack
  into the normalized plan and lists unsupported fields.

### UI handoff

Runtime service rows open the existing container or stack detail with deployment context. Diagnosis
findings render through the existing finding vocabulary. The deployment overview may summarize a finding,
but the remedy remains the action already owned by Docker.

## Git and GitHub

### Reuse

- repository discovery and `?repo=` deep link;
- owner-aware Git command behavior;
- status, history, graph, branches and ahead/behind;
- GitHub device flow, per-repository identity, credential helper, PR listing and push.

### Add

- source connection records that refer to existing credentials by opaque id;
- repository/branch browser backed by the connected provider;
- contained bare mirror and per-release worktrees;
- provider webhook registration, delivery verification and status;
- watch-path simulation against changed files;
- preview deployment mapping from provider PR id to environment.

### Rules

The Git workbench and deployment source are two views of the same repository identity, not two credential
stores. A deployment build never hard-resets the checkout somebody edits in the Git page. A local source
can deliberately opt into “managed in place,” but preflight says that deployment will discard local
changes and requires an ordinary confirmation when the tree is dirty.

## Proxy, domains, certificates, and ports

### Reuse

- `SiteSpec`, server-rendered nginx preview, parse/render round-trip and safe application order;
- DNS checks, wildcard DNS-01 flow, certificate issue/renew/revoke, TLS scan and watched domains;
- port inventory and security service catalogue;
- deep links into site/certificate/TLS detail.

### Add

- deployment dependency id/labels on generated sites without changing nginx semantics;
- candidate upstream support required for health-gated activation;
- port lease records covering the gap between preflight and container creation;
- a domain-conflict query across managed and hand-written proxy sites;
- public-route verification after activation and automatic restoration of the prior proxy spec on failure.

### Rules

- HTTP workloads default to an internal/loopback port behind the proxy.
- A direct public port is an explicit alternative, not an accidental side effect of selecting a template.
- TCP/UDP game ports show firewall state and conflict before deployment.
- Saving a domain does not claim DNS or TLS works. The preflight and run show DNS, certificate and public
  route as separate evidence.
- Proxy remains optional. A missing nginx module permits private/direct workloads and produces a precise
  unavailable result for managed HTTP domains.

## Backups, volumes, and databases

### Reuse

- backup targets, schedules, manual runs, contents, restore and run history;
- database connections, native/built-in dumps, restores, activity and size insight;
- Docker volume/bind inventory and use mapping.

### Add

- resource-aware backup presets generated from blueprint storage declarations;
- `requiredBeforeDeploy`, maximum backup age, retention, destination, and optional restore-test evidence;
- run gate that invokes the backup service and links its run rather than embedding backup logic;
- release dependency snapshots naming volume/bind/database identities;
- import detection that asks whether observed persistent paths are data, cache, or disposable content.

### Rules

- Persistence is never called a backup.
- A stateful blueprint is not Ready to deploy until every required data path is a persistent mount and
  backup policy is explicitly enabled or deliberately declined with a recorded warning.
- Database credentials generated by a blueprint are sealed once and can create a linked Databases entry
  without appearing in logs or audit details.
- Restore remains typed confirmation under the existing invariant. A deployment rollback does not roll
  back database contents unless a separately selected restore plan says so.
- “Backup before deploy” must finish successfully before the state-changing release step. A failed backup
  leaves the current service running and the deployment blocked.

## Metrics, events, health, and diagnosis

### Reuse

- persisted host and per-container history;
- deploy/backup/audit annotations;
- cross-chart cursor, health findings and Docker diagnosis;
- saturation, disk, socket, pressure and inode evidence.

### Add

- deployment phase events for build start/end, activation and rollback;
- release id in container metric identity metadata while keeping name continuity where appropriate;
- comparison window anchored before/after activation;
- a deploy diagnosis aggregator that references existing findings and adds only cross-feature conclusions.

Cross-feature conclusions are conservative and test-pinned. Examples:

- Build slowed while host I/O pressure and disk utilization were saturated.
- Candidate exited 137 and its configured memory limit was reached.
- Candidate is healthy internally, but the public route fails DNS/TLS/upstream verification.
- Runtime wrote significant data into its writable layer and has no persistent mount.
- Release increased restart frequency or memory pressure relative to its predecessor.
- Rollback artifact was pruned, so this release must be rebuilt before activation.

The overview says “cannot assess” when metrics retention is disabled or a required module is unavailable.

## Logs

Build/deployment transcripts and application runtime logs remain distinct:

- the deployment run stream is structured output produced by the orchestrator;
- runtime logs are the application/container/file/journal stream owned by Logs.

The Run page links to runtime logs with a source and time window centered on activation. The overview may
embed the latest small runtime tail using the existing normalized line type. Search, levels, archive
history and export remain on Logs. Secrets are redacted before either persisted deployment output or
runtime environment metadata reaches the browser.

## Files and terminal

- Source and generated-file actions use `/files?path=` and the same root containment.
- A blueprint may declare safe editable files and a field schema, but arbitrary file editing remains the
  Files page and Monaco wrapper.
- A host source shell opens `/terminal?cwd=` exactly once through the established deep link.
- Runtime shell uses container exec because a host shell in the checkout is not the container's
  environment.
- Game console is protocol/process stdin attached to that managed server, not a root host terminal.
- Uploaded Compose, archives, static bundles, worlds and modpacks pass through bounded upload and
  `safepath` extraction. File type, root selection and resulting plan are shown before adoption.

## Firewall and security posture

- A direct public port is annotated by the same service catalogue used by Security.
- Preflight shows whether the firewall permits it and whether the bind is loopback, private, or public.
- The wizard may offer “Open this port” only by calling the existing firewall rule workflow with its
  backend validation and lockout guards. It never edits firewall state implicitly as an unlabelled deploy
  step.
- Security posture gains observed facts for deployment-owned public databases, privileged containers,
  Docker socket mounts, host network, missing backups on persistent services, and unverified public
  routes only when the checks can be made accurately.
- Game server blueprints default to an unprivileged container, explicit TCP/UDP allocations, bounded
  resources and no Docker socket.

## Packages, systemd, PM2, and cron

Container-first adapters cover the 0.6.7 creation contract across distributions. Existing host-native
services remain visible through Processes/systemd/PM2 and can be linked as observed dependencies.

The plan deliberately does not generate arbitrary systemd units or run repository dependencies directly
on the host in 0.6.7: distro/runtime drift and root-level install scripts would create a second execution
security boundary. A future host-native adapter requires its own architecture decision, package ownership
model, rollback strategy, and cross-distribution live tests.

Scheduled deployment tasks use the job scheduler/orchestrator, not host crontab text. A task may deploy,
restart, run a bounded command inside a managed container, issue a game-console command, or invoke a
backup. Every run has history, output, actor `scheduler`, timeout and cancellation behavior.

## Audit, capabilities, and confirmations

New audit actions include draft commit, deployment create/update/archive, source credential attach,
secret reveal/rotate, trigger create/test/delivery, run request/cancel/retry, release activate, import
adopt, domain/storage/backup changes, and blueprint install/update.

Capability mapping is frozen in Checkpoint 0 and route-tested. Content-dependent runtime authorization
reuses the `authoriseSpec` principle: privileged mode, capabilities, devices, host network, sensitive bind
mounts, and Docker socket access cannot be authorized by a generic path-level role check alone.

Typed confirmation remains rare:

- rollback stays ordinary confirmation;
- cancelling a bad run is not destructive and receives no phrase;
- archiving a deployment is ordinary confirmation and leaves runtime/data;
- removing managed volumes, restoring over live data, or discarding a dirty managed checkout retains the
  applicable typed phrases from repository policy;
- turning on a routine auto-deploy trigger is audited but not destructive.

## Notifications and outbound automation

0.6.7 begins with generic outbound HTTPS webhooks rather than embedding many provider SDKs. A channel has
a sealed secret/header set, destination allow/deny policy, test result and event subscriptions.

Events: deployment queued, succeeded, failed, cancelled, automatic rollback, health degraded/recovered,
preview ready/removed, backup gate failed, and blueprint update available.

Payloads are versioned, signed with HMAC, retry with bounded exponential backoff, and contain links and
secret-free summaries. Delivery history states attempts and response class without storing sensitive
response bodies. SMTP/Discord/Slack adapters can be layered later over the same event contract.

## Degraded-host behavior

| Missing facility | Behavior |
| --- | --- |
| Docker | Host deployment module unavailable; existing configuration/history remains readable. |
| Git | Image/Compose paste/blueprint paths remain available when Docker works. |
| GitHub CLI/auth | Public/generic Git remains; connected GitHub choice explains unavailable state. |
| nginx/proxy | Direct/private ports remain; managed HTTP domain step is unavailable, not passed. |
| certificate tooling | HTTP route can exist; HTTPS issuance is unavailable and cannot be reported ready. |
| backup target/module | Stateless deploys proceed; required backup gate blocks with exact reason. |
| metrics recorder | Deploy works; correlation panel says history is unavailable. |
| terminal/tmux | Deploy/runtime exec works; host shell shortcut explains the missing facility. |
| firewall backend | Exposure is shown when known; automatic rule action is unavailable. |

