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

Seventeen packages carry tests (`agent`, `api`, `config`, `dbx`, `dockerx`, `files`, `gitx`,
`httpx`, `jobs`, `metrics`, `netsec`, `procs`, `proxysvc`, `safepath`, `selfupdate`, `term`,
`updates`). They are all fast and hermetic — none needs Docker, systemd or a network — so `go test ./...` is a reasonable thing
to run on every change. There are two exceptions, and both take the same shape: they drive a
real thing outside this process and *skip* when it is absent, so the suite stays green on a
bare machine and gets stricter on an equipped one.

The first is the live database tests (`dbx/live*_test.go`, `api/handlers_db_live_test.go`),
which take each engine's DSN from an environment variable defaulting to a local instance. A
catalogue query naming a column the server does not have is string-matched identically by a
unit test; only the server rejects it. Bring the engines up and re-run with `-count=1` — the
test cache will otherwise serve you yesterday's skips.

The second is `term` and the terminal half of `api`, which drive the machine's real tmux.
The bugs they exist to catch live in the gap between this process and that one — what tmux has been told, what it has got round to
storing, and what it reports half a second later — and a fake tmux would answer instantly and
pass every one of them while the product stayed broken. Both packages give themselves a
private tmux server in `TestMain` (`TMUX_TMPDIR`), so a test run never lists or outlives the
operator's own sessions and two packages running concurrently under `go test ./...` cannot see
each other's. The ones guarding the security invariants (`httpx/confirm_test.go`,
`api/routes_test.go`, `api/docker_spec_test.go`, `files/files_test.go`,
`safepath/safepath_test.go`, `dbx/classify_test.go`, `api/handlers_security_test.go`) are the
ones to extend when you touch that surface. The last of those signs a real admin in — password,
TOTP enrolment, second factor — and drives the security and proxy routes through the whole
chain, because a rule tested in its own package says nothing about whether the route it backs
was mounted inside the group it was meant to be in.

`dockerx/diagnose_test.go`, `netsec/posture_test.go` and `proxysvc/tlsscan_test.go` are the
other kind: they pin the *claims* the product makes — what is wrong with a container, whether
a host is hardened, what a TLS grade means — which are as easy to get backwards as any check
and considerably more embarrassing. `proxysvc/sites_test.go` pins the rendered nginx, including
the round trip: the form reads a file back, so anything the renderer emits and the parser
cannot recognise is a field silently dropped the next time somebody saves.

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
`NEXT_PUBLIC_WS_BASE=http://localhost:8080`; the WebSocket origin check then needs
`JD_ALLOWED_ORIGINS=http://localhost:3000` on the backend, since the page and the socket are
no longer the same origin the way they are behind Caddy.

`bun dev` and `bun run build` both run `scripts/sync-monaco.mjs` first, which copies the code
editor into `public/`. Running `next` directly skips that hook and leaves every editor in the
product spinning — see "The editor is served from here, not from a CDN".

There is no frontend test suite; `bun run build` and `bun run lint` are the whole gate.
(`playwright` sits in devDependencies for ad-hoc screenshotting and is wired to no script.)
`next.config.ts` sets `output: "standalone"` for the Docker image and `agentRules: false` —
this repo keeps its own instructions and the generated ones are noise.

### Whole stack

