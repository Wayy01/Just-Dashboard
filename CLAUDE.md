# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

Just Dashboard is a self-hosted control panel for **one** Linux server: metrics, Docker,
processes, logs, a real PTY, files, git, databases, reverse proxy, firewall, backups and
deploys behind a single authenticated UI. A Go backend and a Next.js frontend sit behind a
Caddy reverse proxy as one `docker compose` stack.

The software is **root-equivalent** — it drives the Docker socket, systemd, the firewall,
host accounts and a shell. Its security boundary is *the network perimeter plus mandatory
2FA*, not the container. Every architectural oddity in this repo traces back to that.

## Commands

### Backend (`backend/`, Go)

```bash
cd backend
go build ./...                      # compile
go vet ./...
go test ./...
go test ./internal/gitx -run TestBranchParse -v   # one test
go run ./cmd/server                 # needs JD_MASTER_KEY set and JD_DATA_DIR writable
```

`go.mod` declares `go 1.25.0`; an older local toolchain will try to auto-download 1.25 and
fail on a network-restricted machine. Check `go version` before blaming the code.

Fourteen packages carry tests (`agent`, `api`, `config`, `dbx`, `dockerx`, `files`, `gitx`,
`httpx`, `metrics`, `netsec`, `procs`, `safepath`, `term`, `updates`). They are all fast and
hermetic — none needs Docker, systemd or a network — so `go test ./...` is a reasonable thing
to run on every change, and the ones guarding the security invariants (`httpx/confirm_test.go`,
`api/routes_test.go`, `files/files_test.go`, `safepath/safepath_test.go`, `dbx/classify_test.go`)
are the ones to extend when you touch that surface.

### Frontend (`frontend/`, Next.js + bun)

```bash
cd frontend
bun install
bun dev                             # :3000, proxies /api to 127.0.0.1:8080
bun run lint                        # eslint (flat config, next core-web-vitals + ts)
bun run build
```

bun is the package manager (`bun.lock`, `packageManager: bun@1.3.11`) — do not introduce
`package-lock.json` or `yarn.lock`. Next's dev rewrite proxies HTTP but **not** WebSocket
upgrades, so for socket-backed pages in dev also set
`NEXT_PUBLIC_WS_BASE=http://localhost:8080`.

There is no frontend test suite; `bun run build` and `bun run lint` are the whole gate.
(`playwright` sits in devDependencies for ad-hoc screenshotting and is wired to no script.)
`next.config.ts` sets `output: "standalone"` for the Docker image and `agentRules: false` —
this repo keeps its own instructions and the generated ones are noise.

### Whole stack

```bash
sudo ./install.sh                   # interactive first install; re-runnable, keeps .env
docker compose up -d --build
docker compose logs backend | grep "bootstrap admin"   # generated password, printed once
```

CONTRIBUTING requires `go build ./...` and `bun run build` to pass before a PR.

## Architecture

### The request chain is the security contract

`backend/internal/api/routes.go` is the map of the entire API surface. Every `/api/v1`
request passes, in order:

```
network allowlist → rate limit → authenticate → capability → handler
```

- **Allowlist before auth** (`httpx.AllowlistCIDRs`) — an off-network attacker cannot reach
  the login handler at all. `httpx.RealIP` only trusts `X-Forwarded-For` from
  `JD_TRUSTED_PROXIES`, otherwise the allowlist could be spoofed past.
- **Three rate limiters** on the `Server`, all `NewLimiter(perMinute, burst)`: `loginLim`
  (10/min, burst 5, per address, on top of the per-account lockout), `apiLim` (600/min,
  burst 120, per principal), `destrLim` (30/min, burst 10, per principal, destructive routes).
- **Capabilities, not roles** (`internal/auth/roles.go`): `read`, `service.control`,
  `file.write`, `terminal`, `destructive`, `system.admin`, held by the three roles `admin`,
  `limited` and `readonly`. Routes gate on `httpx.RequireCapability(...)` so adding a role
  later cannot silently widen an endpoint. `httpx.RequireSession` additionally blocks
  API-token principals from human-only routes (password change, minting tokens, account
  management).
