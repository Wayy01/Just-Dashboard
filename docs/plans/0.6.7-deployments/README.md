# 0.6.7 Deployments: product and delivery plan

- Status: implementation in progress; C0 through C7 are complete, with C8 operations and diagnosis next
- Branch: `patch/0.6.7`
- Research snapshot: 2026-09-02
- Scope: the deployment system for Just Dashboard's one managed Linux server

## Outcome

0.6.7 turns Deployments from a saved `git reset` plus `docker compose up` command into the place where
an operator can bring software onto the server, understand exactly what will happen, watch it happen,
verify that it works, and recover when it does not.

The target experience supports six first-class starting points:

1. source repository, with automatic build detection;
2. Dockerfile;
3. Docker Compose;
4. existing Docker image;
5. a guided service or game-server blueprint;
6. an existing local checkout, Compose stack, or container imported from this server.

The release must keep the product's existing security boundary and its single-server focus. It does not
pretend that “one click” makes state, credentials, DNS, or backups safe. It makes those decisions visible,
checks what can be checked, and links the operator directly to the feature that owns each concern.

## Product thesis

Coolify's strongest idea is that the deployment method is chosen in the user's vocabulary—repository,
Dockerfile, image, Compose, or service—and that a useful default path can reach a running application
without exposing every Docker field. Render, Railway, and Fly.io add a more important operational lesson:
build the candidate first, verify readiness, then replace traffic. Crafty and Pterodactyl show that a game
server is not merely a container; it needs a console, files, ports, resource limits, scheduled actions,
backups, and domain-specific setup.

Just Dashboard can go further because the rest of that operating context already lives here. The
signature interaction is the **release path**:

```text
source  ->  build  ->  release task  ->  start  ->  verify  ->  route
  git        image      migration         new       checks      domain
```

Every node shows its current state, duration, evidence, and recovery action. It is both the creation
wizard's final review and every deployment run's live progress. The same view can link a failure to the
container diagnosis, correlated host metrics, logs, files, proxy site, certificate, database, volume,
backup, or terminal that can explain or fix it.

## Non-negotiable release gates

- A failed build cannot replace a healthy running release.
- A candidate configured for readiness checks cannot receive traffic before it passes them.
- A stateful service cannot be presented as zero-downtime when its storage cannot safely be mounted by
  two releases.
- Deployments survive navigation and browser disconnects; their state and transcript can be resumed.
- Every mutation remains capability-gated, rate-limited where destructive, and audited.
- Secrets remain encrypted at rest, redacted in output, and excluded from child-process inheritance
  except where explicitly scoped to that build or runtime.
- Client-provided paths remain contained; host commands use explicit argv. The existing stored
  operator-authored deploy hook is the only deployment shell boundary unless an architecture decision
  explicitly replaces it.
- Docker, Git, proxy, backup, metrics, or other missing host facilities produce an honest unavailable
  state; they never turn into a reassuring empty result.
- Every generated or edited configuration has a preview and server-side validation before application.
- Every shipped path has a documented recovery procedure and evidence-backed acceptance tests.

## Plan map

| Document | Purpose |
| --- | --- |
| [01-current-state.md](01-current-state.md) | What exists, what is reusable, and the concrete gaps |
| [02-competitive-research.md](02-competitive-research.md) | Competitor analysis, parity ledger, and source record |
| [03-product-ux.md](03-product-ux.md) | Users, information architecture, flows, wireframes, and UI rules |
| [04-end-to-end-architecture.md](04-end-to-end-architecture.md) | Domain model, state machine, execution, migrations, and APIs |
| [05-feature-integration.md](05-feature-integration.md) | How Deployments joins Docker, Git, proxy, backups, metrics, and more |
| [06-blueprints-and-game-servers.md](06-blueprints-and-game-servers.md) | Safe templates and the Minecraft/game-server product slice |
| [07-delivery-roadmap.md](07-delivery-roadmap.md) | Ordered implementation checkpoints and dependency graph |
| [08-test-and-acceptance.md](08-test-and-acceptance.md) | Test matrix, fixtures, failure drills, UX and release acceptance |
| [09-frozen-contracts.md](09-frozen-contracts.md) | Closed domain, state, API, security, error, retention, and migration contracts |
| [10-c0-review.md](10-c0-review.md) | C0 architecture review, upgrade proof, and command evidence |
| [11-c1-review.md](11-c1-review.md) | C1 persistent-engine implementation, race, restart, stream, and compatibility evidence |
| [12-c2-review.md](12-c2-review.md) | C2 source, detection, draft, preflight, security, and verification evidence |
| [adrs/](adrs/) | Accepted orchestration, execution, builder, and browser-test decisions |
| [CHECKPOINTS.md](CHECKPOINTS.md) | The living build/test ledger used during implementation |

## Scope boundaries

### In 0.6.7

- all six creation paths above;
- resumable queued runs with structured steps and live logs;
- preflight, build, release, readiness verification, activation, rollback, and cancellation;
- repository triggers, generic authenticated hooks, schedules, watch paths, and API tokens;
- environment-scoped configuration and secrets;
- first-class domains, ports, persistent storage, backup policy, metrics, logs, and terminal links;
- versioned built-in blueprints, including a complete Minecraft path;
- importing existing server resources without taking ownership until the operator confirms the plan;
- responsive, keyboard-complete, screen-reader-meaningful UI in the existing design system.

### Explicitly not disguised as 0.6.7 scope

- multi-server scheduling, remote build fleets, Docker Swarm, or Kubernetes;
- a public community marketplace that executes unreviewed templates;
- billing, organizations, or hosted-platform tenancy;
- pretending an arbitrary stateful application can have zero downtime;
- arbitrary AI-generated commands executed as root.

Those are product-direction decisions, not small deployment features. The design leaves room for a
future execution target abstraction, but 0.6.7 ships and tests one local host.

## Definition of done

The planning phase is complete when every document above is internally consistent and every researched
competitor capability has a disposition. The implementation phase is complete only when every required
row in `CHECKPOINTS.md` is checked with an evidence link or command result, all gates in
`08-test-and-acceptance.md` pass, the upgrade from the 0.6.6 schema is exercised against populated data,
and the 0.6.7 changelog and version are cut using the repository release process.
