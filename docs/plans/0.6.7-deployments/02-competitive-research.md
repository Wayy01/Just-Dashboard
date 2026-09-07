# Competitive research and parity ledger

Research was performed against official product documentation and official repositories available on
2026-09-02. Marketing counts are treated as directional; behaviors documented in technical pages drive
the plan.

## What competitors do best

### Coolify

Coolify's advantage is breadth with a low-friction first deployment. It starts from the source the user
recognizes—public/private Git, Dockerfile, Compose, or image—uses Nixpacks for detection, connects Git
providers, creates pull-request previews, gives services domains and variables, and exposes deployment
history and rollback. Its large one-click service library makes “I want this product” a valid starting
point instead of requiring prior Docker knowledge.

The best ideas to adopt are source-first creation, useful automatic defaults, preview environments,
watch-path filters for monorepos, a deployment queue, a visible operation log, health checks required for
safe rolling updates, automatic cleanup that pauses during deployments, and event-specific notifications.

The opportunity to improve is evidence and integration. “Container started” is weaker than “candidate
passed its declared checks and the public route answers.” A one-click service still leaves the operator
responsible for versions, credentials, backups, and recovery. Just Dashboard can put those obligations in
the creation review and continuously show whether each remains satisfied.

### Dokploy and Easypanel

Dokploy gives each application a coherent workspace: configuration, variables, domains, previews,
schedules, backups, deployments, logs, monitoring, and advanced controls. Its preview lifecycle—create
on PR, update on new commits, delete on close—is a strong automation baseline. Its registry-backed
rollback avoids rebuilding an old commit.

Easypanel is particularly clear about lifecycle actions and about the distinction between saving settings
and deploying them. It also joins domains, storage backups, resource limits, scripts, logs, shell, and
metrics around one service. These are information-architecture wins worth copying.

### CapRover and Portainer

CapRover demonstrates practical one-click rollback and honest zero-downtime constraints: start-first is
appropriate for stateless services, while persistent local storage forces stop-first unless the
application explicitly supports concurrent access.

Portainer remains strongest at importing Compose in multiple forms and GitOps updates. It can use a web
editor, uploaded file, Git repository, multiple Compose files, polling or webhook updates, and a forced
redeploy. The lesson is to accept the form the operator already has while keeping one normalized internal
plan.

### Railway, Render, and Fly.io

Hosted platforms set the safety bar. Railway waits for the configured health endpoint before activating a
new deployment, treats a pre-deploy command as a gate, and turns a prior deployment into a deployable
artifact. Render builds before replacement, starts the new instance beside the old, waits for health,
switches traffic, then performs a graceful shutdown. Fly.io exposes rolling, canary, and blue-green
strategies, release commands, smoke checks, and health-aware routing.

On one Docker host not every strategy is safe or possible, but the invariant transfers cleanly: preserve
the current release until the candidate proves it can replace it, and state plainly when storage or port
constraints require downtime.

### Pterodactyl and Crafty

Game panels prove that a Minecraft server is an operational workload rather than a one-time install.
Useful abstractions include version/runtime selection, startup variables, TCP/UDP allocation, console,
file access, player controls, schedules, backup/restore, resource limits, and an explicit EULA gate.
Crafty's ability to import an existing server, schedule chained tasks, and run a backup before update are
especially relevant to automation and recovery.

## Capability parity ledger

“0.6.7” means a required implementation checkpoint. “Foundation” means it already exists elsewhere but
must be joined into the deployment experience. “Future” means the competitor capability conflicts with
the one-server architecture or requires a separately approved product boundary.