- **`httpx.AuditMutations`** records every state-changing request. WebSocket routes are GET
  and long-lived, so they call `s.recordAudit(...)` at open time instead — the interesting
  event is "a terminal was opened", not a status code written when the socket finally closes.

Two exceptions to the chain, both deliberate: `/healthz` (unauthenticated, reveals nothing —
a fixed body, no version, no hostname) and `/api/v1/hooks/deploy/{hookID}` (HMAC over the raw
body, still behind the allowlist, and still inside `AuditMutations` so that enumerating hook
ids is not silent).

### Handler conventions (backend)

Handlers are `func(w http.ResponseWriter, r *http.Request) error`, i.e. `httpx.Handler`, whose
own `ServeHTTP` renders a returned error. `s.handle(...)` at the mount site is just the
conversion to `http.Handler` — it adds no behaviour. Return `httpx.Err/BadRequest/Internal/Wrap`
— never write an error body by hand; `httpx.WriteError` is the single renderer and is what
keeps internal error strings off the wire. Decode bodies with `httpx.DecodeJSON` (4 MB cap,
unknown fields rejected).

Irreversible actions require the caller to echo a phrase in the **`X-Confirm`** header via
`httpx.RequireTypedConfirmation(w, r, phrase)`, called **inside the handler** where the
expected phrase is known; the phrase comes back to the client as `error.phrase`, not parsed
out of the message. `s.destructive(r, ...)` is the marker for "irreversible" and adds the
capability check and the tighter `destrLim` budget — it does *not* enforce confirmation.
Adding a destructive route means doing both. It is nested inside stricter groups too
(`system.admin` routes that delete something), because admin holds every capability, so
"which routes are irreversible?" has one answer. The single exception is
`POST /databases/{id}/query`, where the answer depends on the SQL: `dbx.Classify` decides,
fails closed on anything it does not recognise, and the handler applies the same capability
check and budget by hand.

Handlers live in `api/handlers_*.go`, one file per feature, each with its own
`mount<Feature>Routes(r chi.Router)` called from `Routes()`. `handlers_domains.go` is the one
that does not own a mount function — watched-domain certificate checks are part of
`mountProxyRoutes`, under `/certificates/watched`, because that is where they are used.
Shared request plumbing that belongs to no feature (`atoiDefault`, `timeoutCtx`,
`recordAudit`, `detachedContext`) lives in `api/helpers.go`.

### Server, modules, degradation

`api.Server` holds the dependency graph — config, logger, store, auth service, sealer, audit
logger, authenticator, WS upgrader, the three limiters, and in agent mode the `agent.Identity`.
`api/modules.go` (`moduleSet`) holds the feature backends: `sys`, `metrics`, `docker`,
`dockerStats`, `pm2`, `systemd`, `table`, `cron`, `logs`, `term`, `files`, `git`, `updates`,
`proxy`, `dbs`, `linuxUsers`, `netsec`, the three backup pieces (`backupStore`, `backupRunner`,
`backupSched`) and the two deploy pieces (`deployStore`, `deployer`).

Each module is optional by design: a host with no Docker socket, no systemd or no fail2ban
still serves everything else, and the affected routes return a precise "unavailable on this
host" code that the frontend renders as information rather than an error (see `ErrorState` in
`frontend/src/components/state.tsx`).

`Server.Start(ctx)` is separate from `New` so a failure to schedule background work is
reported by `main` rather than swallowed during construction. It starts the metrics recorder
and the backup scheduler; the recorder is started here rather than lazily on first request
precisely because nothing may ever request it — its whole purpose is to have been running
while nobody was looking. `Shutdown` releases what outlives a request: metrics sampler,
backup scheduler, live PTYs, database pools, the Docker client.