```bash
sudo ./install.sh                   # interactive first install; re-runnable, keeps .env
docker compose up -d --build
docker compose logs backend | grep "bootstrap admin"   # generated password, printed once

scripts/release.sh 0.6              # cut a release — see "Cutting a release" below
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

`s.destructive(r, ...)` is the marker for "destructive" and adds the capability check and the
tighter `destrLim` budget — it does *not* enforce confirmation. The **rare and unrecoverable**
subset additionally requires the caller to echo a phrase in the **`X-Confirm`** header via
`httpx.RequireTypedConfirmation(w, r, phrase)`, called **inside the handler** where the
expected phrase is known; the phrase comes back to the client as `error.phrase`, not parsed
out of the message. Which routes are in that subset, and why the test is frequency rather than
severity, is invariant 3 — read it before adding one.

`s.destructive` is nested inside stricter groups too (`system.admin` routes that delete
something), because admin holds every capability, so "which routes are destructive?" has one
answer. The single exception is `POST /databases/{id}/query`, where the answer depends on the
SQL: `dbx.Classify` decides, fails closed on anything it does not recognise, and the handler
applies the same capability check and budget by hand.

Two routes carry a check on the *content* of the request rather than on its path, for the
same reason: the route cannot know. `POST /docker/containers` gates on `service.control`,
and `api.authoriseSpec` additionally demands `system.admin` for a spec that is privileged,
adds capabilities or devices, uses the host's network, or bind-mounts a path — those are the
settings that turn "may run a container" into "owns the server", and there is no way to grant
one without the other. The streaming compose runner
(`GET /docker/stacks/{name}/run?action=…`) decides its capability and confirmation from the
action, using the same `composeIsDestructive` set the POST routes use so a socket cannot
become a way around either.

That socket is also the one place the confirmation phrase may arrive as a query parameter,
through `httpx.RequireTypedConfirmationWS`: a browser cannot set a header on a WebSocket
handshake at all, so the alternative was leaving `compose down` as a request that hangs for a
minute with no output. The header's cross-origin guarantee is replaced there by `wsx`'s
origin check, which rejects a handshake from any page this dashboard did not serve.

Handlers live in `api/handlers_*.go`, one file per feature, each with its own
`mount<Feature>Routes(r chi.Router)` called from `Routes()`. `handlers_domains.go` is the one
that does not own a mount function — watched-domain certificate checks are part of
`mountProxyRoutes`, under `/certificates/watched`, because that is where they are used.
`handlers_docker_manage.go` is the other: the Docker write surface is large enough to be its
own file but is mounted from `mountDockerRoutes`, so the route map for Docker stays in one
place. Shared request plumbing that belongs to no feature (`atoiDefault`, `timeoutCtx`,
`recordAudit`, `detachedContext`) lives in `api/helpers.go`.

### Server, modules, degradation

`api.Server` holds the dependency graph — config, logger, store, auth service, sealer, audit
logger, authenticator, WS upgrader, the three limiters, and in agent mode the `agent.Identity`.
`api/modules.go` (`moduleSet`) holds the feature backends: `sys`, `metrics`, `docker`,
`dockerStats`, `dockerEvents`, `pm2`, `systemd`, `table`, `cron`, `logs`, `term`, `files`, `git`, `github`, `updates`,
`selfUpdate`, `proxy`, `dbs`, `linuxUsers`, `netsec`, the three backup pieces (`backupStore`, `backupRunner`,
`backupSched`) and the two deploy pieces (`deployStore`, `deployer`).

Each module is optional by design: a host with no Docker socket, no systemd or no fail2ban
still serves everything else, and the affected routes return a precise "unavailable on this
host" code that the frontend renders as information rather than an error (see `ErrorState` in
`frontend/src/components/state.tsx`).

`Server.Start(ctx)` is separate from `New` so a failure to schedule background work is
reported by `main` rather than swallowed during construction. It starts the metrics recorder,
the Docker event log, the self-update check and the backup scheduler — and it is also where
`selfupdate.Installer.Reconcile` settles an upgrade that was in flight when this process
began, which after a successful upgrade is every time. The recorder is started here rather than lazily on first request
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

### The verdict, not the settings

`netsec.Assess` (`GET /security/posture`) is to the host's security what `metrics.Assess` is
to its load and `dockerx.Diagnose` is to its containers, and for the same reason: every panel
in this class shows the facts — a rule list, a jail, a set of sshd directives, a table of open
ports — and leaves the reading to somebody who already knows how, which is not the person who
needs the answer. The hosting panels that do take a position sell a score out of a hundred,
which is a number to optimise rather than a thing to fix.

So a `SecurityFinding` carries what was measured, what it means and what to do as three
separate fields, plus a `Fix` naming a remedy the dashboard can carry out itself. It is a
**pure function of its inputs**: the handler gathers exposure, firewall, fail2ban, sshd,
listeners, certificates, failed logins and pending updates concurrently, and `Assess` decides.
That is what makes the rules testable without a firewall, an sshd or a network, and
`posture_test.go` pins each claim — including the two that are easy to get backwards. An
exposed database behind a default-deny firewall is a warning rather than a critical, because
saying otherwise is crying wolf; and "turn off password authentication" stops being offered as
a one-click fix when no account on the host has an authorized key, because there it is not
advice, it is a lockout.

Three inputs are shapes declared *in* netsec (`ExposedPort`, `CertSummary`) rather than
imported from `proxysvc`, so the audit keeps no dependency on how ports or certificates are
discovered.

### Package managers, plural

`internal/updates` was `apt-get` and nothing else, which meant every RPM,
Alpine and Arch host reported no package manager at all — and "no package
manager" renders as "nothing to update" rather than as "never checked". The
posture audit's pending-patch check was therefore silently dead on half the
servers this runs on.

It is a `manager` interface now: apt, dnf, yum, zypper, pacman and apk, each a
listing command parsed by a pure function plus an upgrade argv. Two details are
load-bearing. `dnf check-update` **exits 100 when there is something to do**, so
treating a non-zero exit as failure reports an error for a host with updates and
success for one without — exactly backwards. And Alpine and Arch publish no
advisory data at all, so `SupportsSecurityOnly` is false there,
`Report.SecurityFiltering` tells the UI to say "cannot tell" rather than "none
outstanding", and `guardSecurityOnly` refuses a narrowed upgrade instead of
quietly applying everything.

Reboot detection follows the same shape: Debian's flag file first, then
`needs-restarting -r` for the RPM world, then "cannot tell" reported as false.

### sshd, and the guard that makes it offerable

`netsec/sshd.go` reads the **effective** configuration — `sshd -T`, falling back to parsing
the file — because a file that sets `PasswordAuthentication` twice does not behave the way it
reads. Two of sshd's own semantics are load-bearing and get read backwards by anyone treating
the file as an ini: the **first** value for a keyword wins, not the last, and everything after
a `Match` applies conditionally. So the parser never overwrites an earlier value, a `Match`
ends the file for it, and the fact that one exists is reported rather than dropped.

`sshd_apply.go` writes. The order is the proxy editor's, for the same reason: refuse the
changes that are certainly a lockout, write, validate with `sshd -t`, restore on failure,
reload only then. The write target is a drop-in under `sshd_config.d` **only when the main
file includes that directory before setting anything itself** — position is the whole question,
since first-value-wins means a drop-in included at the bottom is a file this dashboard wrote
and sshd ignored, which is the worst possible outcome for a security setting. Otherwise the
directive is replaced where it stands in the main file and later duplicates are commented out
rather than deleted.

`guardSSHLockout` is deliberately narrow, like the firewall's: it refuses only the certain
cases — passwords off with no key anywhere, both passwords and keys off, root the only keyed
account with passwords off. Disabling passwords on a host where somebody has a key is the
correct thing to do and must never be blocked.

The directive list is **closed**. Every entry is a line this code is willing to write into
sshd_config, and an open set would make the endpoint a config editor with extra steps — one
that can take the machine off the network.

Three of its entries need their own guards. `Port` is bounded by `LegalMin`/
`LegalMax` rather than by the `Min`/`Max` that carry the *recommendation* — the
two were one pair of fields and a port's range was consequently treated as
advice and never enforced. Moving the port also asks the firewall first:
`guardSSHPort` refuses a default-deny firewall with no rule for the new port,
which is the same class of mistake as turning off passwords with no key.
`AllowUsers`/`DenyUsers` are `kind: "list"`, which means spaces — so the value
is checked for a newline explicitly (one would write a directive of the
caller's choosing onto the next line) and normalised through `strings.Fields`
before it is written. An emptied list is commented out rather than written as a
keyword with no argument, which sshd refuses to start behind.

`netsec.Disconnect` ends an interactive login. The PID is matched against the
live session list before anything is signalled; without that check the route is
a "kill any process on this host" primitive wearing a sensible name. SIGHUP
rather than SIGKILL, so the login is recorded as ended rather than as a process
that vanished.

### One firewall page, three firewalls

`netsec/firewall.go` is a dispatcher, not an implementation. `fwBackend` is the
interface and there are three: ufw (`firewall_ufw.go`), firewalld
(`firewall_firewalld.go`) and raw iptables (`firewall_iptables.go`). Validation
and both lockout guards live in the dispatcher rather than in any backend, so a
fourth cannot be added without them — that placement is the whole reason the
refactor was worth doing.

firewalld's model is genuinely different and the difference is the work. There
are no rule numbers: a zone holds services, ports and rich rules, each removed
by handing back the exact thing that was added. Numbers are assigned
positionally by `Service.Status` so "delete rule 4" means something, and
`Rule.Handle` carries what the backend actually needs — it is never serialised
and never accepted from a client. Everything is written `--permanent` and
reloaded, because a runtime-only rule disappears at the next boot.

iptables is **read-only on purpose**. Writing rules there is the easiest of the
three; the problem is that iptables has no persistence of its own, so a rule
added from here would work until the machine rebooted and then vanish, leaving
a page that says protected in front of a host that is not.
`FirewallCapabilities` is how that reaches the UI: each backend declares what it
can do, the page hides the rest, and `ReadOnlyReason` explains the absence. A
greyed-out button with no reason is worse than one that is not there.

`annotateRule` attaches the catalogue's name and warning centrally, so
firewalld's rules read the same way ufw's do.

### Firewall: the parts that decide what the rules mean

A rule list on its own is half the picture. `FirewallStatus` now carries the three default
policies structured (`DefaultPolicy`) and the logging level, because a list of allows in front
of a default of *allow* is decoration, and a firewall that drops silently leaves an incident
with no record of what was refused.

Reading them needed a fix rather than an addition: `ufw status numbered verbose` looks like it
should work and is rejected outright — ufw's parser takes exactly one of the two words and
raises "Invalid syntax" for both. The old single call therefore returned an error, no rules,
and a firewall the page reported as inactive on **every host that had one**. It is two calls
now: `numbered` for the rules with the numbers the delete route needs, `verbose` for the
policy block, with the second a soft failure.

`AppProfiles` is per backend: ufw expands `app list` with an `app info` call
each, firewalld returns its predefined services by name only — resolving ports
would be one subprocess per service, and it ships several hundred. That size
difference is why the picker in the rule form is searchable rather than a plain
select.

`ServiceCatalogue` (`GET /security/services`) is the teaching layer for the rule form, the way
`GLOSSARY` is for Docker: a short list of the ports a single-server operator actually opens
plus the ones they open by accident, each with a `Danger` line for the ones that should never
face the internet. It is served from here rather than duplicated in TypeScript so the warning
the form shows and the finding the audit raises are the same claim. `parseUFWRule` attaches it
to each rule — but only warns when the source is unrestricted, since the same port limited to
a private range is the arrangement being recommended.

`ReplaceRule` is the edit. Neither ufw nor firewalld has one — a rule is a line, and changing
it means writing another and removing this one — so the order is the whole safety property:
the replacement goes in **first**, and only then is the original removed. Deleting first and
failing to add would leave a hole in the firewall, which is the one outcome an edit must never
produce; failing the other way round leaves two rules, which is visible in the list and no
less strict than before. ufw is ordered, so the replacement is inserted at the old rule's
number and the original — pushed down one by the insert — is deleted at `number+1`. firewalld
has no numbers of its own (the position in the listing is this dashboard's), so the old rule's
`Handle` is resolved *before* anything is added, because adding changes what position N points
at. The ordering lives in `replaceRule`, separate from backend detection, so it is driven by
tests against a recording backend rather than discovered on a live firewall.

`AddRule` gained `insert` (ufw stops at the first match, so a deny added after a broad allow
does nothing at all, which looks exactly like a deny that works), application profiles, and
comma-separated port lists. `SetDefaultPolicy` refuses an inbound deny on a host whose rule
list admits nobody; the ambiguous cases go to the typed confirmation, because a rule list that
admits *something* cannot be judged from here without knowing which port the browser arrived
on.

Jail tuning is applied to the running server **and** written into
`jail.d/99-just-dashboard.local`, because fail2ban reads that directory last and
a runtime-only change is gone at the next restart — the same trap the jail
start/stop control was rejected for. `mergeJailOverrides` rewrites one section
and leaves every other jail, and any hand-written line inside the section it
owns, exactly as it was.

There is deliberately **no start/stop for a fail2ban jail**. `fail2ban-client status` lists
only running jails, so one stopped from the UI would vanish from every listing with nothing
left to start it again — a control that can only be used once is a trap.

### Docker: what it can do, and why each piece is where it is

`internal/dockerx` talks to the Engine over its socket through the official SDK. Nothing
shells out except where the Engine genuinely has no equivalent, and those exceptions are
named below.

**Creating and replacing containers.** `dockerx.ContainerSpec` is the dashboard's description
of a container to run, not `container.Config` plus `container.HostConfig` — those are the
Engine's shape, split across the pair on the historical accident of which fields the daemon
could change after creation, and rendering them as a form is how Portainer's create page
became twelve accordions that assume you already know Docker. `toEngine` does the
translation and returns warnings for the choices that are legal and probably unintended: a
port on every interface, no memory limit, an anonymous volume, a writable bind mount of a
sensitive path. `SpecOf` reads a container back into that shape, which is what makes
duplicate, edit-and-recreate, and "save this as a stack" possible at all.

`Recreate` is the one Docker has no verb for. Every field but a handful of resource limits is
fixed at creation, so editing means destroy-and-recreate — which is fine right up until the
create fails and the operator has nothing where their service was. The old container is
therefore renamed aside (`<name>_jd_replaced`), and restored if anything after that point
goes wrong; it is removed only once the replacement is running. A compose-managed container
is refused with `ErrComposeManaged` and the stack's name, because recreating one behind
compose's back leaves the project describing a container that no longer exists.
`UpdateResources` is separate because it is the exception: limits really can be changed in
place, and an operator who set the wrong number should not have to destroy the container.

**`render.go` exists so the form is not a black box.** It turns a spec back into the
`docker run` line and the compose service that would produce it. Both are rendered on the
server so there is exactly one implementation of "what does this spec mean" — a second one in
TypeScript would drift, and the version that mattered would be the one nobody was reading.
The compose half is also the bridge: a container created by hand exists only in the daemon's
memory, and the same container as a file can be committed, diffed and redeployed. The YAML is
hand-written rather than marshalled, because key order carries meaning to a reader and a
marshaller would sort it alphabetically into something correct and unreadable.

**`diagnose.go` is the part nothing else in this class has.** Every Docker panel shows the
same facts — a state, an exit code, a restart count — and leaves the reading to you. This
turns them into sentences: what 137 means and what the limit was, that a container has
restarted twelve times in the last minute, that a health check is failing and what it last
said, that a port is published in front of the firewall, that a json-file log with no rotation
has reached 800 MB, that the data being written is in the container rather than a volume and
will not survive the next update. Findings carry a `Level`, the reasoning, and — where the
dashboard can carry out the remedy itself — an `Action` the UI turns into a button. The rules
are deliberately conservative: a panel that cries wolf gets ignored wholesale, so nothing
fires on a container that is merely unusual. `diagnose_test.go` pins the claims that would be
embarrassing to get backwards, including the two silences (a finished one-shot job, a
loopback-bound port).

**`events.go` keeps what Docker throws away.** The daemon emits an event for everything it
does and stores none of them, so the answer to "why did this restart at 04:00" is nowhere.
The same argument `internal/metrics` makes about samples applies: the dashboard is a
long-running process already connected to the thing producing the record. The buffer is an
in-memory ring — an event log worth keeping across restarts belongs in the audit table, which
already holds everything the dashboard itself did. `describeEvent` renders the object/action
pair as a sentence once, here, rather than in every component that shows an event. `oom` and
`health_status: unhealthy` are the two that justify the feature on their own.

**Update detection.** `CheckUpdate` asks the registry what a tag points at now and compares it
against the digest that was pulled — a different and more useful question than "is there a
newer tag", because it catches the case that matters, a moving tag that moved. Answers are
cached for 30 minutes and fetched by a four-worker pool, because Docker Hub rate-limits
manifest requests by address and a dashboard left open on a screen should not burn the host's
quota. A registry that cannot be reached, or wants credentials, is reported as `unknown` with
the reason attached; an image built locally is `local`, which is not a failure.

**Compose.** `RunCompose` (blocking) still backs the simple POST actions. `RunComposeStream`
is what the UI uses: the same commands with their output forwarded line by line, because `up`
on a stack that pulls and builds takes minutes and a request that hangs for minutes is
indistinguishable from a broken dashboard. `composeSteps` maps an action to its commands, and
two of them are sequences — `update` is a pull followed by an up, in that order, so a registry
that is down leaves the running stack alone rather than taking it half down. `ValidateCompose`
feeds a candidate file to the compose parser on **stdin**, so a syntax error never touches
disk; `WriteComposeFile` writes through a temporary file in the same directory and keeps the
previous content as `<name>.bak`, which guards the failure validation cannot catch — a correct
file that says the wrong thing. `DeclaredServices` is read on demand rather than in the stack
list because it costs a subprocess per stack, and it supplies the one fact the container list
cannot: a service the file declares that has no container is invisible everywhere else in
Docker.

**`Build`** drives the `docker` binary rather than the Engine's build endpoint. BuildKit is a
separate builder the classic API path does not reach, and silently building with the legacy
one would produce images that differ from what the same Dockerfile produces from a shell —
different caching, no mount or secret support. Being wrong in a way that only shows up later
is worse than shelling out. Argv is built explicitly and never passed through a shell, as
everywhere else.

**Efficiency rules that are load-bearing.** `ListContainers` carries `Mounts` because the
Engine's summary already has them and the volumes view needs "what uses this volume" for
every volume at once — the alternative is an inspect per container on every poll.
`ListStacks` uses `ListContainers` rather than a filtered listing of its own so it inherits
the health and uptime already resolved there. `Diagnose` inspects each container once and runs
every rule against that one payload.

### The terminal, and what keeps a session

`internal/term` runs the PTYs. Three properties are load-bearing and easy to
break:

**A session outlives everything but being closed.** With tmux present the shell
runs inside `tmux new-session`, so closing the tab, leaving the page and
restarting the dashboard all leave it running — only `Kill` ends one, and that
is behind a typed confirmation. The reaper detaches an idle persisted session
after `idleDetach` to give back the PTY and the slot; it never kills one. A
host without tmux has no third option, so there the reaper still kills, which
is why the page says which of the two worlds it is in.

**`su -l` cannot open a shell in a chosen directory**, because login *is*
chdir-to-home. Setting tmux's `-c` is not enough — tmux puts the pane in the
right place and su walks straight back out. `loginArgv(shell, keepCWD)`
therefore moves the chdir off su and onto the shell: `su -s <shell> <user> --
-l` switches user without `-l`, so it does not move, and the `-l` after `--`
reaches the shell and still reads the profile. The other half is
`hostexec.CommandOnHostInDir`: `nsenter` resets the working directory to the
target namespace's root, so `cmd.Dir` is silently discarded and only nsenter's
own `--wd` survives.

**tmux is the store for everything the operator chose.** The title, the folder
and the favourite flag live on the tmux session as user options (`@jd_title`,
`@jd_folder`, `@jd_fav`), not in this process and not in SQLite. The same
property that keeps the work — the session outliving the dashboard — is what
keeps its name, with nothing to migrate and nothing to reconcile on restart.
`Reattach` reads the title back, so a session picked up after a restart is
still called what you called it rather than `vpsd-3f2a91c4`.

Two details of the tmux listing are not optional. **tmux escapes non-printable
bytes in format output**, so a 0x1f unit separator comes back as the four
literal characters `\037` and every line parses as one field — `fieldSep` is
printable for that reason. And the **path is always the last field**, read with
`SplitN`, because a directory may contain the separator and the fields before
it may not (a session name is `vpsd-` plus hex, three are numbers, and titles
are sanitised on write).

`GET /terminal/` returns one list of *workspaces* rather than live sessions
plus detached names. They are the same thing in two states — `live` says
whether this process is holding a PTY — and reconciling two lists in the
browser is what made an idle session appear to vanish and reappear elsewhere.

**The listing answers from memory for a live session, and from tmux for the
rest.** tmux stores the title, folder, colour and favourite flag, and it is
still the store — but it cannot answer for a session it has only just been
asked to create: `tmux new-session` has been handed to a PTY and the
`set-option` that files the session away lands up to half a second later. The
page refreshes the instant the POST returns, which is inside that window, so a
shell opened *into* a folder appeared under "Other" and jumped into place on
some later poll — reading, from the operator's side, as two sessions swapping
groups. `Session` therefore shadows the four values, seeded by `Create`, read
back by `Reattach` and written by `SetMeta`, so for a session this process
holds the copy is never behind and is sometimes ahead. `Manager.Meta` and
`Manager.AllMeta` are that reconciliation for callers that need one session or
all of them; anything acting on "every session in this folder" must use
`AllMeta`, or it silently skips the newest one.

`SetMeta` follows the same rule: it writes memory first, and if tmux refuses
because the session does not exist yet, it retries in the background rather
than failing. Filing away the session you just opened is the commonest thing
anybody does, and it must not be the one case that errors.

**Folders are the dashboard's record, not tmux's.** They are the one piece of
organisation with no tmux object to hang off — a folder exists because some
sessions name it — which was fine while a folder was only a string and stops
being fine once it has an order, a colour and the ability to exist while empty.
So `api/handlers_terminal_folders.go` keeps the list in the `settings` table
under `terminal.folders`, membership stays on the sessions, and the two are
reconciled on read: a folder named by a session but missing from the record is
still shown, because a session must never become unreachable by losing its
group. Renaming a folder moves every session in it **in one request**, since a
page that did it as eight requests left half of them behind if the tab closed
halfway.

**Colour is inherited, and that is the point.** A folder can be painted; a
session created in it takes the folder's colour, and a window takes its
session's. Colouring eight sessions by hand is work nobody does twice, and a
group whose members are individually grey is not a group. The palette is a
closed set (`term.Colours`) because the value is written into a tmux format and
read back out of one, and because it has to render against twelve themes.

**Windows and panes.** `organise.go` covers windows — naming, colouring,
reordering (`MoveWindow`, as the adjacent swaps tmux actually offers, since it
has no insert), and handing one to another session (`MoveWindowToSession`,
which checks ownership of *both*). The listing carries `window_bell_flag`,
`window_activity_flag` and `window_zoomed_flag`: tmux has tracked them all
along and nothing in this class surfaces them, and they are the only answer to
"which of these five tabs did something while I was looking at another one".
`panes.go` is tmux's third level — split, select, zoom, kill, layout, and
`synchronize-panes`, which is reported in the window listing because it is the
one setting that turns a typo into the same typo on four servers. Killing the
last pane in a window is refused for the reason killing the last window in a
session is: tmux would take the parent with it. That refusal, and the audit
entry, are the whole guard on closing: none of the three close routes asks for
a typed phrase — see invariant 3 for why frequency rather than severity decides
that.

`SendKeys` is the only way this package writes to a session other than through
the PTY the operator is attached to. The literal text and the named keys are
separate fields because `send-keys` decides between them by parsing what it is
given — a stored one-liner containing the word `Enter` would otherwise become a
keypress — and the key names are a closed list.

Two tmux display options are set per session — at creation *and* on reattach,
so a session made by an older build is fixed by being picked up rather than by
being recreated. Per session, so the operator's own tmux sessions and anything
they reach over SSH keep their own settings.

`status off`: the status line is the same information the page draws above the
pane, rendered green-on-green inside a terminal that is short to begin with.

`mouse on`: this is what makes the scroll wheel scroll. Without it the wheel
does something actively wrong rather than nothing — tmux holds the alternate
screen for the whole session, and xterm translates a wheel tick in the
alternate screen into a cursor key, so scrolling up in a shell walked backwards
through command history instead of showing what had scrolled past. The
scrollback that matters is tmux's in any case; xterm's own stays empty because
tmux repaints the viewport rather than emitting lines. The cost used to be that a plain
drag selected into tmux's copy buffer instead of the browser's — tmux clears
its own selection on mouse-up, so text highlighted and unhighlighted inside one
gesture and `getSelection()` stayed empty, which is what made the Copy button
and the copy shortcut both report that nothing was selected.

`forcePointerToSelect` in `xterm-pane.tsx` takes the pointer back. xterm gates
mouse-report forwarding on one predicate — `shouldForceSelection`, which its
selection service and its forwarding both ask, so answering it once is what
keeps the two from disagreeing — and the pane inverts it: the drag belongs to
the page unless **Alt** is held, which is left as the way through for a program
that wants a mouse of its own (vim, htop, less). The wheel is bound separately
inside xterm and does not consult it, so scrolling still belongs to tmux.
`clipboardKey` is the other half: Ctrl+C copies **only when something is
selected** and clears the selection as it goes, so the interrupt is never more
than one keypress away; Ctrl+V returns false *without* `preventDefault`, so
xterm leaves the key alone instead of sending ^V and the browser's own paste
runs — arriving through `onData`, where the multi-line confirmation still sees
it. Reading the clipboard there instead would need a permission Firefox does
not grant at all.

### Signing in to GitHub

`internal/ghx` is the GitHub half of the git page, and it exists because the honest answer
to "why did my push ask for a password" used to be an ssh session.

**Everything is per repository, and that is not a detail.** gh stores its token under the
home of whichever account runs it, and writes a credential helper into that account's git
config; gitx already runs git as the account that *owns the checkout* (`hostexec.AsOwner`),
so ghx runs gh the same way. Sign in as root and push as `deploy` and the push is anonymous
again. Every route therefore takes `?path=`, and the account chip says which host account
the credential belongs to rather than implying the answer is global.

**gh is in the image, not borrowed from the host.** The host's copy would run as the host's
root in the host's namespaces, and the account that actually pushes would see neither the
token nor the helper. Signed in from this image both halves land in the same account's
home — which is bind-mounted from the host, so a shell over ssh finds the same credential.

**The login is the CLI's own device flow, performed here.** `gh auth login` is a series of
prompts and a web request has nobody to answer one, so `device.go` runs the OAuth device
flow itself — against the GitHub CLI's own public client id, which is what makes the token
indistinguishable from one gh minted, and what the operator sees named on the authorisation
screen — and hands the finished token to `gh auth login --with-token`, which is
non-interactive by design. The device code stays on the server and the access token never
reaches the browser: the page holds an opaque flow id and polls with that. GitHub's polling
interval is enforced server-side from the flow's own clock, because its remedy for polling
too fast is to slow the whole flow down. `LoginWithToken` is three steps that are one
operation — store the token, `gh auth setup-git`, and write a committer identity if the
account has none — since any two without the third is a state nobody can see: a token with
no helper pushes anonymously, a helper with no identity fails at the commit instead. The
pasted-token path is the way in for a GitHub Enterprise host or a machine account.

**`gh auth status` is parsed, because it has no `--json` and never will.** It is written for
a person, so the wording is the contract; `parseAuthStatus` matches both the wordings gh has
shipped (`as <name>` and `account <name>`) and `ghx_test.go` pins them, because the sign-in
state of the whole page hangs off it. Every field it reads is optional, so a future rewording
costs a missing scope list rather than a broken page.

**Pull requests are the one thing git has no verb for.** `CreatePull` shells to `gh pr
create`; the handler pushes the branch first, because gh refuses a branch the remote has
never seen and its remedy is an interactive prompt. That is also why `gitx.Push` now sets the
upstream when there is none rather than repeating git's "no upstream branch" advice, which is
a command to copy into a terminal the operator is trying not to open. `gitConfigured` answers
one question with one dot — would a commit and a push from this page be this account's — and
knows that an **ssh** remote never asks a credential helper anything, so it reports the
identity and stays quiet about the token.

### Databases: eight engines behind one shape

`internal/dbx` drives PostgreSQL, MySQL/MariaDB, SQLite, SQL Server, ClickHouse, Oracle,
MongoDB and Redis, all on pure-Go drivers so the image still needs no CGO.

**`Dialect` is the whole abstraction.** Everything one SQL engine does differently — its
`database/sql` name, its quote character, its bind marker, its pagination tail, its
catalogue queries, its DDL keywords, its session list, its size query — is one method on
one interface with six implementations. The earlier shape was a `switch driver` inside each
of a dozen functions; it worked for three engines and stopped working at seven, because a
new engine meant finding and extending eleven separate switches and a missed one failed at
runtime rather than at compile time. Adding an engine now is one file the compiler checks.

**Identifiers are quoted, values are bound, always.** `validateIdent` refuses only what
quoting cannot make safe (a NUL, a control character) rather than a conservative character
class — a table called `user-profiles` was listed and then refused to open under the old
rule — and `quoteWith` doubles the engine's own quote character. Every row edit is scoped by
the primary key and refused outright without one, because an UPDATE the caller believes
touches one row would otherwise touch all of them.

`rowsql.go` is the one deliberate exception, and it does not generalise: it renders a row as
an INSERT *for the clipboard*. Nothing executes what it produces — the statement is text the
operator pastes somewhere else, having read it — and no code path may call it and then run
the result. `TestLiveRowInsertSQLQuoting` hands a value containing `'); DROP TABLE …` back
to every live engine and checks the table is still standing.