| Capability | Seen in | Disposition | Just Dashboard interpretation |
| --- | --- | --- | --- |
| Public Git repository | Coolify, Dokploy, CapRover, Portainer, hosted PaaS | 0.6.7 | URL, ref, optional subdirectory; server-managed checkout. |
| Private Git repository | Coolify, Portainer, hosted PaaS | 0.6.7 | Reuse GitHub sign-in first; deploy key/token adapter with sealed credentials. |
| GitHub App repository browser | Coolify | 0.6.7 | Extend existing per-repository GitHub identity; minimal permissions and webhook verification. |
| GitLab/Bitbucket/Gitea/generic Git | Coolify, Dokploy | 0.6.7 | Provider-specific webhook verifier over a generic Git source. |
| Local checkout | Existing product | 0.6.7 | Preserve and label as “managed checkout” or “use in place.” |
| Dockerfile build | All application PaaS products | 0.6.7 | BuildKit-backed immutable image and server-rendered build plan. |
| Automatic buildpack detection | Coolify, Dokploy, Railway, Render | 0.6.7 | Pluggable detector; spike Nixpacks/Pack before selecting dependency. |
| Static-site build | Coolify, Render | 0.6.7 | Build command + output directory + local static runtime behind proxy. |
| Docker image | Coolify, CapRover, Railway, Easypanel | 0.6.7 | Reuse `ContainerSpec`; support registry credentials and digest recording. |
| Docker Compose | Coolify, Dokploy, Portainer, Easypanel | 0.6.7 | Git, paste, upload, local file, multiple `-f` files, validated normalization. |
| One-click services/templates | Coolify, CapRover, Railway, Portainer | 0.6.7 | Small reviewed, versioned blueprints with provenance; no unreviewed marketplace. |
| Import existing workload | Portainer, Crafty | 0.6.7 | Detect local stack/container/checkout; preview ownership before adopting it. |
| Monorepo base directory | Coolify, Render, Railway | 0.6.7 | Auto-detect candidates; explicit root and build context. |
| Watch paths | Coolify, Render | 0.6.7 | Ordered include/exclude matcher; webhook-only; test simulator in UI. |
| Submodules and Git LFS | Coolify | 0.6.7 | Detected opt-ins, availability preflight, bounded fetch behavior. |
| Environment variables | All | 0.6.7 | bulk dotenv editor, per-variable editor, scopes, generated values, sealed secrets. |
| Reference variables | Railway, Easypanel | 0.6.7 | typed references to domain, service host/port, database URL, and generated secret. |
| Build vs runtime secret scope | Hosted PaaS | 0.6.7 | explicit scopes and redaction; never silently expose runtime secrets to builds. |
| Persistent volumes/binds/files | All container platforms | Foundation + 0.6.7 | Reuse Docker model; classify state and attach backup readiness. |
| Domain and managed HTTPS | Coolify, Dokploy, Easypanel, CapRover | Foundation + 0.6.7 | Compose Proxy `SiteSpec`, DNS check, certificate and firewall evidence in preflight. |
| TCP/UDP published ports | All; critical for games | Foundation + 0.6.7 | conflict-safe port planner, loopback default, explicit public exposure. |
| Health/readiness checks | Coolify, Railway, Render, Fly.io | 0.6.7 | TCP, HTTP, Docker health, command and public-route checks with evidence. |
| Zero-downtime deployment | Coolify, CapRover, Render, Fly.io | 0.6.7 where safe | Health-gated blue/green for eligible HTTP services; honest stop-first otherwise. |
| Rolling/canary strategies | Fly.io, Coolify | 0.6.7 limited | Single-host canary/blue-green for stateless services; no fake multi-node orchestration. |
| Release/pre-deploy command | Fly.io, Railway | 0.6.7 | Named gated step after build, before activation, with timeout and explicit env scope. |
| Post-deploy/smoke checks | Fly.io, hosted PaaS | 0.6.7 | Candidate and public-route checks; automatic rollback policy. |
| Graceful shutdown/drain | Render, Railway | 0.6.7 | configurable signal and bounded drain window. |
| Immutable release artifact | Dokploy, Railway, Fly.io | 0.6.7 | image digest + configuration snapshot + source revision + provenance. |
| One-click rollback | All major competitors | 0.6.7 | activate known artifact when retained; rebuild only when artifact is unavailable and say so. |
| Deployment queue | Coolify, Dokploy, hosted PaaS | 0.6.7 | persistent per-project serialization, host concurrency budget, cancel/supersede queued runs. |
| Live resumable logs | All | 0.6.7 | structured step events and bounded log chunks; reconnect from sequence. |
| Cancel deployment | Coolify/Dokploy/Railway | 0.6.7 | queued and running cancellation with process-group semantics and cleanup. |
| Force rebuild/no cache | Coolify, Easypanel | 0.6.7 | deploy action variant recorded in run metadata. |
| Automatic deploy on push | All | 0.6.7 | provider event verification and branch/repository match. |
| Generic deploy webhook/API | Coolify, Dokploy, Portainer | 0.6.7 | authenticated action endpoint; retain HMAC compatibility and add delivery records. |
| Pull-request previews | Coolify, Dokploy, Render, Railway | 0.6.7 | opt-in, quota, generated hostname, inheritance rules, cleanup on close. |
| Scheduled jobs | Dokploy, Crafty, Render | Foundation + 0.6.7 | run command, deploy, restart, backup, update, or game command with history. |
| Runtime logs and shell | Dokploy, Easypanel, Pterodactyl | Foundation | Open prefiltered Logs, container exec, or host terminal in one click. |
| Per-service metrics | Dokploy, Easypanel, hosted PaaS | Foundation | Existing persistent container metrics, correlated to release events. |
| Notifications | Coolify, hosted PaaS | 0.6.7 | outbound webhook first; event subscriptions for failure/success/health/backup. |
| Resource limits and replicas | All | Foundation + 0.6.7 | reuse limits; replicas only for eligible stateless services on this host. |
| Automatic Docker cleanup | Coolify | Foundation + 0.6.7 | schedule/threshold policy, deployment lock, preview, never volumes by default. |
| Database provisioning/backups | Coolify, Dokploy, Easypanel | Foundation + 0.6.7 | blueprint creates service; Databases and Backups own operations and restore evidence. |
| Backup before stateful update | Crafty | 0.6.7 | policy gate with freshness and optional tested-restore requirement. |
| Game server setup | Pterodactyl, Crafty | 0.6.7 | reviewed blueprint profile; Minecraft is acceptance workload. |
| Game console/files/players/schedules | Pterodactyl, Crafty | Foundation + 0.6.7 | join existing primitives; add only game-specific protocol/actions. |
| Multi-server/remote builders/Swarm | Coolify, Dokploy, Fly.io | Future | Requires a separate target/agent architecture decision. |
| Teams and per-resource collaborators | Coolify, Pterodactyl | Future | Existing capability model remains instance-wide; resource ACL is a separate auth project. |
| Public template marketplace | Coolify, Railway | Future | Requires signing, moderation, update policy, and supply-chain ownership. |