`helpers.detachedContext` is the deliberate opposite: work that must outlive the request that
started it (a backup transfer, a `docker compose up --build`) descends from
`context.Background()` and is *not* cancelled by shutdown, because a deploy killed halfway is
worse than one that finishes into a dashboard that is no longer running. Its timeout is the
only bound; nothing joins those goroutines.

### Reaching the host, not the container

`internal/hostexec` is how the dashboard acts on the server rather than on its own image:

- `Command` runs a binary locally when present, else via `nsenter --target 1` into the
  host's namespaces. `CommandOnHost` *always* crosses (for tools like `who` that exist in
  the image but would report on the container).
- `AsOwner(cmd)` drops to the UID/GID owning `cmd.Dir`, so a `git pull` on a repo owned by
  `deploy` does not leave root-owned files.
- Argv is passed through unchanged and **never** through a shell. Keep it that way.

`internal/files.Resolve` is the single choke point for client-supplied paths — it checks the
cleaned path *and* the symlink-resolved path (parent directory for files that do not exist
yet) against `JD_FILE_ROOTS`. Any new filesystem entry point goes through it, including the
ones that only look like they belong elsewhere (backup restore destinations, database dump
paths). Its sibling `ResolveEntry` applies the same containment but returns the entry rather
than what it points at — that is the one to use for delete, move, stat and chmod, which act
*on* a symlink rather than through it.

`internal/safepath` holds the rules for unpacking an archive whose contents an attacker may
control: absolute symlink targets are refused, nothing is written through a symlink already
in the destination, and the final component is unlinked rather than followed. Both
`files/archive.go` and `backups/restore.go` use it; they used to carry a copy each of the
same lexical prefix test, with the same hole in it.

`internal/sysinfo` reads the host through gopsutil rather than parsing `/proc` by hand, so
the same code path works across kernels and inside a container with the host's `/proc`
bind-mounted.

### Auth, sessions and secrets

`internal/auth` owns users, sessions, TOTP, recovery codes and API tokens.
Session cookie is `vpsd_session` (HttpOnly, SameSite=Strict, Secure unless `JD_DEV`).
A password alone yields a *partial* session accepted only by the 2FA routes
(`AuthenticatePartial`); everything else rejects it with `totp_required` /
`totp_enrollment_required`. API tokens may narrow their creator's role, never widen it, and
are demoted with the account.

`auth.Sealer` (from the 64-hex `JD_MASTER_KEY`) encrypts every stored secret — TOTP seeds,
database connection strings, deploy env, backup credentials.

### State: SQLite, and what is in it

State is SQLite in `JD_DATA_DIR`. The schema is a single `CREATE TABLE IF NOT EXISTS` block
in `internal/store/store.go` with no migration tool, so schema changes must be **additive**
and tolerate an existing database.

The file is still named `vpsd.db` (`store.DatabaseFile`) through the "Just Dashboard" rename:
moving it would buy nothing and would strand every existing install's accounts, audit log and
encrypted secrets. `config.Load` looks for it by that name when deciding whether a pre-rename
data directory should be adopted.

Tables: `users`, `recovery_codes`, `sessions`, `api_tokens`, `audit_log`, `db_connections`,
`backup_jobs`/`backup_runs`, `deploy_projects`/`deploy_env`/`deploy_runs`, `watched_domains`,
a `settings` key/value table (`store.Setting` / `store.SetSetting`) and the three metrics
tables below.

`internal/audit` writes `audit_log` and mirrors every entry to the process log, so an
operator retains a trail even if the database is later tampered with. An `Entry` records who
(user id, name, role, and `Actor` — session or token), from where (IP), what (action, target,
method, path), and how it went (status, success, detail).

### Metrics history