**Reading is separated from running, on purpose.** `dbx.Classify` decides whether a
statement is destructive, fails closed on anything it does not recognise, and the handler
applies the capability check and the tighter budget by hand — the route cannot know. Every
dialect's `ExplainPlan` must describe a statement *without executing it*; that is asserted in
the interface and proved by `TestLiveExplainDoesNotExecute`, which asks each engine to
explain a DELETE and then counts the rows.

**The diagnostic surface is what a data browser usually lacks.** `activity.go` lists what
the server is running now — with the blocking session named where the engine reports it,
which is what turns twenty "slow" sessions into one culprit — and stops one. The list
includes the dashboard's own connections, marked `self`: hiding them meant a server with
nothing else connected reported an empty table, which reads as a broken query rather than an
idle server. `size.go` is the per-table disk breakdown for when the alert fires and nobody
knows which table grew; row counts there are the engine's own estimate, because counting
forty tables exactly is a full scan of the database to answer a question about relative
size. `search.go` finds which table a value lives in, bounded in three directions at once
(tables visited, columns compared, matches per table) — those bounds are not tuning knobs,
they are what makes the feature safe to point at a production server.

**A dump for every engine, with no external dependency.** Three of the eight
have a client-side dump tool the image can carry (`pg_dump`, `mysqldump`,
`mongodump`); the rest have none — ClickHouse's is a separate package, SQL
Server's ships only inside Microsoft's image, Oracle's runs on the server, Redis
has none — and the answer used to be `ErrUnsupported` at the moment the operator
pressed the button, which is the worst time to learn that a backup was never
possible. `dump_sql.go` writes the dump itself over the connection already open:
DDL then INSERTs, ordered so a referenced table is created and filled before the
tables referencing it, since alphabetical order fails on the first foreign key.
`dump_nosql.go` does the same for Mongo and Redis as gzipped JSON Lines — Redis
through `DUMP`/`RESTORE`, so every type survives including the ones with no
textual form. The native tool is still preferred where it exists, and a native
tool that *fails* falls through to the built-in one rather than to an error, so
a `pg_dump` too old for its server is a slower dump rather than no dump.
`dumpLiteral` is the second place in the package that puts a value into SQL text
rather than binding it — unavoidable, since a dump file is text — and it is
per-engine for a reason `rowsql.go` can ignore: a backslash is an escape inside
a string literal on MySQL and ClickHouse and a plain character on the other
four, so one rule loses data on half of them. `Restore` picks its reader from
the file's first bytes rather than from the driver, because a Postgres
connection may hold either a `PGDMP` archive or the SQL this package wrote.

