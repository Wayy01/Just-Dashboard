# Product and UX blueprint

## The people this page serves

### The guided operator

They know the outcome—“host this repository,” “run Minecraft,” “install Uptime Kuma”—but not the
container vocabulary. They need safe defaults, automatic detection, examples, and an explanation next to
the decision it affects. They should reach a correct review without opening Advanced.

### The application developer

They know their repository, commands, environment, health endpoint, and domain. They want speed, exact
control, reproducible releases, deploy-on-push, previews, live output, and a trustworthy rollback. They
should be able to paste a URL or Compose file, accept or edit detection, and deploy.

### The server operator

They care about contention, ports, storage, backups, exposure, disk usage, audit history, and recovery.
They may import existing workloads and must see what the dashboard will take ownership of before it
changes anything.

The same user can move between all three modes. The interface must not force them to choose an “expert”
identity; it reveals detail as the decision requires it.

## Jobs to be done

- Put a known application or game online without translating its documentation into Docker fields.
- Turn a repository into a running service without first learning its build system.
- Know what source, image, configuration, secret set, storage, and route are live now.
- See a deployment's progress and continue watching after a disconnect or navigation.
- Learn why a release failed and open the exact logs, metrics, container, file, domain, or terminal needed
  to recover.
- Change configuration and know whether it is saved, pending deployment, or live.
- Automate routine releases without giving an unauthenticated webhook more authority than necessary.
- Roll back quickly without guessing whether that old revision can still be reproduced.
- Update a stateful service only after its data has a current recoverable backup.

## Interaction principles

1. **Start with the thing, not the mechanism.** “Web app,” “Compose stack,” “Minecraft server,” and
   “existing workload” come before build flags.
2. **Detect, show, then let the operator decide.** Detection never silently becomes execution.
3. **One normalized plan.** Every entry route ends at the same release-path review.
4. **Safe defaults remain visible.** Loopback binding, resource limits, health checks, and volumes are
   prefilled with a brief reason; they are not hidden magic.
5. **Saving is not deploying.** Every changed setting shows `Pending deployment`; the deploy action says
   which changes it will apply.
6. **Progress is an artifact.** A deployment has a stable URL, states, steps, timestamps, actor, source,
   transcript, and evidence. It is not a toast.
7. **Failure ends in a next action.** Show the first failing step, the concrete reason, and targeted
   recovery links. Keep the full transcript available.
8. **Recovery is close to risk.** Rollback, restore, backup, and prior-release comparison live beside
   release history.
9. **Unknown is not healthy.** Unavailable checks and skipped integrations are named.
10. **Advanced means uncommon, not dangerous.** A public bind, host mount, capability, or privileged
    container surfaces at the moment it is selected and receives the server-side authorization it needs.

## Information architecture

### Routes

```text
/deploy
  deployment fleet, active queue, health and release summary

/deploy/new
  resumable creation draft and preflight

/deploy/{project}
  Overview
  Deployments
  Runtime logs
  Configuration
  Variables
  Domains & ports
  Storage & backups
  Automations
  Metrics
  Console & files (game profile adds Players)

/deploy/{project}/runs/{run}
  permanent live/completed run view and release-path evidence

/deploy/blueprints
  reviewed built-in starting points, versions, provenance and update notices
```

Existing `/deploy` links remain valid. Side-panel run history is replaced by deep-linkable pages because a
long-running operation, failed run shared for help, and preview environment need durable addresses.

### Fleet page

The page answers three questions in order:

1. Is anything deploying or unhealthy now?
2. What is running and where can I reach it?
3. What needs action next?

The default layout uses one compact status table at desktop density rather than a two-column wall of
cards. A small installation still reads cleanly; a server with fifty resources remains scannable.

Columns: name/type, environment, live release/source, public endpoint or port, runtime health, last
deployment, resource pressure, pending changes, actions. Search and filters run before rendering. On
mobile each row becomes one disclosure card with the same reading order.

An active-work strip appears only when there is active or queued work. It shows host slots, project,
step, elapsed time, queue position, and Cancel when permitted. Completed work does not leave empty chrome.