`internal/metrics` samples the host on the server's own timer and stores the result in
SQLite, because the live socket can only ever describe the time since a browser tab was
opened — a dashboard whose charts start empty on every visit cannot show the spike that
happened overnight, which is most of the reason to have charts. `GET
/system/metrics/history` aggregates a window into at most N buckets **in SQL**, and every
series carries its bucket's peak next to its mean: a 100% second inside a ten-minute
bucket averages away to nothing, so a chart drawn only from means reports a quiet night
that was not quiet.

Capacity is recorded per filesystem in `metric_mount_samples` (`GET /system/metrics/storage`),
not as one worst-of line: when the fullest mount stops being the fullest, a single line drops
to whatever the runner-up was and reads as space freed on a disk that never changed.
Cardinality stays small because pseudo filesystems are filtered out before the write.

The same recorder samples every running container into `metric_container_samples` (`GET
/docker/containers/{id}/stats/history`, and `GET /docker/containers/stats/history` for a
sparkline per row of the container table in one query rather than one per row). That
series is keyed by container **name**, not id: a compose redeploy replaces the container
with a new id, and seeing across the restart is most of the point. Docker being
unavailable is not an error there — the recorder logs it once and carries on with the
host metrics. The recorder keeps its own `sysinfo.Collector` and its own
`dockerx.StatsSampler`, since rates are deltas against the previous call and sharing one
with the request handlers would let a one-shot `GET /system/metrics` shorten the interval
the next recorded rate is divided by.

Container network and block totals are stored as Docker's **cumulative counters** and
differenced into rates in SQL (`MAX - MIN` within a bucket, over the bucket width). A
total can be re-bucketed at any width later; a rate recorded against one interval cannot.

### Saturation, not just utilisation

Utilisation percentages answer "how busy", which is the question that stops being useful
exactly when something is wrong. The series that answer "is work waiting" are recorded
alongside them and are what most of the Overview page is now about:

- **CPU by mode** (`sysinfo.CPUModes`) — user, system, iowait and **steal**, deltas
  against the previous call. One "68% busy" figure cannot separate a server doing work
  from one waiting on a disk from one whose hypervisor is running another tenant on the
  core, and the response to each is completely different. On a VPS, steal is the one
  whose fix is not inside the machine.
- **Pressure** (`sysinfo.ReadPressure`, `/proc/pressure`) — the kernel's own share of
  time spent stalled. A kernel without PSI reports `Supported: false`, which the UI shows
  as "cannot tell" rather than as three reassuring zeroes.
- **Disk IOPS, service time and %util** — a device saturated by small random writes moves
  almost no bytes, so the byte rates alone describe an idle disk that cannot take another
  request. Recorded as the worst device, not the mean, for the same reason the capacity
  figure is the fullest mount.
- **Sockets** (`/proc/net/sockstat`, both families) — read as totals rather than by
  enumerating connections, which on a busy host is thousands of lines a sample.
- **Run queue** (`load.Misc`) — `Blocked` is the field worth having: read next to iowait
  it turns "the CPU is idle but everything is slow" into a sentence.
- **Inodes per mount** — a build server hits this ceiling first, on a filesystem every
  capacity chart calls half empty.

`metrics.Assess` turns all of it into a verdict (`GET /system/health`): findings carrying
what was measured, what it means and what to do, ranked worst-first. It runs on the
server because the thresholds are a claim the product is making and belong with the code
that records the data, and because the checks read an hour of history to tell a spike
from a trend — which is not work to repeat for every client wanting a badge. Memory is
judged on **available**, never on "used": Linux counts the page cache there, and judging
a server by it produces a permanent, meaningless warning.

### Why a line moved

`metrics.Events` (`GET /system/metrics/events`) is the annotation layer. Grafana expects
you to wire up a data source for this; the dashboard already *is* the thing that ran the
deploy, took the backup and restarted the box, so it answers from `deploy_runs`,
`backup_runs` and `audit_log` directly. Reboots are not stored anywhere and do not need
to be — a sample whose `uptime_seconds` is lower than its predecessor's can only mean the
machine went down in between, which also catches the restart nobody initiated from here.
It works with `JD_METRICS_RETENTION=0`: only the reboot markers go quiet.