**A dump the operator can take away, and a database they can remove.** The dump stays on the
server — that is what the restore route reads — and a copy goes to the browser as soon as it
is written, because a backup whose only copy is on the machine it protects is not one.
`GET /databases/{id}/backup/download` takes a *name*, not a path, and contains it against that
connection's own dump directory with a `files.Service` scoped to it: invariant 6's containment
with the right root, since narrowing `JD_FILE_ROOTS` must not stop the dashboard handing back
a file it wrote itself. `DELETE /databases/{id}/database` is the other end — `Dialect` gained
`DropDatabaseSQL` and `AdminDatabase` because Postgres and SQL Server both refuse to drop the
database the session is inside, and because two engines have no such verb at all: a SQLite
database is a file to unlink, and a Redis keyspace is fixed at startup and can only be
emptied, which `DropResult.Gone` reports rather than pretending otherwise. The connection is
deleted with the database when it was that connection's own, since a connection to a database
that no longer exists fails every request it is asked.

**Testing is against real servers, and skips rather than fails.** `live_test.go`,
`live_nosql_test.go`, `live_devx_test.go` and `api/handlers_db_live_test.go` take each
engine's DSN from an environment variable defaulting to a local instance, and skip with a
message naming the variable when it is unreachable — so `go test ./...` stays green on a
machine with nothing installed and gets stricter on one with the servers up. A suite that
fails for want of a database teaches people to ignore it. These are the tests that matter
here: a catalogue query naming a column the server does not have is string-matched
identically by a unit test and rejected by the engine. Every bug this feature has shipped
was of that kind — SQL Server rejecting `ADD COLUMN`, a size query summing every index_id
and reporting a table at four times its size, Postgres's `now()` being the *transaction*
timestamp and so reporting the asking session a negative age. Oracle is the one engine with
no live coverage: its installer cannot be driven headlessly, which CONTRIBUTING says
plainly rather than pretending otherwise.