### Project overview

The header contains identity, environment, public endpoint, live release, health, and the primary Deploy
or Redeploy action. Secondary actions live in one menu: restart, force rebuild, rollback, stop, archive.

The page body is a two-column operational summary on wide screens:

- the release path and last deployment;
- current findings with direct remedies;
- runtime services and health;
- source and automatic-deploy state;
- domains/TLS and published ports;
- storage with backup freshness;
- CPU/memory/disk/network since the last release;
- recent runtime logs.

Every summary links to its owning tab or existing feature page. It does not become a second Docker,
Proxy, Logs, or Backups implementation.

## Creation flow

The wizard is a dedicated page, not a modal. A deployment can require repository authorization, a DNS
decision, generated credentials, and a Compose review; a modal would either overflow or hide context.

### Step 1: What are you deploying?

Choices use plain outcomes and examples:

- Web app or API
- Static site
- Background worker, bot, or scheduled task
- Docker image
- Docker Compose stack
- Service from a blueprint
- Game server
- Import something already on this server

The choice sets defaults only. It never closes off Advanced options.

### Step 2: Where does it come from?

Contextual choices include connected repository, public Git URL, local checkout, Docker image, paste or
upload Compose, reviewed blueprint, existing container/stack, and game-server import. Credentials are
selected by name; secrets are never echoed back.

Repository access is tested here. The user does not fill five more steps and learn at Deploy that the
server cannot clone it.

### Step 3: Detection

A bounded scan reports evidence rather than a magic verdict:

```text
Detected Next.js because package.json contains next@…
Use /apps/web as the root because its lockfile and package.json are together
Build: bun run build
Start: bun start
Port: 3000 from the start script and framework default (confirm)
Environment: 8 names from .env.example; 3 appear secret-shaped
Persistence: no required directory detected
Health: no endpoint detected; choose one or use TCP readiness
```

Ambiguity is a choice between detected candidates. Low-confidence defaults are explicitly marked
“Confirm,” never presented as facts.

### Step 4: Configure

The basic path shows only decisions needed for the selected workload:

- name and production/preview environment;
- build/start or image/Compose source;
- internal port or game port;
- domain and HTTPS for HTTP workloads;
- required variables with descriptions and generated-secret actions;
- storage and backup for stateful workloads;
- CPU/memory defaults;
- auto-deploy toggle.

Advanced groups are source/build, runtime, networking, health and release, storage, automation, and
security. Group labels describe user intent, not Docker struct names.

### Step 5: Preflight and review

The release path becomes the review. Each node is Pass, Needs decision, Warning, Blocked, or Unavailable,
with measured evidence and an action:

- source identity and revision can be read;
- build method and command are valid;
- required tools/images are available or pullable;
- variables resolve without cycles and required values exist;
- paths are contained;
- ports do not conflict and public exposure matches intent;
- domain DNS/proxy/certificate plan is valid;
- persistent paths are named and backup policy is explicit;
- resource headroom is estimated against current host pressure;
- readiness/smoke checks are syntactically valid;
- destructive or root-equivalent runtime options receive the correct capability gate.

“Deploy” is disabled only for blockers. Warnings require acknowledgement in the ordinary confirmation
dialog and are recorded with the run. A downloadable/rendered plan shows the Git, build, Docker, proxy,
backup, and automation actions without exposing secret values.

### Step 6: Deploy and handoff

The run page opens immediately after a `202` response. It shows queue state and the release path; the
transcript follows the selected step. Navigation away is safe. Completion offers Open application,
Runtime logs, Metrics, and Configuration. A first deployment also offers “Enable deploy on push” when it
was not selected in setup.

## Wireframes

### Fleet