## Source record

Primary and official sources used for the ledger:

- Coolify: [Applications](https://coolify.io/docs/applications/index),
  [deployment overview](https://next.coolify.io/docs/applications/deployments/overview),
  [automatic deployments](https://next.coolify.io/docs/applications/deployments/automatic-deployments),
  [health checks](https://coolify.io/docs/knowledge-base/health-checks),
  [services](https://coolify.io/docs/services/introduction),
  [service directory](https://coolify.io/services),
  [GitHub App setup](https://next.coolify.io/docs/applications/sources/github/app),
  [notifications](https://coolify.io/docs/knowledge-base/notifications/), and
  [automated cleanup](https://coolify.io/docs/knowledge-base/server/automated-cleanup).
- Dokploy: [applications](https://docs.dokploy.com/docs/core/applications),
  [preview deployments](https://docs.dokploy.com/docs/core/applications/preview-deployments),
  [rollbacks](https://docs.dokploy.com/docs/core/applications/rollbacks), and
  [scheduled jobs](https://docs.dokploy.com/docs/core/schedule-jobs).
- Easypanel: [App service](https://easypanel.io/docs/services/app) and
  [backups](https://easypanel.io/docs/backups).
- CapRover: [deployment methods](https://caprover.com/docs/deployment-methods.html),
  [one-click apps](https://caprover.com/docs/one-click-apps),
  [persistent apps](https://caprover.com/docs/persistent-apps), and
  [zero-downtime deployments](https://caprover.com/docs/zero-downtime.html).
- Portainer: [add a stack](https://docs.portainer.io/sts/user/docker/stacks/add),
  [stack webhooks](https://docs.portainer.io/user/docker/stacks/webhooks), and
  [templates](https://docs.portainer.io/user/docker/templates).
- Railway: [deployment reference](https://docs.railway.com/deployments/reference),
  [health checks](https://docs.railway.com/deployments/healthchecks),
  [pre-deploy commands](https://docs.railway.com/deployments/pre-deploy-command), and
  [template practices](https://docs.railway.com/templates/best-practices).
- Render: [deployments](https://render.com/docs/deploys),
  [health checks](https://render.com/docs/health-checks), and
  [Blueprint specification](https://render.com/docs/blueprint-spec).
- Fly.io: [deploy an app](https://fly.io/docs/launch/deploy/),
  [health checks](https://fly.io/docs/reference/health-checks/), and
  [seamless deployments](https://fly.io/docs/blueprints/seamless-deployments/).
- Pterodactyl: [Panel repository](https://github.com/pterodactyl/panel),
  [Wings](https://github.com/pterodactyl/wings), and
  [custom eggs](https://github.com/pterodactyl/documentation/blob/master/community/config/eggs/creating_a_custom_egg.md).
- Crafty Controller: [Minecraft creation/import](https://docs.craftycontrol.com/pages/user-guide/server-creation/minecraft/),
  [task scheduler](https://docs.craftycontrol.com/pages/user-guide/task-scheduler/),
  [backup manager](https://docs.craftycontrol.com/pages/user-guide/backup-manager/), and
  [roles](https://docs.craftycontrol.com/pages/user-guide/user-role-config/).