### The proxy: a site, a certificate, and what a visitor actually gets

Three additions to `proxysvc`, each closing a gap that sent the operator somewhere else.

**`sites.go` / `sites_render.go` / `sites_apply.go` — the site builder.** `SiteSpec` is the
dashboard's description of a site, not nginx's, for the reason `dockerx.ContainerSpec` is not
`container.Config`: the server's own shape is a historical accident and rendering it as a form
is how a proxy UI becomes twelve accordions. Rendering happens **on the server**, so there is
exactly one implementation of what a spec means — a second one in TypeScript would drift, and
the version that mattered would be the one nobody was reading. The output is hand-written
rather than templated, because order carries meaning to whoever maintains the file after this
dashboard is gone.

`ApplySite`'s order differs from the plain config editor's, and the difference is the point: a
brand-new file in `sites-available` is not in nginx's include tree, so `nginx -t` has nothing
to say about it. The symlink goes in **before** the test, and both the file and the link are
undone together if it fails.

Three details in the renderer are load-bearing. The ACME challenge location is emitted **above**
the catch-all redirect, or renewal stops working in sixty days and nobody finds out until the
certificate expires. `http2 on;` is a directive, not a `listen` parameter, which nginx 1.25
deprecated and warns about on every reload. And WebSocket upgrades pass `$http_connection`
through rather than the usual `$connection_upgrade` map, because a `map` is only legal in the
`http` block and a site file cannot reach there.

`ParseSiteSpec` reads a file back so the form can edit one, and reports whether it carries our
marker — a hand-written file round-trips only as far as the parser recognises, and the UI says
so rather than silently offering to overwrite somebody's work. It skips the generated ACME
location: reading that back as one of the operator's own emits it twice on the next save.

**`tlsscan.go` — what the domain is actually serving.** Everything else on that page reads
files, which answers a question nobody has: a certificate renewed and never reloaded, a proxy
still offering TLS 1.0, a redirect that quietly stopped working are all invisible on disk. Each
protocol version is probed on a connection pinned to exactly that version, so the answer is the
server's rather than a negotiation — and a version this client will not ask for is reported as
`unknown`, never as `refused`, because reporting it absent would be a false reassurance about
exactly the versions that matter most. `grade` is a pure function of the scan for the same
reason `Assess` is.

**`dns01.go` — wildcards, and domains behind a CDN.** Let's Encrypt will only
sign `*.example.com` against a DNS challenge, and a domain proxied by Cloudflare
cannot answer an HTTP challenge because the request never reaches this host.
Between them that was most of the certificates people actually wanted. Eight
certbot plugins are supported as a closed set — each names its credentials and
propagation arguments after itself, and route53 has neither — with credentials
written 0600 into certbot's own tree. Asking for a wildcard over HTTP is refused
here with what to do instead, rather than relaying certbot's "wildcard domains
are not supported by the HTTP-01 challenge", which is accurate and useless.

**`import.go` — a certificate somebody bought.** The key is checked against the
certificate *before* either is written: a mismatched pair is accepted by every
text editor and refused by nginx at reload, which on a live server means finding
out during an outage. Imports live in `/etc/ssl/just-dashboard` rather than
under `/etc/letsencrypt`, so a renewal run can never prune a certificate it did
not issue.

**`streams.go` — the things that do not speak HTTP.** nginx's `stream` is a
top-level context, a sibling of `http`, so a stream cannot live in a file under
sites-available. They go in `/etc/nginx/stream.d` and the page reports plainly
when `nginx.conf` does not include it — a config nginx never reads is the same
failure as a drop-in it ignores. nginx.conf itself is never edited from here:
every other configuration on the host depends on it.

**`htpasswd.go` — the site form's password field, with something to put in it.**
bcrypt in process rather than shelling to `htpasswd`, which lives in
apache2-utils and is not installed on a host running nginx — and which would
put the password in an argv that `/proc/*/cmdline` makes world-readable.

**`certbot.go` — issue, renew, revoke.** The dashboard already knows a certificate has eleven
days left; leaving the operator to remember certbot's arguments is where every panel in this
class stops. `renewalScheduled` gets a field of its own because it is the real story behind
almost every expired certificate: not a forgotten renewal, a timer that stopped months ago.
Issuance defaults to `--staging` in the UI — the real limit is five failures an hour and it is
easy to reach. `dns.go` answers "does this domain point here yet", and recognises Cloudflare
explicitly, because reporting a CDN as a misconfiguration is the commonest false alarm a check
like this produces.

### Streaming