```text
+ Deployments ------------------------------------ [Deploy something]
| 1 active · 1 queued             host slots 1/2                 |
| api-prod  Verify public route  01:42  [View] [Cancel]           |
+---------------------------------------------------------------+
| Search...  [Health v] [Type v] [Environment v]  [Pending only] |
+---------------------------------------------------------------+
| Resource       Live release      Reachable at       Health      |
| api · prod     a12bc34 · 8m       api.example.com    healthy  >  |
| Minecraft      Paper 1.21.8       :25565 TCP         7 players > |
| worker · prod  image sha256:…     private            warning  >  |
+---------------------------------------------------------------+
```

### Project overview

```text
api / production                     healthy   [Open] [Redeploy v]
main @ a12bc34 · live for 8m · no pending changes

+ Release path -------------------------------------------------+
| Source ok -> Build ok -> Migration ok -> Start ok -> Verify ok |
| deployed by webhook · 1m 42s                     [Open run]     |
+---------------------------------------------------------------+
+ Findings -------------------+ + Runtime -----------------------+
| No current findings         | | web  healthy  214 MB  2.1%    |
+----------------------------+ | worker healthy  91 MB   0.4%    |
+ Domain & TLS ---------------+ +--------------------------------+
| api.example.com · A/AAAA ok |
| TLS A · expires in 72d      |
+----------------------------+
```

### Run

```text
Deployment #84 · api / production          verifying · 01:42
main a12bc34 · webhook · actor ci · cancel available

source [done] -- build [done] -- release task [done] -- start [done]
                                                     verify [active] -- route

+ Verify candidate ----------------------------------------------+
| HTTP GET /healthz                           2/5 passing         |
| [12:03:41] 503 Service Unavailable                               |
| [12:03:46] 200 OK · 34 ms                                         |
| ...                                                               |
+-----------------------------------------------------------------+
Old release a09fe11 continues receiving traffic until this passes.
```

## Visual direction

The project design system in `CLAUDE.md`, `Page`, `Panel`, and `globals.css` remains authoritative. The
deployment page uses the existing Geist typography, palette, raised control language, status vocabulary,
hairlines, and `Panel` chrome.

The generic UI skill search proposed blue/orange colors, Fira typography, a marketing hero, and scroll
reveals. Those were intentionally rejected because they conflict with the established dashboard and do
not serve an operational page. The useful guidance retained is dense-dashboard spacing, subtle motion,
explicit progress, inline error recovery, keyboard-complete controls, visible focus, and honest
responsive behavior.

The one deliberate visual signature is the release path. It is structural, not decoration: the line
connects actual ordered states, the active node owns the transcript, and a failure breaks at the exact
point where the operation stopped. Motion is limited to a quiet active-step progress treatment and stops
under `prefers-reduced-motion`.

## Accessibility and responsive contract

- All controls have native semantics, accessible names, visible focus, and at least 44×44 CSS pixel touch
  targets where used on touch layouts.
- Status never relies on color alone; icon, label, and text announce state.
- Run state and new log errors use restrained `aria-live`; the continuously streaming transcript is not a
  live region that overwhelms screen readers. A separate “latest status” region announces phase changes.
- Wizard validation puts a specific error beside each field with `aria-describedby`, focuses a summary
  after submit, and links summary entries to their fields.
- Step navigation says “Step 3 of 5,” permits completed-step review, and never traps the keyboard.
- Tables provide a card/list transformation below the compact breakpoint rather than horizontal viewport
  scrolling. Logs and code previews may scroll inside their panels.
- The primary action remains reachable at 375 px without a sticky control covering form errors or the
  device safe area.
- Copy buttons announce what was copied; icon-only destructive actions keep explicit labels and tooltips.
- Long repository names, domains, image digests, commands, and variable names truncate visually with an
  accessible full value and an expand/copy action.
- Light and dark modes meet contrast requirements using existing semantic tokens; no raw feature colors
  are introduced.

## Content vocabulary

Use: Deploy, Redeploy, Restart, Force rebuild, Roll back, Save changes, Pending deployment, Live release,
Candidate release, Readiness check, Public route, Deployment hook.

Avoid: Submit, magic deploy, serverless, instant, zero downtime when not proven, “healthy” when the check
did not run, and “project” when the visible thing is specifically an application, service, or game server.