### History the host already keeps

Not everything worth showing needs recording. `netsec` reads three records the machine
writes on its own — wtmp (`GET /logins`), btmp (`GET /logins/failed`) and fail2ban's log
(`GET /fail2ban/history`) — rather than polling and remembering. Polling a jail for its
banned set would invent the events between samples and miss every ban shorter than the
interval; the log is the record. btmp sits behind `system.admin` because it holds whatever
was typed at a login prompt, which is sometimes a password in the username field.

`netsec.Exposure` answers the one question the product actually rests on: who can reach this
panel. It grades the machine (`tailscale`, `tunnel`, `private`, `public`, `open`) from the
configured allowlist and the host's own interfaces, and offers a recommendation. The setting
lives in an env file nobody re-reads after install day, which is exactly why it belongs on
screen — a machine that quietly became reachable from the internet should say so rather than
wait to be discovered.

### Streaming

`internal/wsx` wraps gorilla/websocket: origin check on upgrade (a WS handshake is not
subject to CORS, so this is what stops a malicious page using the operator's cookie),
serialised writes, ping/pong, 1 MB read limit. Frames are `Envelope{type, data, error, ts}`,
consumed by `useSocket` on the frontend. Server-side filtering is the rule — log grep and
level filters are applied *before* lines are sent. A single container log line is bounded at
256 KB (`dockerx.maxLogLine`) so a container emitting a gigabyte without a newline cannot
exhaust the dashboard's memory.

### Keeping secrets from leaking sideways

Four separate places, all guarding the same thing:

- `main.scrubSecretEnv` unsets the boot-time secrets (both `JD_` and `VPSD_` names) once
  they have been consumed, so nothing spawned later can read them out of the environment.
- `deploy.mergeEnv` strips every `JD_*`/`VPSD_*` variable from a deploy child's environment.
  The command is content the repository owner controls; it has no business reading the
  dashboard's configuration. This is the half that keeps working when a new secret-bearing
  variable is added later.
- `dbx` never puts a database password in argv — `/proc/*/cmdline` is world-readable, so
  `pg_dump` gets `PGPASSWORD` in its environment and MySQL gets a temporary defaults file.
- `dockerx.RedactEnv` masks credential-shaped values in a container's environment, because
  container env is where deployments keep their secrets and the container detail view is not
  a `system.admin` route.

### Agent mode

`JD_AGENT_MODE` / `-agent` swaps the human login surface for mutual TLS: no password route,
no session, no 2FA; `httpx.HubOnly` admits only the enrolled hub's certificate, and
`/agent/enrol` is the one route reachable before enrolment. TLS asks every caller for a
certificate without *requiring* one at the handshake — requiring it would lock out
`/agent/enrol`, which by definition runs before the hub is trusted, so `HubOnly` does the
enforcement per route instead. The enrolment token is printed once per boot while the agent
is unclaimed, in the same spirit as the generated bootstrap password. The feature routes are
the same program either way — only who may reach them changes. Not useful standalone yet.

### Configuration

`internal/config` resolves everything from `JD_*` environment variables at boot and **fails
closed**: no `JD_ALLOWED_CIDRS` with a non-loopback bind is a startup error, and a missing or
malformed `JD_MASTER_KEY` is fatal. A `loader` collects *every* malformed setting so `Load`
can refuse to start with the whole list rather than one error at a time. `config.Env` falls
back to the legacy `VPSD_*` prefix, and `adoptLegacyData` picks up pre-rename
`/var/lib/vps-dashboard` data — an install that predates the rename must keep working.

Most variables are read in `config.go`. A handful are not, and looking for them there is a
dead end: `JD_LOG_LEVEL` and `JD_BOOTSTRAP_USER`/`JD_BOOTSTRAP_PASSWORD` are read in
`cmd/server/main.go`, and `JD_SITE` belongs to the proxy (`deploy/Caddyfile`), never to the
backend.

Wherever a variable is read, its documentation lives in four places that must stay in step:
the reading site, `.env.example`, `docker-compose.yml` and the README's configuration table.

### Deployment topology

```
browser ──(Tailscale / SSH tunnel)──▶ Caddy :8443
                                        ├─ /api/* ─▶ backend :8080  (loopback)
                                        └─ /*     ─▶ frontend :3000 (loopback)
```

Caddy (`deploy/Caddyfile`) is the only listener bound to anything but loopback, and it binds
`{$JD_SITE}` **plus** loopback explicitly — site addresses alone would leave Caddy listening
on every interface. Serving UI and API from one origin is load-bearing: `SameSite=Strict`
cookies and the same-origin WebSocket check both depend on it. Caddy also rewrites
`X-Forwarded-For` to the real client address, which is what makes `JD_TRUSTED_PROXIES` safe;
`flush_interval -1` and zero read/write timeouts are what keep the long-lived streams alive.

The backend container runs `privileged`, `pid: host`, `network_mode: host` with the Docker
socket and real host paths mounted at their real names — remove a mount and the file manager
silently browses the container's own empty filesystem.

## Frontend

App Router, all pages `"use client"`, one page per feature under
`src/app/(dashboard)/<feature>/page.tsx`. Seventeen routes plus `/login`.

### The shell

`(dashboard)/layout.tsx` is the authenticated shell and owns four things:
`CommandPaletteProvider`, `SidebarProvider` + `AppSidebar`, `TopBar`, and `MetricsStream`
(which renders nothing and exists to hold the metrics socket open for the whole shell, so the
Overview charts and the top bar's vitals keep filling while you are on another page). Its
redirect to `/login` is convenience, not a control — every API call behind it is
independently authenticated by the server.

The scroll container lives on the `SidebarInset`, not on the document. That is what pins the
top bar and what lets a page ask for the remaining height (`<Page fill>`) instead of growing
past the viewport.

`components/app-sidebar.tsx` exports the nav registry itself — `NAV` (grouped) and
`PERSONAL_NAV` — so `command-palette.tsx` can offer the same destinations without a second
list to keep in step. Items may carry a `capability`, and the sidebar hides what the role
cannot use. `command-palette.tsx` is ⌘K: every page plus every theme, because a server
dashboard is navigated by someone who already knows where they are going.

### The design system

Two files define the whole visual language, and pages compose them rather than hand-rolling
layout:

- `components/page.tsx` — `Page` (one measure, one gutter, one vertical rhythm; `fill` for
  the terminal and log pages whose content *is* the viewport), `PageHeader` (eyebrow, title,
  description, actions), `Section`, `Toolbar`, `SearchInput`, `Metric`/`MetricStrip`,
  `DetailList`/`Detail`.
- `components/panel.tsx` — `Panel`, `PanelHeader`, `PanelToolbar`, `PanelBody`,
  `PanelFooter`, `Well`. A panel is *the* content block: a framed surface with a tinted
  header strip and a hairline under it, so it reads as "chrome, then content" at a glance and
  a toolbar or a full-bleed table can sit flush beneath the header without inventing a second
  edge. Every page is a stack of these.

`min-w-0` on the frame and its children is load-bearing: a wide table's intrinsic width would
otherwise widen the flex column and take the whole shell sideways instead of scrolling inside
the panel that owns it.

Before these existed, each page assembled its own `Card`/`CardHeader`/`CardContent` with
whatever padding and title size its author picked, which is why fourteen pages read as
fourteen products. **Reach for `Panel`/`Page`, not raw `Card`, and add a variant here rather
than a one-off in a feature page.** `components/state.tsx` covers the non-happy paths the
same way: `Spinner`, `LoadingRows`, `LoadingPanel`, `EmptyState`, `ErrorState`, `Notice`.

`components/ui/*` is generated shadcn/ui (new-york, zinc, lucide, 36 primitives). Prefer
composing over editing these; feature-specific pieces live in `components/<feature>/`
(`docker/`, `files/`, `metrics/`, `procs/`).

### Charts

`components/metrics/` is the third design-system file in all but name, and every chart in
the product goes through it rather than assembling its own recharts tree — which is how
the old Overview page ended up with three tooltip formats and no way to compare a moment
across its four charts. Adding a measurement should mean naming a series, not rebuilding
a chart.

- `metric-chart.tsx` — `MetricChart` plus the `Series` descriptor. The x-axis is
  **numeric over time**, not a category axis of pre-formatted labels: a category axis
  spaces every bucket equally, which lies whenever the record has a hole in it. It owns
  the shared crosshair, drag-to-zoom, event markers, threshold lines and one tooltip
  listing every series at the hovered instant.
- `chart-panel.tsx` — `ChartPanel`, the header/chart/legend shape every metric panel
  takes, including the empty state. A series with no numbers anywhere in the window is
  dropped rather than drawn flat at zero, which is what makes a kernel without PSI say so
  instead of reporting three healthy zeroes.
- `series-legend.tsx` — min/mean/max/last per series, and **"At cursor"**: while the
  pointer is over any chart on the page, every legend switches to the value its series
  held at that instant. Max reads the peak column where a series has one, since the
  maximum of the *means* is exactly the figure a downsampled window hides.
- `sparkline.tsx` — a bare SVG path for a table cell. Not recharts: a table of forty
  containers would otherwise mount forty responsive containers and resize observers to
  draw forty polylines.
- `range-picker.tsx`, `health-panel.tsx` — the window control (with pan and zoom-out,
  which appear only once a window has been dragged) and the health verdict.

`lib/metrics-crosshair.ts` holds the hovered instant outside React, for the same reason
the live buffer is outside React: a pointer crossing a chart fires continuously, and
routing that through a context above the router would re-render the terminal and the log
tail on every mousemove. The value is a **timestamp**, not a row index — the charts on a
page do not share a row array.

### Data

- `src/lib/api.ts` is the only fetch layer: `get/post/put/patch/del`, `credentials: "include"`,
  `X-Confirm` passthrough, `ApiError` with `needsConfirmation` / `isAuthProblem` / `needsTotp`.
  `wsUrl()` and `downloadUrl()` build the non-JSON URLs.
- `usePoll` — abort-per-run so a slow endpoint cannot stack requests, paused on a hidden tab.
- `useMetricsWindow` — the window the charts cover, as a **stack**: zooming is
  exploratory, so the way back out of five minutes is to the hour it was inside, not to
  the day you started from. Deliberately component state, not a stored preference — the
  named range is a standing choice worth remembering, a zoom is a question being asked
  now, and restoring yesterday's zoom would show an empty window with no obvious way out.
- `useMetricEvents` / `useHealth` — the annotation and verdict layers, polled on their
  own much slower cadences.
- `useSocket` — reconnect with backoff (these sockets ride a VPN tunnel that drops routinely),
  handlers kept in a ref so a fresh closure does not rebuild the socket.
- `src/lib/types.ts` mirrors the backend's JSON shapes by hand, including the `Capability`
  union — it drifts if backend types change without it.
- `useAuth`'s `can("capability")` predicate hides controls a role cannot use. This is UI
  affordance only; the server re-decides every request.
- `ConfirmDialog` collects the typed phrase and passes it to the API call; the server
  re-checks it.

Metrics are the one place with machinery beyond those hooks:

- `lib/metrics-store.ts` keeps the live series **outside React**. Owning five minutes of
  history in a route component threw it away on navigation, and pushing a 2s frame through a
  context above the router re-rendered the terminal and the log tail twice a second. Explicit
  subscribers mean only the components reading metrics re-render. The buffer is mirrored to
  sessionStorage so a reload keeps its chart, and points older than `STALE_MS` are dropped on
  the way back in — a graph that silently stitches this minute onto one from an hour ago is
  worse than one that starts empty.
- `lib/metrics-range.ts` defines the windows (`live`, `1h`, `6h`, `24h`, `7d`), how many
  buckets each asks the server for, how often it re-reads, and the `MetricsWindow` a
  dragged span becomes — a zoomed window is a fixed span in the past, so it is fetched
  once and never re-polled. Live and recorded data are
  **never spliced into one line**: the cadences differ by two orders of magnitude, and a chart
  drawing twenty coarse points and a hundred fine ones at equal spacing lies about when things
  happened. Container charts offer only the recorded ranges, since nothing accumulates a
  container's stats in the browser.
- `hooks/use-metrics.ts` (`MetricsStream`, `useMetrics`) and `hooks/use-metrics-history.ts`
  (`useMetricsHistory`, `useStorageHistory`, `useRangePreference`) are the React surface over
  those two.

### Theming

Twelve palettes, nine dark and three light. `lib/themes.ts` is the registry — id, name, blurb,
mode — and `app/themes.css` holds one `[data-theme="<id>"]` block per entry. **Colour is
defined only in that CSS**; the registry carries no values, so switching themes is a single
attribute write on `<html>`.

`themeBootstrapScript()` is generated from the registry and inlined in `<head>` by the root
layout, so the stored choice is applied before first paint — reading it after hydration would
flash a full screen of near-black at anyone who chose a light theme, on every navigation that
reloads the document. `<html>` carries `suppressHydrationWarning` for exactly that divergence.
`hooks/use-theme.tsx` treats the document as the store (`useSyncExternalStore` over
`data-theme`) rather than holding a second copy to synchronise in an effect. The choice lives
in localStorage, not on the account: it belongs to the screen you are sitting at.

Adding a palette means an entry in `THEMES` **and** a matching block in `themes.css` — the
bootstrap script derives its id list from the registry, so a registry entry with no CSS
renders unstyled and CSS with no entry is unreachable.

## Conventions

- **Comments explain why, not what.** The existing prose in this codebase is unusually
  dense with rationale — match it, and when you change a behaviour that a comment justifies,
  update the reasoning rather than deleting it.
- **Commit messages are imperative sentences describing intent**, not conventional-commit
  prefixes: "Report on the server, not on the container it runs in", "Stop handing out the
  master key, and bind the proxy where it says it does".
- Prettier for TS/TSX: no semicolons, double quotes, `printWidth: 100`, trailing commas.
- Go: standard formatting, no extra linter config.
- Licence is AGPL-3.0 and contributions carry an additional grant to the owner — see
  CONTRIBUTING.md before touching licence headers or adding dependencies.

## Invariants that must not regress

A change that weakens any of these has to say so explicitly:

1. The network allowlist runs **before** authentication.
2. Two-factor is mandatory; a password-only session reaches nothing but the 2FA routes.
3. Irreversible actions require the typed `X-Confirm` phrase, enforced server-side.
4. Capability checks live on the route, never in the UI alone.
5. Every state-changing request lands in the audit log.
6. Client-supplied paths go through `files.Resolve`; host commands go through `hostexec`
   with an argv, never a shell string. The one shell in the codebase is
   `deploy.Deployer.shell`, and it is deliberate: those are pipelines an admin stored for
   their own project ("bun install && bun run build"), not anything supplied per request.
   Do not add a second one, and do not "fix" that one into an argv.
7. Nothing but Caddy binds a routable address.
8. Store schema changes are additive and tolerate an existing database. There is no
   migration tool: `CREATE TABLE IF NOT EXISTS` is a no-op against a database that
   already has the table, so a **column** added later goes in `store.addedColumns` as
   well, which `applyAddedColumns` ALTERs in at open. Every entry needs a `DEFAULT`
   (SQLite refuses a NOT NULL column on a table with rows without one) and no entry is
   ever removed — the list is the path from every shipped schema to the current one, not
   a description of the current one.