`internal/wsx` wraps gorilla/websocket: origin check on upgrade (a WS handshake is not
subject to CORS, so this is what stops a malicious page using the operator's cookie),
serialised writes, ping/pong, 1 MB read limit. Frames are `Envelope{type, data, error, ts}`,
consumed by `useSocket` on the frontend. Server-side filtering is the rule — log grep and
level filters are applied *before* lines are sent. A single container log line is bounded at
256 KB (`dockerx.maxLogLine`) so a container emitting a gigabyte without a newline cannot
exhaust the dashboard's memory.

### Jobs: the operations that outlive their request

`internal/jobs` runs the commands that take minutes — issuing a certificate, upgrading every
package, writing sshd's config and reloading the daemon. All three used to be ordinary
handlers, so the browser held a request open for the length of a certbot run and a dropped
VPN meant an operator with no idea whether their SSH config had been applied.

It is deliberately *not* the compose runner's shape. `RunComposeStream` is a socket that owns
its command and refuses to reconnect, because reconnecting re-issues the GET and re-issues the
GET runs the command again. A job inverts every part of that: the work descends from
`context.Background()` (like `helpers.detachedContext`) so nothing about the browser can kill
it, output goes into a ring buffer with a sequence number per line, and a subscriber asks for
everything `after` a sequence it already has. So closing the tab, navigating away and losing
the connection all leave the work running and the transcript complete.

- `Manager.Start(spec, run)` returns immediately with a `Job`; the API answers `202`.
- `Emitter` is what a runner writes through: `Status` for the narration between steps, `Line`
  for raw output, and `Run`/`RunEnv`, which execute through `hostexec` and forward both
  streams line by line.
- `Subscribe(id, after)` is the resume: backlog first, then a live channel. A subscriber that
  cannot keep up is skipped rather than allowed to stall the command — the buffer is the
  record, the channel is only the tail.
- Bounded on purpose: 5000 lines a job, 64 KB a line, 50 jobs kept. `prune` runs when a job
  finishes as well as when one starts, or a burst that ends after the last `Start` would sit
  over the cap until something else happened — which on an idle dashboard is never. A running
  job is never pruned.

The split between what is checked before the job and what happens inside it is the other
half. Validation stays synchronous — a bad email, a wildcard asked for over HTTP, an sshd
change that would lock the operator out — so a refusal answers the click that caused it
rather than arriving a minute later as a failed job. That is why `certbot.go` exposes
`IssueArgs`/`RenewArgs`/`RevokeArgs` rather than `Issue`/`Renew`/`Revoke`, why
`netsec.PlanSSHSettings` is separate from `ApplySSHPlan`, and why `updates.UpgradeCommand`
returns an argv instead of running one.

`GET /jobs/{id}/stream` sends the job, then the backlog, then batches output every 120ms.
Cancelling is `service.control`; the routes are in `api/handlers_jobs.go`.

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

### The version

Two constants, and nothing else: `internal/version.Version` and
`frontend/src/lib/version.ts`. Everything that shows or logs a version reads one of them —
the wordmark in the sidebar and on the sign-in page, and the line the server logs at boot.

They are separate because neither half can reach the other's build, and `internal/version`'s
test reads the TypeScript file and fails the run if the two disagree, which is what makes a
release two one-line edits rather than a hunt. It skips when the frontend is absent, so the
backend module still tests on its own. `frontend/package.json` carries a third copy because
npm demands the field; the same test pins it to the same release, loosely, since npm wants
three components and the product version has two.

A fourth file now joins them, and it is the one that cannot be skipped:
`backend/internal/selfupdate/changelog.json` says what the release *contains*.
`TestChangelogHeadIsTheProductVersion` pins its newest entry to `version.Version` in both
directions, because both directions are wrong in a way that reaches every install in the
world — see the next section.

### Cutting a release

**When the user asks for a new version, they name it: "make this 0.6", "release 0.6.1".
That is the trigger, and this is what it means.** It is one command, and it is not
`git tag` — a release here is a commit on the tracked branch, because that is what every
install compares itself against.

```bash
# 1. Write the release notes FIRST, in backend/internal/selfupdate/changelog.json.
#    Anywhere in the array; it is sorted on read.
# 2. Then:
scripts/release.sh 0.6
```

`scripts/release.sh` bumps `internal/version/version.go`, `frontend/src/lib/version.ts` and
`frontend/package.json`, regenerates the root `CHANGELOG.md` from the same JSON, and runs the
two tests that pin all four together. It refuses — before touching anything — if the
changelog does not already describe the version being cut, and prints the entry skeleton to
fill in. `CHANGELOG.md` is **generated**; never edit it by hand.

The ordering is deliberate and is the whole point of the rule. Every install in the world
decides whether to offer itself an update by comparing its own `version.Version` against the
changelog at the head of the tracked branch, so:

- a version bumped with no changelog entry is a release nobody is told about, and
- a changelog entry with no version bump is every existing install permanently offering
  itself an update it already has.

Both are caught by the test run rather than by an operator. Writing the notes first is what
makes "0.6" a release with something to say rather than a number.

A changelog entry is written for the person deciding whether to upgrade a root-equivalent
panel on their own server, not for the person who wrote the code. Each change carries a
`kind` (`added`, `changed`, `fixed`, `removed`, `security`, `deprecated` — a closed set the
parser enforces) and reads as what they can now do; `detail` is for where the consequence is
not obvious, and most changes do not need one. `breaking: true` requires a `breakingNote`
saying what has to be done by hand, and the UI refuses to fold it away.

### Updating the dashboard itself

`internal/selfupdate` is the one module that manages the product rather than the server, and
it exists because the honest answer to "how do I upgrade this" used to be an ssh session.
Four pieces:

**The changelog is data, not prose.** `changelog.json` is embedded with `go:embed` *and*
fetched from `raw.githubusercontent.com` at the tracked branch, parsed by the same function
both times — so a malformed file fails the test run before it can be a malformed file every
install downloads. Markdown would have needed a parser and could not answer the questions
the UI actually asks: which releases sit between this install and the newest, and what kind
is each line. Reading it needs no network: the compiled-in copy is what an install with
`JD_UPDATE_CHECK=false` still shows.

**The check is the only outbound request this product makes on its own initiative**, and it
is kept deliberately small: one unauthenticated GET, four times a day, a user agent carrying
the product and version and nothing else, and a switch to turn it off. A failure keeps the
previous good answer rather than blanking the banner, because a dropped tunnel is a normal
Tuesday here and a notice that flickers is one nobody trusts.

**Where the install lives is discovered, not configured.** The dashboard asks Docker which
container bind-mounts its own data directory — the directory holding the database this
process has open, which is decisive where a service name is not — and reads the compose
`working_dir` label off it. `JD_UPDATE_DIR` is the escape hatch, and an install that cannot
be identified says so precisely instead of showing a button that fails.

**The upgrade runs in a sibling container, and that is the load-bearing decision.**
`docker compose up -d --build` on this stack rebuilds three images and recreates three
containers, one of which is the process that ran the command. A child process is killed
with the backend's cgroup somewhere in the middle, leaving the frontend and proxy never
recreated and no way to report it. So the backend creates a *separate container* through the
Docker socket — running its own image, which already carries git, the docker CLI and the
compose plugin, so there is nothing to pull — and that container is untouched by its own
stack being rebuilt around it. It writes progress to `self-update.json` and a transcript to
`self-update.log` in `JD_DATA_DIR`, which both halves mount, so the *new* backend can read
what the old one was doing. `Installer.Reconcile` runs at boot and is the other half: the
updater still alive means leave it, gone with the version moved means it worked (this
process running at all is the proof), gone with the version unchanged means it stopped.

Two details inside the upgrade are worth keeping. It **fast-forwards, never resets** —
unlike `internal/deploy`, whose target's working tree is disposable, this is the operator's
own checkout and an edited compose file is the normal case, so a local change survives
unless it genuinely collides and git says exactly what is in the way. And it **waits for the
dashboard to answer** on its own health URL before calling itself finished, because
`compose up -d` returns as soon as containers start and a backend that starts and then dies
looks identical from there.

On the frontend, `hooks/use-self-update.tsx` is one poll for the whole shell and its gotcha
is the feature's whole design problem: **the API goes away in the middle of the thing it is
watching**. A poll that fails while a run is in flight is rendered as "restarting", never as
an error, and the cadence is set from the fetch rather than from an effect so a failed fetch
does not drop back to the five-minute interval. `components/update/` is the notice in the
sidebar footer (which renders *nothing* when there is nothing to say), the release-notes
sheet, and the panel at the top of `/updates` — where the dashboard's own version sits above
the host's packages, because "what can be updated on this machine" is one question.

### Deployment topology

```
browser ──(Tailscale / SSH tunnel)──▶ Caddy :8443
                                        ├─ /api/* ─▶ backend :8080  (loopback)
                                        └─ /*     ─▶ frontend :3000 (loopback)
```

All three ports are variables — `JD_PORT` (8443), `JD_BACKEND_PORT` (8080), `JD_FRONTEND_PORT`
(3000) — read by `docker-compose.yml` and `deploy/Caddyfile` from the same `.env`, and chosen
from what is free by `install.sh`, which also fills them in on a re-run against an `.env`
written before they existed. It never *moves* a recorded port: on a re-run against a dashboard
that is up, the process holding the port is this dashboard, and every way of telling that apart
from a squatter is a guess that moves the ports out from under a working install when it is
wrong.

**The frontend is the one service not on the host network**, and that is what makes a taken
port survivable rather than silent. It has no host access of its own, so it gets its own bridge
network and a port published on loopback that the proxy dials exactly as before. On the host
namespace, Next failed to bind, the container restart-looped, and the proxy's catch-all
forwarded to whatever already held 3000 — the operator got a stranger's application over the
dashboard's own certificate. Published, Docker refuses first: `docker compose up` stops with
*"failed to bind host port 127.0.0.1:3000/tcp: address already in use"*, before anything
serves. Inside the container the port is always 3000; only the host side is a variable, because
only the host side can collide. The defaults are the three most contested numbers on a
Linux server, and the collision they used to produce was the worst kind to debug: only the
frontend and backend fail to bind, while the proxy comes up clean and forwards to whatever
already held the port — so the operator reaches somebody else's application over the
dashboard's own certificate, with nothing in any log saying so. The variables must stay
readable from one place; a proxy forwarding to a port its service did not bind is exactly the
failure they exist to remove.

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

`(dashboard)/layout.tsx` is the authenticated shell and owns five things:
`CommandPaletteProvider`, `SelfUpdateProvider`, `SidebarProvider` + `AppSidebar`, `TopBar`,
and `MetricsStream`
(which renders nothing and exists to hold the metrics socket open for the whole shell, so the
Overview charts and the top bar's vitals keep filling while you are on another page).
`SelfUpdateProvider` is one poll of the dashboard's own version for the sidebar notice and
the Updates page together, and is what keeps that poll alive across the moment the backend
restarts itself. Its
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

`components/logo.tsx` is the third, and it is smaller than it looks: the logo is the
wordmark and nothing else — "Just" in `text-primary`, "Dashboard" in the text colour, the
version as small muted text beside it. No mark, no tile, no strapline. It is the only
rendering of the product's name in the app, so the sidebar, the sign-in screen and the
loading splash all show the same thing and a rename is one file. Colour comes from the
palette rather than from a fixed hex, so the logo re-themes with everything else, and
`LogoMark` is the single letter the collapsed rail falls back to when three rem is all there
is.

`components/ui/*` is generated shadcn/ui (new-york, zinc, lucide, 36 primitives). Prefer
composing over editing these; feature-specific pieces live in `components/<feature>/`
(`docker/`, `files/`, `metrics/`, `procs/`, `proxy/`, `security/`, `terminal/`).

### The editor is served from here, not from a CDN

`components/code-editor.tsx` wraps Monaco, and the one line that matters is
`loader.config({ paths: { vs: "/monaco/vs" } })`. `@monaco-editor/react` otherwise fetches the
editor from `cdn.jsdelivr.net` at runtime, which is wrong twice over: it is third-party
JavaScript executing in the same origin as a session that drives the Docker socket, systemd
and a root shell — exactly what this product's security argument rules out — and it means
every editor in the dashboard (files, compose, nginx, the site preview) is a spinner that
never resolves for an operator whose workstation has no egress, which is the workstation this
is meant to be reached from.

`scripts/sync-monaco.mjs` copies `monaco-editor/min/vs` into `public/monaco/vs`. It runs from
`predev` and `prebuild`, **and** explicitly in the Dockerfile, because the image builds by
invoking next's entrypoint directly and never sees the npm hooks. The copy is generated, so it
is gitignored and excluded from eslint — 24 MB of somebody else's minified output takes longer
to lint than everything this repo wrote.

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

### The Docker panel

`components/docker/` is the largest feature directory and the one where the product's
opinion about newcomers is expressed, so it is worth knowing what each file is for.

- `explain.tsx` is the teaching layer: `Hint` (one line under a control), `Term` (a dotted
  underline with the definition one hover away), `Field`, and `GLOSSARY` — the definitions
  themselves, written for somebody who has never run a container and phrased around the
  decision rather than the mechanism. The rule it enforces is that explanation is *quiet*: a
  form that shouts every caveat is as unusable as one that explains nothing.
- `create-container.tsx` is the create form, and its three entry points matter more than the
  form does. **Paste a command** covers the overwhelmingly common case — a README with a
  `docker run` line and a reader with nowhere to put it; `lib/docker-run.ts` parses it,
  leniently, returning a usable spec plus an honest list of what it could not represent.
  **Start from something common** is `lib/docker-templates.ts`: a dozen images almost every
  server ends up running, pre-filled with the ports, volumes and settings they need, every
  one bound to 127.0.0.1. It is a set of starting points, not an app store — the thing that
  goes stale and becomes the maintenance burden in Yacht and CasaOS. **From scratch** is for
  people who know what they want. The Command tab shows the server-rendered `docker run` and
  compose equivalent, live.
- `run-console.tsx` (`useRunConsole` + `RunConsole`) is how every long command is watched
  rather than waited for. It deliberately **does not** let `useSocket` reconnect: reconnecting
  re-issues the GET, and re-issuing the GET runs the command again — a redeploy fired twice
  because a VPN blinked is not a re-render. A dropped socket ends the run and says so.
- `diagnosis-panel.tsx` renders `dockerx.Diagnosis`, collapsed by default so the list is a
  list of findings rather than a wall of argument, with the remedy as a button where the
  server named one. `ContainerFindings` filters the page's single diagnosis pass rather than
  fetching per panel.
- `stack-detail.tsx` is a stack as the application it is: services with clickable ports, the
  compose file editable in place (validated before saving, and saving is *not* deploying —
  the UI says so), one merged log feed tagged by service, and the links out to Files, git and
  a shell in the stack's directory.
- `container-detail.tsx` gained the findings, the reachability join (a published port plus
  the proxy site pointing at it, which is what turns "running on 3000" into a URL), the
  writable-layer view, editable limits, raw inspect, and Update/Duplicate/Rename.
- `events-tab.tsx`, `images-tab.tsx`, `volumes-tab.tsx`, `networks-tab.tsx`,
  `build-dialog.tsx` are the remaining tabs. The build dialog is where the git panel and
  Docker stop being two products: a repository the dashboard already pulls is a build context.

Three deep links exist for this feature and are worth preserving: `/files?path=`,
`/git?repo=` and `/terminal?cwd=`. The first two are read once as an initial value rather than
kept in sync — the URL is where the reader arrived, not where they are now — and the terminal
one opens a session exactly once per mount, because a shell is a process and not a render.

### The security and proxy panels

`components/security/` and `components/proxy/` are the two feature directories added for the
network pages, and they follow the Docker panel's rule: explanation is quiet, and the teaching
lives next to the control rather than in a banner above it.

- `posture-panel.tsx` renders `netsec.Posture` the way `health-panel.tsx` renders `Health`,
  with one addition — a finding whose `fix` the server named becomes a button, and the page
  maps that string to the request plus the confirmation it deserves. `AreaFindings` filters the
  page's single verdict rather than fetching per tab.
- `rule-form.tsx` is the firewall's create form and the reason the catalogue exists on the
  server: picking "Redis" fills in 6379 *and* raises the warning at the moment of choosing,
  rather than in a report afterwards. The `ufw` line it would run is shown in the footer, for
  the reason the Docker create form shows its `docker run`.
- `ssh-panel.tsx` stages every change and applies them together, since each one is a write, a
  parse and a reload. The dialog says to keep the session open and check a second terminal —
  the one piece of advice that matters and the one no error message can give afterwards.
- `site-form.tsx` shows the server-rendered nginx live beside the form, debounced. The preview
  request is the same renderer that writes the file, so the form is not a black box and what
  is on screen is what lands on disk. `DNSCheck` sits under the domain field because "the name
  does not point here yet" is the cause of most certificate failures and certbot reports it as
  "challenge failed".
- `tls-report.tsx` renders the scan, and says out loud what `unknown` means for a protocol row.
  A grade with no working is a number to optimise.

`ConnectionsPanel` and `OffendersPanel` both offer a one-click firewall deny, which is the
join that makes the pages one product: the address the ban log keeps naming is the one that
deserves a rule outliving the ban.

### The terminal panel

`components/terminal/` is the rail, the strips and the tags; `components/xterm-pane.tsx` is
the emulator. The split matters: the pane is reused by the compose runner and knows nothing
about sessions.

- `session-rail.tsx` is the workspace tree. Its first version drew a folder and a session as
  the same row at the same weight, which put the hierarchy in the data and nowhere on
  screen. Four things carry it now and are worth keeping: a folder header is **chrome** (the
  panel-header tint, an icon in a tinted tile, an uppercase label) while a session is content;
  children are indented behind a rule in the folder's colour; colour is inherited; and
  everything is draggable. Pinning sorts a session to the top *of its folder* rather than
  lifting it into a separate group — the separate group was the earlier design and it made a
  starred session vanish from the folder it had been filed in.
- `dnd.ts` holds the in-flight drag payload outside React, for the reason
  `lib/metrics-crosshair.ts` does: `dragover` fires at pointer rate across the whole rail, and
  a context above it would re-render the terminal on every one. The browser will not let a
  `dragover` handler read the payload — only its MIME types — so the payload is kept beside
  the drag and read back on the way over.
- `window-strip.tsx` is the window chips plus `PaneBar`. A pane's label is the command
  running in it: "pane 2" says nothing, `pg_dump` says which half of the screen not to close.
- `tags.tsx` is the colour vocabulary. The values live in `globals.css` as `--tag-*` and are
  the one deliberate exception to "compute it from the palette": a tag is a label the operator
  applied, and one that changed hue with the theme would stop being the same label. What *is*
  computed is everything drawn from it — the row tint and the edge rule are `color-mix`
  against the surface, so one lightness holds up on a near-black card and a near-white one.
- `lib/terminal-settings.ts` keeps the font, cursor, scrollback and behaviour switches in
  localStorage, on the screen and not on the account, for the same reason the theme is.
- `lib/terminal-keymap.ts` is every shortcut, and the fact that all of them are rebindable.
  A chord has to get past the browser, the page and the shell in the pane, and there is no
  default that annoys nobody — tmux settled that argument with a prefix key half the world
  rebinds. Ctrl+Alt is the default family because neither the browser nor a shell wants it;
  Ctrl+Shift is the emulator's own, as on every Linux terminal. Matching is on `event.code`,
  the physical key, so a binding recorded on QWERTY survives a switch to a Romanian layout.
  Actions carry a **scope**: `navigation` is the page's (it alone knows the sessions), and
  `terminal` is the pane's (the compose runner reuses the emulator and needs copy, paste and
  search where there is no session at all). That split is what stops one keydown being
  handled twice. `shortcuts-dialog.tsx` is both the cheatsheet and the editor, because a
  read-only list is opened once and a settings page hidden elsewhere is opened never.

Several things in `xterm-pane.tsx` and the page are load-bearing and easy to undo:

**Multi-line paste is confirmed, and the guard lives in `onData`.** A pasted block runs every
line but the last, immediately, with no chance to read them — and Ctrl+V, the right-click menu
and the X11 middle click all arrive as a browser paste event that becomes one `onData` call.
Guarding only the Ctrl+Shift+V handler guarded the one route nobody uses. The explicit
clipboard reads guard themselves, and the shortcut handler must call `preventDefault`:
returning false from `attachCustomKeyEventHandler` stops xterm, not the browser, so without it
Ctrl+Shift+V opened the confirmation *and* pasted natively at the same time.

**Replies are suppressed while the scrollback is replayed.** A terminal answers some of what
is written to it — `CSI c` and the other device queries are the shell asking the terminal a
question, and xterm replies down the same channel a keystroke uses. Replaying a buffer that
contains one makes it answer again, at whatever prompt exists now: reopening a tab typed
`1;2c0;276` into the shell and left a column of "command not found". The server announces the
replay with a `scrollback` frame before the binary snapshot, because from the browser's side
the bytes are identical either way, and the client drops its own output until xterm's write
callback says the replay is parsed.

`allowProposedApi` is on because the search addon's match count and highlight-all are built on
xterm's decoration API, which is not frozen yet. Without it `findNext` throws where it would
have decorated and the counter reads "none" over a scrollback full of matches.

**Clicking inside a pane focuses it, and the arithmetic is the only way it can.** tmux composes
every pane into one screen before the PTY sees a byte, so the browser has a single terminal and
no element to hang a handler on — a click lands on a cell and nothing else. `Panes` therefore
carries `pane_left/top/right/bottom`, `XtermPane` reports the clicked cell (the grid is uniform,
so the screen's box divided by rows and columns is exact — xterm publishes no pixel-to-cell
mapping), and the page finds the rectangle containing it. It no-ops for a single pane and for a
click already inside the focused one, because either would be a tmux subprocess per click.

**The navigation listener runs in the capture phase and must not skip the terminal.** Bubbling
would land after xterm had already forwarded the keystroke, so Ctrl+Alt+→ would switch the
window *and* type an escape sequence at the prompt. And the usual "ignore keys while a text
field has focus" guard has to make an exception for `.xterm`: xterm receives keystrokes through
a hidden `.xterm-helper-textarea`, so the plain form of that guard disables every shortcut
exactly when the terminal has focus, which is the only time anybody presses one.

**The page has no header.** A terminal is the one screen whose content *is* the viewport, and a
title band plus an explanatory notice cost about a fifth of the pane on a laptop. The
breadcrumb already says where you are, "New session" sits in the rail beside "New folder", and
which account a shell runs as is on the pane's own header. The one banner that stays is a
missing login account, which is a broken feature rather than information.

**The drag image is drawn by hand.** Left to the browser it snapshots the row, and a row with a
transparent background — which every row here has until it is hovered — gets composited onto an
opaque white rectangle with hard corners. Chrome exposes no way to style that, so `dnd.ts`
builds an off-screen chip in the theme's own tokens, hands it to `setDragImage`, and removes it
on the next frame.

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
  re-checks it. Its `phrase` is optional, and the absence is meaningful: a request without
  one is a reversible action that still deserves a pause — deleting a terminal folder, which
  loses a grouping and nothing else — and asking somebody to type "delete folder" for that
  teaches them to type phrases without reading, which is the one habit the typed confirmation
  exists to prevent.

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
3. Every destructive action is behind `s.destructive` — the `destructive` capability, the
   tighter `destrLim` budget and an audit entry — and pauses the operator with a confirmation
   dialog. A **subset** of those additionally requires the typed `X-Confirm` phrase, enforced
   server-side inside the handler where the expected phrase is known.

   **The test for the phrase is frequency, not severity.** A phrase in front of something done
   a dozen times a day is not read, it is typed — and the operator who has learned to type one
   table name without looking types the next one the same way. That habit is precisely what the
   phrase is protecting on the routes that keep it, so every route added to the typed set makes
   the typed set weaker. The question to ask is not "is this dangerous" (they all are, that is
   what `s.destructive` marks) but "how often does somebody do this, and can they get it back".

   Typed, because they are rare and there is no way back: `DROP DATABASE`, `DROP TABLE`,
   `DROP COLUMN`, `TRUNCATE`, an import that truncates first, dropping a Mongo collection, a
   Mongo pipeline with `$out`/`$merge`, a `critical` statement in the query runner, restoring
   a database or a backup over live data, `compose down`, removing a Docker volume, any prune,
   deleting a dashboard or Linux account, a recursive directory delete, `git discard` and
   `git reset --hard`, toggling the firewall, applying package updates, and installing a new
   version of the dashboard itself.

   That last one is worth spelling out, because it is the newest and the most tempting to
   soften: the phrase is the **version being installed** (`0.6`), not a fixed sentence. It
   names the object, which is what every other typed route here does — a stack's name for
   `compose down`, a table's name for `DROP TABLE` — and what has to be read before pressing
   a button in the sidebar is *which* version. A release lands every few weeks, and an
   install that comes back broken is recovered over ssh rather than from here, so it sits
   squarely on the rare-and-unrecoverable side of the frequency test.

   Not typed, because they are routine or recoverable or both: deleting rows and documents and
   Redis keys, dropping an index, forgetting a connection, stopping a database session,
   stopping/restarting/killing/removing/recreating a container, removing an image or a network,
   deleting one file, signalling a process, stopping or restarting a service, revoking a token
   or an SSH key, deleting a backup job or a deploy project, rolling back a deploy, disabling a
   vhost, deleting a firewall rule, and closing a terminal session, window or pane.

   Several routes decide by content rather than by path, and the narrowing lives at the call
   site: `handleDBQuery` types only for `critical`, `handleFileDelete` only when `recursive`,
   `handleGitReset` only when `--hard`, `handleDBImport` only when `truncate`, and
   `composeNeedsPhrase` only for `down` — with `requireComposePhraseWS` applying the same
   narrowing on the socket so the two entry points cannot disagree. The frontend mirrors each
   with a conditional `phrase`, and the server re-decides regardless.

   One relaxation on top of all this: `httpx.RequireTypedConfirmationWS` also accepts the
   phrase as a query parameter — used only by WebSocket routes, where a browser cannot set a
   header at all, and where `wsx`'s origin check supplies what the header was protecting
   against. Do not reach for it from an ordinary handler.

   `api/handlers_db_test.go` pins both directions for the database surface —
   `TestIrreversibleDatabaseRoutesDemandAPhrase` and
   `TestRoutineDatabaseRoutesDoNotAskForAPhrase` — because the line has two failure modes and
   the second one (a phrase creeping back onto routine work, one defensible route at a time) is
   the quieter of the two.
4. Capability checks live on the route, never in the UI alone. Where the answer depends on
   what is *in* the request rather than which route it hit, the handler checks by hand and
   fails closed: `dbx.Classify` for SQL, `api.authoriseSpec` for a container spec that is
   privileged or mounts a host path.
5. Every state-changing request lands in the audit log.
6. Client-supplied paths go through `files.Resolve` — including the ones that do not look
   like file operations: a container's bind-mount source, a build context, a new stack's
   directory. Host commands go through `hostexec` with an argv, never a shell string. The one
   shell in the codebase is `deploy.Deployer.shell`, and it is deliberate: those are pipelines
   an admin stored for their own project ("bun install && bun run build"), not anything
   supplied per request. Do not add a second one, and do not "fix" that one into an argv.
   `dockerx` invokes the `docker` binary in three places — compose, the streaming compose
   runner and `Build` — because the Engine API has no equivalent for compose projects or for
   BuildKit. All three build the argv explicitly.
7. Nothing but Caddy binds a routable address.
8. Store schema changes are additive and tolerate an existing database. There is no
   migration tool: `CREATE TABLE IF NOT EXISTS` is a no-op against a database that
   already has the table, so a **column** added later goes in `store.addedColumns` as
   well, which `applyAddedColumns` ALTERs in at open. Every entry needs a `DEFAULT`
   (SQLite refuses a NOT NULL column on a table with rows without one) and no entry is
   ever removed — the list is the path from every shipped schema to the current one, not
   a description of the current one.
