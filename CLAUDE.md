# CLAUDE.md

Guidance for Claude Code working in this repository.

## What this is

Just Dashboard is a self-hosted control panel for **one** Linux server: metrics, Docker, processes,
logs, a real PTY, files, git, databases, reverse proxy, firewall, backups and deploys behind a single
authenticated UI. Go backend + Next.js frontend behind Caddy, one `docker compose` stack.

The software is **root-equivalent** — it drives the Docker socket, systemd, the firewall, host
accounts and a shell. Its security boundary is *the network perimeter plus mandatory 2FA*, not the
container. Every architectural oddity here traces back to that.

## Commands

```bash
# backend/
go build ./... && go vet ./... && go test ./...
go test ./internal/gitx -run TestBranchParse -v
go run ./cmd/server                  # needs JD_MASTER_KEY and a writable JD_DATA_DIR

# frontend/
bun install && bun dev               # :3000, proxies /api to 127.0.0.1:8080
bun run lint && bun run build        # the whole frontend gate; there is no test suite

# whole stack
sudo ./install.sh                    # interactive first install; re-runnable, keeps .env
docker compose up -d --build
docker compose logs backend | grep "bootstrap admin"   # generated password, printed once
scripts/release.sh 0.6               # cut a release — see "Cutting a release"
```

CONTRIBUTING requires `go build ./...` and `bun run build` to pass before a PR.

**Backend testing.** 22 packages carry tests, all fast and hermetic — `go test ./...` is reasonable on
every change. Two families skip rather than fail when the thing they drive is absent:

- **Live database tests** (`dbx/live*_test.go`, `api/handlers_db_live_test.go`) read each engine's DSN
  from an env var defaulting to a local instance. Re-run with `-count=1` or the cache serves yesterday's
  skips. These are the tests that matter for dbx: a catalogue query naming a column the server does not
  have is string-matched identically by a unit test, and only a real engine rejects it.
- **`term` and the terminal half of `api`** drive the machine's real tmux — the bugs they exist to catch
  live in the gap between this process and that one, and a fake tmux would pass every one. Both take a
  private tmux server in `TestMain` (`TMUX_TMPDIR`) so a run never touches the operator's sessions.

Extend these when you touch the matching surface: security — `httpx/confirm_test.go`,
`api/routes_test.go`, `api/docker_spec_test.go`, `files/files_test.go`, `safepath/safepath_test.go`,
`dbx/classify_test.go`, `api/handlers_security_test.go` (signs a real admin in and drives whole routes,
because a rule tested in its own package says nothing about which group the route was mounted in);
product *claims* — `dockerx/diagnose_test.go`, `netsec/posture_test.go`, `proxysvc/tlsscan_test.go`;
nginx rendering **including the parse back** — `proxysvc/sites_test.go`, since anything the renderer
emits and the parser cannot read is a field silently dropped on the next save.

**Frontend notes.** bun only (`bun.lock`); never add `package-lock.json` or `yarn.lock`. Next's dev
rewrite proxies HTTP but **not** WebSocket upgrades, so socket-backed pages in dev need
`NEXT_PUBLIC_WS_BASE=http://localhost:8080` plus `JD_ALLOWED_ORIGINS=http://localhost:3000` on the
backend. `bun dev`/`bun run build` run `scripts/sync-monaco.mjs` first; invoking `next` directly skips
it and leaves every editor spinning. `go.mod` declares `go 1.25.0` — check `go version` before blaming
the code on a network-restricted machine.

## Architecture

### The request chain is the security contract

`backend/internal/api/routes.go` is the map of the whole API. Every `/api/v1` request passes:

```
network allowlist → rate limit → authenticate → capability → handler
```

- **Allowlist before auth** (`httpx.AllowlistCIDRs`): an off-network attacker cannot reach the login
  handler at all. `httpx.RealIP` trusts `X-Forwarded-For` only from `JD_TRUSTED_PROXIES`, or the
  allowlist could be spoofed past.
- **Three limiters**, `NewLimiter(perMinute, burst)`: `loginLim` (10/5, per address, on top of the
  per-account lockout), `apiLim` (600/120, per principal), `destrLim` (30/10, destructive routes).
- **Capabilities, not roles** (`auth/roles.go`): `read`, `service.control`, `file.write`, `terminal`,
  `destructive`, `system.admin`, held by `admin`/`limited`/`readonly`. Gate with
  `httpx.RequireCapability` so adding a role later cannot silently widen an endpoint.
  `httpx.RequireSession` additionally blocks API tokens from human-only routes (password change,
  minting tokens, account management).
- **`httpx.AuditMutations`** records every state-changing request. WebSocket routes are GET and
  long-lived, so they call `s.recordAudit(...)` at open time — the event is "a terminal was opened".

Two deliberate exceptions: `/healthz` (unauthenticated, fixed body, no version or hostname) and
`/api/v1/hooks/deploy/{hookID}` (HMAC over the raw body, still allowlisted, still audited so
enumerating hook ids is not silent).

### Handler conventions

Handlers are `httpx.Handler` — `func(w, r) error`, rendered by its own `ServeHTTP`. `s.handle(...)` at
the mount site is only the conversion.

- Return `httpx.Err/BadRequest/Internal/Wrap`; never write an error body by hand. `httpx.WriteError` is
  the single renderer and is what keeps internal error strings off the wire.
- Decode with `httpx.DecodeJSON` (4 MB cap, unknown fields rejected).
- `s.destructive(r, ...)` = capability check + `destrLim` + audit. It does **not** enforce confirmation;
  the typed-phrase subset calls `httpx.RequireTypedConfirmation(w, r, phrase)` **inside** the handler,
  where the phrase is known, and it reaches the client as `error.phrase`. See invariant 3.
- `s.destructive` nests inside stricter groups too (admin holds every capability), so "which routes are
  destructive" has one answer.

Three routes decide from request **content**, because the path cannot know: `POST /databases/{id}/query`
(`dbx.Classify`, fails closed, handler applies capability + budget by hand); `POST /docker/containers`
(`service.control`, plus `api.authoriseSpec` demanding `system.admin` for privileged, added
capabilities/devices, host network, or a bind mount — the settings that turn "may run a container" into
"owns the server"); and `GET /docker/stacks/{name}/run?action=…`, which derives capability and
confirmation from the action using the same `composeIsDestructive` set the POST routes use. That socket
is the one place a phrase may arrive as a query parameter (`RequireTypedConfirmationWS`) — a browser
cannot set a header on a WS handshake, and `wsx`'s origin check replaces what the header guarded.

Files: `api/handlers_*.go`, one per feature, each with `mount<Feature>Routes` called from `Routes()`.
`handlers_domains.go` and `handlers_docker_manage.go` own no mount function — they are mounted from the
proxy and Docker mounts so those route maps stay in one place. Shared plumbing (`atoiDefault`,
`timeoutCtx`, `recordAudit`, `detachedContext`) lives in `api/helpers.go`.

### Server, modules, degradation

`api.Server` holds config, logger, store, auth service, sealer, audit logger, authenticator, WS
upgrader, the three limiters, and in agent mode the `agent.Identity`. `api/modules.go` (`moduleSet`)
holds the feature backends: `sys`, `metrics`, `docker`, `dockerStats`, `dockerEvents`, `pm2`, `systemd`,
`table`, `cron`, `logs`, `term`, `files`, `git`, `github`, `updates`, `selfUpdate`, `proxy`, `dbs`,
`linuxUsers`, `netsec`, `jobs`, three backup pieces, two deploy pieces.

**Every module is optional.** A host with no Docker socket, no systemd or no fail2ban serves everything
else; affected routes return a precise "unavailable on this host" code the frontend renders as
information (`ErrorState` in `components/state.tsx`), not an error.

`Server.Start(ctx)` is separate from `New` so failing to schedule background work is reported by `main`
rather than swallowed in construction. It starts the metrics recorder (here, not lazily — its whole
purpose is to have been running while nobody was looking), the Docker event log, the self-update check,
the backup scheduler, and `selfupdate.Installer.Reconcile`. `Shutdown` releases what outlives a request:
sampler, scheduler, live PTYs, database pools, Docker client.

`helpers.detachedContext` is the deliberate opposite: work that must outlive its request (a backup
transfer, a `compose up --build`) descends from `context.Background()` and is not cancelled by shutdown
— a deploy killed halfway is worse than one finishing into a dashboard that is gone. Its timeout is the
only bound.

### Reaching the host, and containing paths

`internal/hostexec`:

- `Command` runs a binary locally when present, else via `nsenter --target 1`. `CommandOnHost` *always*
  crosses (for tools like `who` that exist in the image but would report on the container).
  `CommandOnHostInDir` exists because nsenter resets the working directory, silently discarding `cmd.Dir`.
- `AsOwner(cmd)` drops to the UID/GID owning `cmd.Dir`, so a `git pull` does not leave root-owned files.
- Argv is passed through unchanged and **never** through a shell. Keep it that way (invariant 6).

`files.Resolve` is the single choke point for client-supplied paths: it checks the cleaned path *and*
the symlink-resolved path (the parent, for files not yet existing) against `JD_FILE_ROOTS`. Every new
filesystem entry point goes through it, including the ones that do not look like file operations —
backup restore destinations, database dump paths, bind-mount sources, build contexts. `ResolveEntry`
applies the same containment but returns the entry rather than its target: use it for delete, move, stat
and chmod, which act *on* a symlink.

`internal/safepath` holds the archive-unpacking rules (absolute symlink targets refused, nothing written
through a symlink already in the destination, the final component unlinked rather than followed). Both
`files/archive.go` and `backups/restore.go` use it; they used to carry a copy each of the same lexical
prefix test, with the same hole. `internal/sysinfo` reads the host through gopsutil rather than parsing
`/proc`, so the same path works across kernels and inside a container with `/proc` bind-mounted.

### Auth, secrets, state

`internal/auth` owns users, sessions, TOTP, recovery codes, API tokens. Cookie `vpsd_session` (HttpOnly,
SameSite=Strict, Secure unless `JD_DEV`). A password alone yields a *partial* session accepted only by
the 2FA routes (`AuthenticatePartial`); everything else answers `totp_required` /
`totp_enrollment_required`. API tokens may narrow their creator's role, never widen it, and are demoted
with the account. `auth.Sealer` (from the 64-hex `JD_MASTER_KEY`) encrypts every stored secret — TOTP
seeds, connection strings, deploy env, backup credentials.

State is SQLite in `JD_DATA_DIR`, schema as one `CREATE TABLE IF NOT EXISTS` block in
`internal/store/store.go` with no migration tool (invariant 8). The file is still named `vpsd.db`
through the rename: moving it would strand every existing install's accounts, audit log and secrets.
Tables: `users`, `recovery_codes`, `sessions`, `api_tokens`, `audit_log`, `db_connections`,
`backup_jobs`/`backup_runs`, `deploy_projects`/`deploy_env`/`deploy_runs`, `watched_domains`, a
`settings` key/value table, and three metrics tables.

`internal/audit` writes `audit_log` **and** mirrors every entry to the process log, so a trail survives
the database being tampered with. An `Entry` records who (user, role, `Actor` = session or token), from
where, what (action, target, method, path), and how it went.

## Feature backends

### Metrics, saturation, health

`internal/metrics` samples on the server's own timer into SQLite: a live socket only describes the time
since a tab was opened, and charts that start empty every visit cannot show last night's spike.

- `GET /system/metrics/history` buckets **in SQL**, and every series carries its bucket's **peak** beside
  its mean — a 100% second inside a ten-minute bucket averages away to nothing.
- Capacity is **per filesystem** (`metric_mount_samples`, `GET /system/metrics/storage`), never a
  worst-of line: when the fullest mount stops being the fullest, one line drops to the runner-up and
  reads as freed space on a disk that never changed. Pseudo filesystems are filtered before the write.
- Containers are sampled into `metric_container_samples`, keyed by **name, not id** — a compose redeploy
  replaces the container and seeing across the restart is the point. Docker being absent is logged once,
  not an error. `/docker/containers/stats/history` serves a sparkline per table row in one query.
- Container network/block totals are stored as Docker's **cumulative counters** and differenced in SQL
  (`MAX - MIN` over the bucket). A total can be re-bucketed later; a rate recorded against one interval
  cannot.
- The recorder keeps its **own** `sysinfo.Collector` and `dockerx.StatsSampler`: rates are deltas, and
  sharing with request handlers would let a one-shot `GET /system/metrics` shorten the next interval.

**Saturation series** answer "is work waiting", which utilisation cannot: CPU **by mode** including
`steal` (on a VPS the one whose fix is outside the machine); PSI pressure (`Supported: false` renders as
"cannot tell", never three reassuring zeroes); disk IOPS/service time/%util as the **worst device** (a
disk saturated by small random writes moves almost no bytes); socket totals from `/proc/net/sockstat`
(enumerating connections is thousands of lines a sample); `load.Misc.Blocked`; and inodes per mount,
where a build server hits the ceiling first on a filesystem every capacity chart calls half empty.

`metrics.Assess` (`GET /system/health`) turns those into findings — measured / means / do — ranked
worst-first. It runs on the server because the thresholds are a claim the product makes, and because
each check reads an hour of history to tell a spike from a trend. **Memory is judged on available, never
on "used"**: Linux counts page cache there and judging by it is a permanent meaningless warning.

`metrics.Events` (`GET /system/metrics/events`) is the annotation layer, answered from `deploy_runs`,
`backup_runs` and `audit_log` — this dashboard *is* the thing that ran the deploy. Reboots need no
storage: a sample whose `uptime_seconds` dropped means the machine went down, which also catches
restarts nobody initiated here. Works with `JD_METRICS_RETENTION=0`; only reboot markers go quiet.

### netsec: exposure, posture, login history

`netsec` reads records the host already keeps rather than polling: wtmp (`GET /logins`), btmp
(`/logins/failed`, behind `system.admin` — it holds whatever was typed at a login prompt, sometimes a
password in the username field) and fail2ban's log (`/fail2ban/history`). Polling a jail would invent
events between samples and miss every ban shorter than the interval.

`netsec.Exposure` grades who can reach this panel (`tailscale`, `tunnel`, `private`, `public`, `open`)
from the allowlist and the host's interfaces. The setting lives in an env file nobody re-reads after
install day, which is exactly why it belongs on screen.

`netsec.Assess` (`GET /security/posture`) is to security what `metrics.Assess` is to load: every panel
in this class shows facts and leaves the reading to somebody who already knows how; the ones that take a
position sell a score out of a hundred, which is a number to optimise rather than a thing to fix.

- A `SecurityFinding` carries measured / means / do as three fields, plus a `Fix` naming a remedy the
  dashboard can perform.
- `Assess` is a **pure function of its inputs** (the handler gathers exposure, firewall, fail2ban, sshd,
  listeners, certificates, failed logins and updates concurrently), which is what makes it testable with
  no firewall or network. `posture_test.go` pins each claim, including the two easy to get backwards: an
  exposed database behind a default-deny firewall is a **warning**, not a critical (otherwise it cries
  wolf), and "turn off password authentication" stops being offered when no account has a key — there it
  is a lockout, not advice.
- `ExposedPort` and `CertSummary` are declared *in* netsec rather than imported from `proxysvc`, so the
  audit has no dependency on how ports or certificates are discovered.
- **A check that could not run is not a pass.** `Posture.Skipped` says which is which, because a zero and
  an unanswerable question look identical: `SecurityFiltering` is false on Alpine/Arch (no advisory
  data), `LoginRecordRead` false wherever `last`/`lastb` are missing (util-linux-extra, absent from
  minimal cloud images). Each is reported as a finding — silence in a security verdict reads as
  "checked, nothing outstanding".

`netsec.Disconnect` ends an interactive login: the PID is matched against the live session list first,
or the route is a "kill any process on this host" primitive wearing a sensible name. SIGHUP, not
SIGKILL, so the login is recorded as ended.

### sshd

`netsec/sshd.go` reads the **effective** config (`sshd -T`, falling back to parsing) — a file setting
`PasswordAuthentication` twice does not behave the way it reads. Two sshd semantics are load-bearing and
get read backwards by anyone treating the file as an ini: the **first** value wins, and everything after
a `Match` is conditional. So the parser never overwrites an earlier value, a `Match` ends the file for
it, and its existence is reported rather than dropped.

`sshd_apply.go` writes in the proxy editor's order: refuse certain lockouts → write → `sshd -t` →
restore on failure → reload only then.

- The write target is a drop-in under `sshd_config.d` **only if the main file includes that directory
  before setting anything itself**. First-value-wins means a drop-in included at the bottom is a file we
  wrote and sshd ignored — the worst outcome for a security setting. Otherwise the directive is replaced
  in place and later duplicates commented out.
- `guardSSHLockout` refuses only the certain cases: passwords off with no key anywhere, both passwords
  and keys off, root the only keyed account with passwords off. Disabling passwords where somebody has a
  key is correct and must never be blocked.
- The directive list is **closed** — an open set makes this a config editor that can take the machine off
  the network.
- `Port` is bounded by `LegalMin`/`LegalMax`, not the `Min`/`Max` carrying the recommendation (they were
  one pair of fields, so the range was treated as advice). `guardSSHPort` refuses a move to a port a
  default-deny firewall has no rule for.
- `AllowUsers`/`DenyUsers` are `kind: "list"`: the value is explicitly checked for a newline (one would
  write a directive of the caller's choosing on the next line) and normalised through `strings.Fields`.
  An emptied list is commented out — sshd refuses to start behind a bare keyword.
- `permitrootlogin` folds `without-password` onto `prohibit-password`, because `sshd -T` still prints the
  deprecated spelling distributions ship as default and a dropdown missing it renders empty.
- `reloadSSH` tries systemd units, then `rc-service`, then `service`.

**Where sshd listens is not always sshd's decision.** Ubuntu ships socket-activated SSH by default on
24.04+: `ssh.socket` holds the listener, `sshd_config`'s `Port` is read, reported by `sshd -T`, and
ignored — which made the port control the one setting that reported success and did nothing.
`sshd_socket.go` reads and writes the unit alongside the daemon; `SSHDConfig.Socket` carries which unit
holds the port and which port that actually is. A move writes a drop-in and **restarts** (systemd
rebinds addresses only on restart; `daemon-reload` leaves the old port bound while reporting success).
Three drop-in details, each found against real systemd:

- An empty `ListenStream=` before the new one, because systemd *appends* — without it a socket moved to
  2222 still answers on 22.
- Both families named explicitly: a bare `ListenStream=2222` binds the IPv6 wildcard, and under
  `BindIPv6Only=ipv6-only` that takes IPv4 SSH off the machine.
- `BindIPv6Only=ipv6-only` repeated in the drop-in, since naming both addresses is only legal with it.

Addresses are rewritten rather than replaced (a socket bound to one interface stays bound to it), with
wildcards as fallback. Refusing the move would have been the safe-looking choice and would leave the
control broken on the commonest server distribution.

### Firewall: one page, three backends

`netsec/firewall.go` dispatches to ufw, firewalld or iptables (`firewall_{ufw,firewalld,iptables}.go`).
**Validation and both lockout guards live in the dispatcher**, so a fourth backend cannot be added
without them — that placement is the reason the refactor was worth doing. The shared `run` is a
**variable** so recorded transcripts can stand behind it; the fixtures in the three test files are copied
from the tools themselves, since a host running one backend is no way to check the other two.

ufw's grammar has shapes that are accepted and mean something else, checked against a real ufw with
`--dry-run` (`TestUFWAppProfileGoesInATargetClause`):

- **An app profile is a destination, and only a `to` clause reads it as one.** `allow in app OpenSSH` is
  refused; `allow in from 10.0.0.0/8 app OpenSSH` is accepted and binds the profile to the *source* port.
  Every rule names both ends: `from <src|any> to <dst|any>`, `app` inside `to`.
- **`ALLOW FWD` is a third direction** (ufw-docker writes many). An unrecognised direction leaks into the
  source address, and a rule parsed with no direction reads as inbound — which let a host whose only
  rules were forwarding rules pass `admitsAnything` and switch its inbound default to deny. They are
  refused by `replaceRule` and hidden from the edit control.
- **The destination column carries an address when the rule names one** (`10.0.0.5 5432/tcp`), so the
  port is its last field; `presetForRulePort` expands the lists and ranges the form itself writes
  (`6379,6380`, `8000:8010`) before consulting the catalogue. Both were silent: the rule parsed and the
  catalogue warning never fired.
- **ufw writes every rule twice and deletes one of them.** The page folds "(v6)" duplicates away, which
  is right for reading and was catastrophic for deleting: closing a port removed the IPv4 line and *hid*
  the standing IPv6 one. `DeleteRule` reads the rule first and finds its twin **afterwards** by shared
  fields (`sameRule`) — afterwards, because a delete renumbers everything below it.
- **ufw answers a duplicate by doing nothing and exiting zero.** "Skipping adding existing rule" read as
  success, and the `number+1` delete that followed removed the rule *below* the one being edited.
  `AddRule` reports `errRuleExists`, and only when *nothing* was written (a v4 accepted with its v6 twin
  skipped is a real add).
- `AddRule` has `insert` because ufw stops at the first match — a deny added after a broad allow does
  nothing at all, which looks exactly like a deny that works.
- **A ban is a deny rule wearing another name**: `netsec.Ban` refuses the caller's own address, the same
  guard the firewall route has. `IgnoreIP` writes through to the jail.d drop-in — `addignoreip` changes
  only the running server.

firewalld differs in ways that are the work: no rule numbers (a zone holds services, ports and rich
rules, each removed by handing back exactly what was added — numbers are positional from
`Service.Status`, and `Rule.Handle` is never serialised or accepted from a client), everything written
`--permanent` and reloaded (a runtime rule dies at reboot), ports spelled `8000-8010` with no multiport
at all (validation accepts the ufw spelling and `firewalldPorts` translates, one rule per port in a
single call rather than a partly-applied rule), and `AppProfiles` returning predefined services by name
only, since resolving several hundred would be a subprocess each — which is why the picker is searchable.

**iptables is read-only on purpose**: it has no persistence of its own, so a rule added here works until
reboot and leaves a page saying protected in front of a host that is not. `FirewallCapabilities` lets
each backend declare what it can do and `ReadOnlyReason` explains the absence — a greyed-out button with
no reason is worse than one that is not there.

Cross-cutting:

- `FirewallStatus` carries the three default policies structured plus the logging level: allows in front
  of a default of *allow* are decoration, and a firewall that drops silently leaves no record.
  `ufw status numbered verbose` is rejected by ufw outright — it takes exactly one of the two words — so
  the old single call returned no rules and reported every firewall as inactive. Two calls now:
  `numbered` for rules, `verbose` for policy (soft failure).
- **`ReplaceRule` order is the safety property.** Neither ufw nor firewalld has an edit, so the
  replacement goes in **first**; deleting first and failing to add leaves a hole in the firewall, the one
  outcome an edit must never produce. The rule is read before anything is added and found again by what
  it says rather than where it sat. Ordering lives in `replaceRule`, separate from backend detection.
- `SetDefaultPolicy` refuses an inbound deny on a host admitting nobody; ambiguous cases go to the typed
  confirmation, since a rule list admitting *something* cannot be judged without knowing which port the
  browser arrived on.
- `ServiceCatalogue` (`GET /security/services`) is the rule form's teaching layer, served from the server
  so the form's warning and the audit's finding are the same claim. `annotateRule` attaches it centrally
  so firewalld's rules read like ufw's — and `parseUFWRule` must find the port, since `ufw allow 6379`
  writes a destination with no slash and a portless rule dropped the catalogue's warning entirely. It
  warns only when the source is unrestricted; the same port on a private range is the recommendation.
- **fail2ban jail tuning writes `jail.d/99-just-dashboard.local` as well as the running server** —
  fail2ban reads that directory last and a runtime change is gone at the next restart.
  `mergeJailOverrides` rewrites one section and leaves every other jail, and any hand-written line inside
  its own section, alone.
- **fail2ban-client does not print a list.** 1.x draws a tree under a heading and answers an empty set in
  a sentence (`No IP address/network is ignored`); `parseClientList` knew only the two shapes 0.x
  printed, so the allowlist panel showed entries reading "These", "IP", "addresses/networks". All four
  shapes parse now; the ambiguous bare line is settled by what the words look like — every real value is
  an address, a network or a path, and prose is not.
- **No start/stop for a jail**: `fail2ban-client status` lists only running jails, so one stopped from the
  UI would vanish with nothing left to start it. A control usable once is a trap.
- `FailedLoginVolume` counts inside a **window** and reports `Capped`. The posture verdict used `len()`
  of a 500-record btmp listing, which made the 2000-attempt threshold unreachable and the 200-attempt
  notice permanent on every host with a public SSH port.

### Packages: six managers, one interface

`internal/updates` was apt-only, so every RPM, Alpine and Arch host reported *no package manager* —
which renders as "nothing to update" rather than "never checked", leaving the posture audit's patch
check silently dead on half the servers this runs on. It is a `manager` interface now (apt, dnf, yum,
zypper, pacman, apk), each a listing command parsed by a pure function plus an upgrade argv. Four
details were each confirmed by running the tool in a container of its distribution:

- **`dnf check-update` exits 100 when there is something to do.** Treating non-zero as failure is exactly
  backwards; it is read as a *code*, because guessing from the output's shape passed a real failure
  through as an empty package list.
- **dnf5 takes `--security` only after the subcommand**, so `dnf -y --security upgrade` failed on every
  Fedora from 41. Command first, which dnf4 also accepts.
- **zypper reserves 100–106 for informational exits** — one stale repo is not a failure, and returning
  early on those reported an error and no packages.
- **Alpine and Arch publish no advisory data**: `SupportsSecurityOnly` false, `SecurityFiltering` tells
  the UI "cannot tell", and `guardSecurityOnly` refuses a narrowed upgrade rather than quietly applying
  everything.

Reboot detection: Debian's flag file, then `needs-restarting -r`, then "cannot tell". **Exactly exit 1**
means yes — every other non-zero is the tool failing, and reading those as yes puts a permanent reboot
warning on a host that never asked for one.

`manager` embeds `catalogue` (`ListInstalled`, `Search`, `Info`, `Files`, `InstallCommand`,
`RemoveCommand`) — embedded rather than optional because all six can do all of it, and a compiler error
beats a page that renders empty.

- **The installed set comes from the local database, never the front end** (dpkg, `rpm -qa`). Asking dnf
  needs a metadata cache present to answer a question about this disk.
- **"Installed on purpose" is a different question on each** and is what makes two thousand rows
  readable: `apt-mark showmanual`, `dnf repoquery --userinstalled`, pacman's `Install Reason`, Alpine's
  `/etc/apk/world`. zypper has no supported query, so `Explicit` stays false and the filter is hidden.
- **Ranking happens here** (`rankResults`: exact, prefix, contained, summary-only), identically for all
  six, so results do not reorder when the operator moves from Fedora to Debian. `apt-cache search git`
  otherwise puts `git` around row four hundred. The cap applies *after* ranking, and on apt before the
  `apt-cache policy` call — one subprocess rather than sixty.
- **A thin name-only search widens to descriptions.** Matching names is why `nginx` returns nginx and not
  the four hundred packages mentioning it; it is also why "web server" returned nothing, since no package
  is called that. Somebody who knows the name types the name; somebody who does not types what the
  software does. That widened bucket needs its own tie-break (ordering on name length put `nd` and `h2o`
  above nginx), so it ranks by how many typed words the summary carries.
- **The index has an age and it is on screen.** Every read answers from the on-disk database (a search
  that refreshed first would take a minute per keystroke), so the page is only as current as the last
  `apt update` — and on a server nobody logs into, that timer is the first thing to stop. `IndexAge`
  reads the manager's own cache, skipping lock files (touched by operations that fetched nothing);
  `RefreshCommand` fixes it. pacman returns false: a refresh without an upgrade is what turns the next
  `pacman -S` into a partial upgrade.
- **Names are validated before they are arguments** — not for quoting (nothing goes through a shell) but
  because every one of these tools reads a leading dash as a flag and several accept a path to a package
  *file* in the same position.
- `protectedReason` guards removal and is deliberately narrow, in the spirit of `guardSSHLockout`: the
  package manager, init, libc, the shell, sshd, docker, a kernel, plus dpkg's own `Essential`. Everything
  else goes through ordinary confirmation — a guard that second-guesses every risky removal is one nobody
  can work with.

`usage.go` answers "it is installed, now what", the part nothing else in this class has: a version and a
dependency list do not tell you `postgresql-client-16` gave you `psql`. The file list is read into
commands on the path, manual pages, systemd units, /etc entries and the README, and the primary man page
is *rendered*. **Nothing here executes the package's own binaries** — `foo --help` would run an arbitrary
host binary as root on a route needing only `read`. Three details, each a bug found by running it: a
command is a file whose **parent** is a bin directory (`/usr/bin` is in every file list; a binary in
`/usr/lib/postgresql/16/bin` is on nobody's path); a page is recognised by a `manN` **component**, not
`/man/man` (translated pages live under `de/man1`); and the page to render is **ranked**, or coreutils
shows TEST(1) and openssh-client shows scp. `stripOverstrike` undoes nroff bold in four lines rather than
shelling to `col -b`, which lives in the same package whose absence already costs the login records.

### Docker

`internal/dockerx` uses the official SDK over the socket. It shells out in exactly three places —
compose, the streaming compose runner, and `Build` — because the Engine API has no equivalent; all three
build argv explicitly.

- **`ContainerSpec` is the dashboard's shape, not `container.Config` + `HostConfig`.** Those are split on
  the historical accident of which fields the daemon could change after creation, and rendering them as a
  form is how Portainer's create page became twelve accordions. `toEngine` translates and warns about
  what is legal but probably unintended (a port on every interface, no memory limit, an anonymous volume,
  a writable bind mount of a sensitive path). `SpecOf` reads a container back into that shape, which is
  what makes duplicate, edit-and-recreate and "save as a stack" possible.
- **`Recreate` is the verb Docker lacks.** Editing means destroy-and-recreate, which is fine until the
  create fails and the operator has nothing where their service was — so the old container is renamed
  aside (`<name>_jd_replaced`), restored if anything later fails, and removed only once the replacement
  runs. Compose-managed containers are refused with `ErrComposeManaged`. `UpdateResources` is separate
  because limits genuinely can change in place.
- **`render.go` keeps the form from being a black box**: a spec back into the `docker run` line and the
  compose service, rendered **on the server** so "what does this spec mean" has one implementation. The
  YAML is hand-written, not marshalled — key order carries meaning and a marshaller would sort it into
  something correct and unreadable.
- **`diagnose.go` is what nothing else in this class has.** Every panel shows a state, an exit code and a
  restart count and leaves you to read them; this says what 137 means and what the limit was, that a
  container restarted twelve times in a minute, that a health check is failing and what it last said,
  that a port is published in front of the firewall, that an unrotated json-file log has reached 800 MB,
  that data is being written into the container rather than a volume. Findings carry a `Level`, the
  reasoning and an `Action` where the remedy is ours to run. Deliberately conservative — a panel that
  cries wolf is ignored wholesale — and `diagnose_test.go` pins the claims, including the two **silences**
  (a finished one-shot job, a loopback-bound port).
- **`events.go` keeps what Docker throws away**, so "why did this restart at 04:00" has an answer. An
  in-memory ring: an event log worth keeping across restarts belongs in the audit table. `oom` and
  `health_status: unhealthy` justify the feature alone.
- **`CheckUpdate` compares the registry's current digest against the pulled one** — a more useful question
  than "is there a newer tag", because it catches a moving tag that moved. Cached 30 min, four-worker
  pool, because Docker Hub rate-limits by address. Unreachable or credentialed registries are `unknown`
  with the reason; a locally built image is `local`, not a failure.
- **Compose.** `RunComposeStream` forwards output line by line, because a request that hangs for minutes
  is indistinguishable from a broken dashboard. `composeSteps` maps actions to commands; `update` is a
  pull **then** an up, so a registry that is down leaves the running stack alone. `ValidateCompose` feeds
  the candidate to the parser on **stdin** so a syntax error never touches disk; `WriteComposeFile` goes
  through a temp file in the same directory and keeps `<name>.bak`, guarding what validation cannot catch
  — a correct file that says the wrong thing. `DeclaredServices` costs a subprocess per stack and is read
  on demand; it supplies the one fact the container list cannot, a declared service with no container.
- **`Build` drives the `docker` binary** because BuildKit is a separate builder the classic API path never
  reaches, and silently building with the legacy one produces images differing from the same Dockerfile
  from a shell.
- **Efficiency rules that are load-bearing**: `ListContainers` carries `Mounts` (the Engine summary
  already has them, and "what uses this volume" for every volume at once is otherwise an inspect per
  container per poll); `ListStacks` builds on `ListContainers` so it inherits resolved health and uptime;
  `Diagnose` inspects each container once and runs every rule against that payload.

### Files

`internal/files` used to be a listing, a reader and a writer, with a page that answered every click by
loading the file into Monaco — right for a config file, wrong for a picture, a tarball, a video and a
two-gigabyte log.

- **`preview.go`** reports what a thing *is* without loading it: a trimmed head for text (whole lines, no
  rune cut in half), image dimensions, the first 200 entries of a zip or tar without unpacking, child
  counts for a directory. `Editable` is separate from `Kind` on purpose — a preview of a huge log must not
  offer a Save that writes those hundred lines over it. Extensions decide media kinds, **bytes** decide
  the rest (a `.log` is sometimes a rotated binary; a file with no extension is usually a script).
  `imageSize` parses webp and svg by hand: half of what a web root holds, and neither has a stdlib decoder.
- **`MediaType` is a security boundary.** `GET /files/raw` returns a file's bytes with a content type the
  browser acts on, on the same origin as a session that drives the Docker socket — so it is a closed
  allowlist (images, video, audio, PDF) and everything else is refused rather than sniffed. **HTML is not
  on it and must not be**: served inline it runs as this dashboard. The route tightens CSP to `sandbox`
  (which neuters a directly opened SVG) and is the one route with a short `Cache-Control` instead of
  `no-store` — forty thumbnails otherwise re-read every JPEG on every scroll; callers append mtime so a
  saved image is a new URL.
- **`find.go`** is the fuzzy finder (`search.go` is the literal/regex one, optionally grepping contents).
  Subsequence matching scored so the basename beats directories, a run beats scattered characters, a
  boundary beats mid-word and a shallow path beats a deep one; terms ANDed; positions as **UTF-16
  offsets**. Bounded three ways (time, visits, matches) and it *says* when it stopped early — a fuzzy
  search that quietly answers from a third of the disk is worse than one that admits it.
- **`places.go`**: `Home` prefers `$HOME`, then `/root`, then a single account under `/home`, then the
  first configured root — every candidate checked through `Resolve`, because a shortcut landing outside
  the roots is worse than no shortcut. `Complete` treats a trailing separator as "inside this directory"
  and anything else as a component being typed; dotfiles appear only once a dot is typed.
- **`Usage`** accumulates per-child totals in the *same* bounded walk (forty children would otherwise be
  forty-one walks) and reports `Truncated` rather than quoting a partial total. A symlink counts as the
  link, or a tree with links into /usr reports the size of the operating system.

Bookmarks live in the `settings` table (`handlers_files_browse.go`), not the browser — which directory
matters is a fact about the server and should be there from a phone. Recent folders are the opposite and
stay in `useViewState`. The bookmark list is saved whole, so an add, a removal and a reorder cannot
disagree about order; every path is resolved before storing.

Frontend `components/files/`: `file-icon.tsx` is the vocabulary (~200 extensions, the files with none —
Dockerfile, authorized_keys, lockfiles — and the folders whose name says more than "folder") mapped to
eight **categories** rather than languages, in the terminal rail's `--tag-*` hues. `file-actions.tsx` is
the one menu both the row and the tile use, or an action ends up in one view only. Two layout rules are
easy to undo: **the panel body does not scroll** (a sticky table header sticks to its nearest scrolling
ancestor, and the header rode away with the rows), and **the rail's tree waits for `/files/places`**
before mounting, since it caches and would keep showing the refusal from listing "/". The image editor
commits each operation to a **new canvas** rather than a live parameter pipeline — that is what makes
undo a stack of bitmaps and why "rotate, crop, rotate again" behaves the way it looks; saving goes
through the ordinary upload route so owner and mode survive.

### Logs

`logsx` + `handlers_logs.go` were three products wearing one page: the grep box and level chips applied
to *file* tails only, `/logs/search` and `/logs/logrotate` had no caller, export ignored the filter, and
rotated archives were unreachable — so "when did this start" could not be asked past last night's
logrotate run, which is the question that sent people back to ssh and zgrep.

- **One filter, compiled once, applied to every kind.** `logsx.Filter` is the single description of what
  the operator wants; `handleLogStream` turns a file, a container, a PM2 process and the journal into the
  same `logsx.Line` before filtering. A bad regex is refused **before** the socket upgrades, so it is a
  form error rather than a stream that opens and stays empty. `logsx.Collector` does the same for history.
- **A filtered tail opens on n *matches*, not n lines.** `TailLines` scans backwards through the last
  32 MB collecting matches, then follows from the byte the scan stopped at — a byte earlier duplicates a
  line, a byte later loses one. `Prefill` distinguishes "few matches" from "we only looked so far back".
- **Archives are part of the log.** `Archives` orders rotated generations by **mtime**: the two schemes
  count in opposite directions (`syslog.1` is newer than `syslog.2`; `syslog-20240612` is older than
  `-20240613`), so ordering by name reports an incident running backwards. gzip and bzip2 read
  transparently; xz and zstd are skipped rather than offered and then refused.
- **`unknown` is a level.** Most lines carry no level word, so a filter without that chip hid every
  unclassified line — including the continuation lines of the stack trace being hunted.
- **The journal's numbers and a text log's words are one vocabulary** (`LevelFromPriority`).
  `maxJournalPriority` pushes only the *maximum* down to `journalctl -p 0..n`, since the chips are a set
  and `-p` takes a range; the exact test is still done here. A text filter is deliberately **not** pushed
  down — `journalctl -g` needs a PCRE2 build nobody can assume — so the window is widened instead.
- **Nothing is offered that cannot be opened**: `Discover` runs `Allow` over the well-known paths, or an
  install that narrowed `JD_LOG_ROOTS` gets a rail of files that refuse to open.
- **Retention is a verdict, not a rule list.** `MatchRetention` finds the file no rule governs — precisely
  the entry a rule list cannot show. Two parser details, both found against a real host: a stanza's paths
  may be listed **one per line before the brace** (exactly how Debian ships rsyslog's, so reading only the
  brace line reported syslog, auth.log and kern.log as governed by nothing), and the globals at the top of
  `logrotate.conf` sit at the same indentation and must not be mistaken for paths. A rule that has not run
  in a fortnight is a warning — that is the failure this panel is for.
- **Parsing performance is the difference between usable and not** (a five-file auth.log scan: 19 s → 2 s):
  a word scan with a map lookup (`detectLevel`) instead of a regex, timestamp layouts chosen from the
  first byte rather than tried in turn, and case-insensitive substring folding in place. The ISO timestamp
  is read as a **token**, not a fixed width — slicing 25 characters chopped the zone off
  `2026-08-28T23:03:24.804642+02:00` and filed the line an hour wrong outside UTC. `Filter.Highlights`
  returns **UTF-16** offsets, because Go counts bytes and the browser slices by code unit.

Frontend `components/logs/`: `filter-bar.tsx` holds the one filter and the Live/History switch, because
"these errors are scrolling past, when did they start" is one thought. Live applies as you type
(debounced; the socket restarts, which is what makes the prefill meaningful); History runs on Enter,
because a keystroke-triggered full scan would queue a pass over gigabytes per character.
`log-console.tsx` uses `content-visibility` rather than a virtualiser — off-screen rows skip layout while
the scrollbar stays honest, wrapped rows keep real heights, and the browser's own find still works.
**Pausing holds incoming lines instead of dropping them.** `histogram.tsx` is matches by level over time;
clicking a column narrows the window to it.

### The terminal

`internal/term` runs the PTYs. Three properties are load-bearing:

**A session outlives everything but being closed.** With tmux the shell runs inside `tmux new-session`,
so closing the tab, leaving the page and restarting the dashboard all leave it running; only `Kill` ends
one, behind a typed confirmation. The reaper *detaches* an idle persisted session after `idleDetach` to
give back the PTY and the slot — it never kills one. Without tmux there is no third option, so there the
reaper kills, which is why the page says which world it is in.

**`su -l` cannot open a shell in a chosen directory**, because login *is* chdir-to-home; tmux's `-c` is
not enough, since su walks straight back out. `loginArgv(shell, keepCWD)` moves the chdir off su onto the
shell: `su -s <shell> <user> -- -l` switches user without `-l`, and the `-l` after `--` reaches the shell
and still reads the profile. The other half is `hostexec.CommandOnHostInDir`.

**tmux is the store for everything the operator chose** — title, folder, favourite flag as user options
(`@jd_title`, `@jd_folder`, `@jd_fav`), not in this process and not in SQLite. The property that keeps
the work keeps its name, with nothing to migrate on restart. Two listing details are not optional: tmux
**escapes non-printable bytes** in format output (a 0x1f separator returns as the four characters `\037`
and every line parses as one field, so `fieldSep` is printable), and **the path is always the last
field**, read with `SplitN`, since a directory may contain the separator.

- `GET /terminal/` returns one list of *workspaces*, not live sessions plus detached names — the same
  thing in two states (`live` = this process holds a PTY). Reconciling two lists in the browser made an
  idle session appear to vanish and reappear elsewhere.
- **The listing answers from memory for a live session and from tmux for the rest.** `tmux new-session` is
  handed to a PTY and the `set-option` filing it away lands up to half a second later — inside the window
  where the page refreshes after the POST — so a shell opened into a folder appeared under "Other" and
  jumped later. `Session` shadows the four values, seeded by `Create`, read back by `Reattach`, written by
  `SetMeta`. **Anything acting on "every session in this folder" must use `Manager.AllMeta`**, or it
  silently skips the newest one. `SetMeta` writes memory first and retries tmux in the background rather
  than failing — filing away the session you just opened is the commonest thing anybody does.
- **Folders are the dashboard's record, not tmux's** (`handlers_terminal_folders.go`, `settings` key
  `terminal.folders`): a folder exists because sessions name it, which stopped being enough once it had an
  order, a colour and the ability to be empty. Membership stays on the sessions and the two are reconciled
  on read — a folder named by a session but missing from the record is still shown, because a session must
  never become unreachable by losing its group. Renaming moves every session **in one request**.
- **Colour is inherited**: a session takes its folder's, a window takes its session's. Colouring eight
  sessions by hand is work nobody does twice, and a group whose members are individually grey is not a
  group. `term.Colours` is closed because the value goes into a tmux format and back out.
- **Windows and panes.** `organise.go` — naming, colouring, reordering (`MoveWindow` as adjacent swaps,
  since tmux has no insert), `MoveWindowToSession` (checks ownership of *both*). The listing carries
  `window_bell_flag`, `window_activity_flag`, `window_zoomed_flag`: tmux has tracked them all along and
  they are the only answer to "which of these tabs did something while I was looking at another".
  `panes.go` adds split/select/zoom/kill/layout and `synchronize-panes`, reported in the listing because
  it is the setting that turns a typo into the same typo on four servers. Killing the last pane in a
  window is refused, as is the last window in a session — tmux would take the parent with it.
- `SendKeys` keeps literal text and named keys as separate fields, because `send-keys` decides by parsing
  what it is given and a stored one-liner containing "Enter" would become a keypress. Key names are closed.

Two tmux options are set per session **at creation and on reattach**, so an older build's session is
fixed by being picked up. Per session, so the operator's own tmux and anything over SSH keep their
settings. `status off` (the page draws that information above the pane, and green-on-green costs a line);
`mouse on` — without it the wheel does something actively *wrong*, since tmux holds the alternate screen
and xterm turns a wheel tick there into a cursor key, so scrolling up walked backwards through history.

**What tmux does with a tick is not the browser's to guess.** tmux's root binding enters copy mode only
when the program has *not* asked for the mouse; every full-screen TUI has, and there nothing scrolls — so
a "Jump to the end" button drawn from the browser's optimism offered to return a terminal that never
moved. A tick now only *asks*: `sync-copy` goes out once the gesture settles, and `#{pane_in_mode}` coming
back is the only thing that shows the button. The keystroke path stays optimistic because copy mode eats
the key that would leave it, and a cancel sent to a pane not in a mode prints "not in a mode" and writes
nothing. Both frames are gated behind the pane's `copyMode` prop, **off by default**: the same component
drives `docker exec`, whose handler writes every non-resize frame into the container's stdin — so the
question would type `{"type":"sync-copy"}` at somebody's prompt.

### GitHub sign-in

`internal/ghx` exists because the honest answer to "why did my push ask for a password" used to be an ssh
session.

- **Everything is per repository.** gh stores its token under the home of whichever account runs it and
  writes a credential helper into that account's git config; gitx already runs git as the account that
  *owns the checkout* (`hostexec.AsOwner`), so ghx runs gh the same way. Sign in as root, push as
  `deploy`, and the push is anonymous again. Every route takes `?path=`.
- **gh is in the image, not borrowed from the host** — the host's copy runs as the host's root in the
  host's namespaces, and the account that pushes would see neither token nor helper. From this image both
  land in the same account's home, bind-mounted, so ssh finds the same credential.
- **The login is the CLI's own device flow, performed here.** `gh auth login` is a series of prompts and a
  web request has nobody to answer one, so `device.go` runs the OAuth device flow against the GitHub
  CLI's public client id — which is what makes the token indistinguishable from one gh minted, and what
  the operator sees on the authorisation screen — then hands it to `gh auth login --with-token`. The
  device code stays server-side and the token never reaches the browser; the page holds an opaque flow id.
  The polling interval is enforced from the flow's own clock, because GitHub's remedy for polling too fast
  is to slow the whole flow. `LoginWithToken` is three steps that are one operation (store, `gh auth
  setup-git`, write a committer identity if missing) — any two without the third is a state nobody can
  see: a token with no helper pushes anonymously, a helper with no identity fails at the commit.
- **`gh auth status` is parsed, because it has no `--json` and never will.** It is written for a person,
  so the wording is the contract; `parseAuthStatus` matches both wordings gh has shipped and `ghx_test.go`
  pins them. Every field is optional, so a rewording costs a scope list rather than the page.
- **Pull requests are the one thing git has no verb for.** `CreatePull` shells to `gh pr create` and the
  handler pushes the branch first, since gh refuses an unseen branch and its remedy is an interactive
  prompt. That is also why `gitx.Push` sets the upstream itself rather than repeating git's advice.
  `gitConfigured` answers "would a commit and push from this page be this account's" with one dot, and
  knows an **ssh** remote never consults a credential helper.

### Databases: eight engines, one shape

`internal/dbx` drives PostgreSQL, MySQL/MariaDB, SQLite, SQL Server, ClickHouse, Oracle, MongoDB and
Redis on pure-Go drivers, so the image still needs no CGO.

- **`Dialect` is the whole abstraction**: driver name, quote character, bind marker, pagination tail,
  catalogue queries, DDL keywords, session list, size query — one method each, six implementations. The
  old shape was a `switch driver` inside a dozen functions; it worked at three engines and broke at seven,
  because a missed switch failed at runtime rather than compile time.
- **Identifiers quoted, values bound, always.** `validateIdent` refuses only what quoting cannot fix (NUL,
  control characters) rather than a conservative character class — a table called `user-profiles` was
  listed and then refused to open. Row edits are scoped by primary key and refused without one, or an
  UPDATE the caller thinks touches one row touches all of them.
- `rowsql.go` is the one exception and does not generalise: it renders a row as an INSERT **for the
  clipboard**. Nothing executes what it produces, and no code path may call it and then run the result.
  `TestLiveRowInsertSQLQuoting` feeds `'); DROP TABLE …` to every live engine and checks the table stands.
- **Reading is separated from running**: `dbx.Classify` decides destructiveness and fails closed; the
  handler applies capability and budget by hand. Every dialect's `ExplainPlan` must describe a statement
  *without executing it* — asserted in the interface, proved by `TestLiveExplainDoesNotExecute`.
- **The diagnostic surface is what a data browser usually lacks.** `activity.go` lists what the server is
  running now with the blocking session named, turning twenty "slow" sessions into one culprit, and can
  stop one; it includes our own connections marked `self`, because hiding them made an idle server report
  an empty table that reads as a broken query. `size.go` is the per-table breakdown for when the alert
  fires and nobody knows which table grew (row counts are the engine's estimate — counting forty tables
  exactly is a full scan to answer a question about *relative* size). `search.go` finds which table holds
  a value, bounded three ways at once; those bounds are what make it safe to point at production.
- **A dump for every engine with no external dependency.** Three have a client tool the image can carry;
  the rest returned `ErrUnsupported` at the moment the operator pressed the button — the worst time to
  learn a backup was never possible. `dump_sql.go` writes DDL then INSERTs over the open connection,
  ordered so referenced tables come first (alphabetical fails on the first foreign key); `dump_nosql.go`
  does Mongo and Redis as gzipped JSON Lines, Redis via `DUMP`/`RESTORE` so every type survives. A native
  tool that *fails* falls through to the built-in rather than to an error. `dumpLiteral` is the second
  place putting a value into SQL text (unavoidable — a dump is text) and is per-engine, since a backslash
  escapes on MySQL and ClickHouse and is a plain character on the other four. `Restore` picks its reader
  from the file's first bytes, not the driver: a Postgres connection may hold a `PGDMP` archive or our SQL.
- **A dump the operator can take away, and a database they can remove.** The dump stays on the server
  (restore reads it) and a copy goes to the browser immediately, because a backup whose only copy is on
  the machine it protects is not one. `/databases/{id}/backup/download` takes a **name**, not a path, and
  contains it with a `files.Service` scoped to that connection's dump directory — invariant 6 with the
  right root, since narrowing `JD_FILE_ROOTS` must not stop us handing back a file we wrote.
  `DELETE /databases/{id}/database` needed `DropDatabaseSQL` and `AdminDatabase` (Postgres and SQL Server
  refuse to drop the database the session is inside) and has no verb at all on two engines — a SQLite
  database is a file to unlink, a Redis keyspace can only be emptied, which `DropResult.Gone` reports
  rather than pretending. The connection is deleted with the database when it was that connection's own.
- **Live tests skip rather than fail**, or a suite failing for want of a database teaches people to ignore
  it. Every bug this feature shipped was a catalogue query a unit test string-matched identically and only
  the engine rejected — SQL Server refusing `ADD COLUMN`, a size query summing every index_id and
  reporting four times the real size, Postgres's `now()` being the *transaction* timestamp and so
  reporting a negative session age. Oracle has no live coverage (its installer cannot run headlessly),
  which CONTRIBUTING says plainly.

### Proxy

- **Site builder** (`sites.go`, `sites_render.go`, `sites_apply.go`). `SiteSpec` is our shape, not
  nginx's, for the reason `ContainerSpec` is not `container.Config`; rendering happens **on the server**
  so a spec has one meaning, and the output is hand-written rather than templated because order carries
  meaning to whoever maintains the file after this dashboard is gone. `ApplySite` puts the **symlink in
  before `nginx -t`** — a new file in `sites-available` is not in the include tree, so the test has
  nothing to say about it — and undoes both together on failure. Four renderer details:
  - The ACME challenge location goes **above** the catch-all redirect, or renewal silently stops and
    nobody finds out for sixty days.
  - `http2 on;` is a directive, not a `listen` parameter (nginx 1.25 warns on every reload).
  - WebSocket upgrades pass `$http_connection` through rather than a `$connection_upgrade` map — a `map`
    is only legal in the `http` block and a site file cannot reach there.
  - **An `allow` list is fenced with `deny all`.** nginx stops at the first match and otherwise permits,
    so a site restricted to `10.0.0.0/8` was reachable from anywhere; expecting the operator to write the
    fence themselves into a box labelled "Deny" fails too, since `0.0.0.0/0` lets in every IPv6 client.
    Explicit denials render *above* the allows for the same first-match reason. One case cannot be fenced
    and is said rather than rendered anyway: nginx answers a `return` in the rewrite phase, before access,
    so an allow list does not restrict a redirect site.

  **`ParseSiteSpec` must round-trip every field the form writes**, or an edit deletes what it cannot read
  back — `Custom` and `BlockExploits` were the two that did not, so opening a site and pressing save
  removed the operator's directives and turned the probe blocks off. `customMarker` and
  `exploitDotLocation` are shared with the renderer and
  `TestCustomConfigurationSurvivesARoundTrip` renders/parses/re-renders; that test is what a new field
  needs. It skips the generated ACME location (reading it back emits it twice), reports whether the file
  carries our marker so the UI can say a hand-written file may not survive, and leaves an `allow`/`deny`
  inside a location where it is — hoisting it into the site-wide list applied one path's restriction to
  the whole site on the next save.
- **`tlsscan.go` — what the domain actually serves.** Everything else on the page reads files, which
  cannot see a certificate renewed and never reloaded, a proxy still offering TLS 1.0, or a redirect that
  quietly stopped. Each version is probed on a connection pinned to exactly that version; a version this
  client will not ask for is `unknown`, **never `refused`**, since reporting it absent would be false
  reassurance about the versions that matter most. `grade` is a pure function of the scan.
- **`dns01.go` — wildcards and CDN-fronted domains**, which between them are most of the certificates
  people want: Let's Encrypt signs `*.example.com` only against DNS-01, and a Cloudflare-proxied domain
  never receives an HTTP challenge. Eight certbot plugins as a closed set (each names credentials and
  propagation after itself; route53 has neither), credentials 0600 inside certbot's tree. A wildcard over
  HTTP is refused here with what to do instead, rather than relaying certbot's accurate and useless
  "wildcard domains are not supported by the HTTP-01 challenge".
- **Two layouts, and files that are not sites.** `nginxVHosts` reads sites-available where it exists and
  conf.d where it does not (every RPM distro, Alpine, Arch — most of the servers this runs on); the
  difference reaches the UI as an empty `EnabledPath`, because conf.d has no symlink and a switch that can
  only error is worse than none. `confdPath` stops `app.conf` becoming `app.conf.conf`. The listing
  **skips backups** (`isBackupFile`: `.bak`, `~`, `.dpkg-old`, `.rpmsave`) — nginx reads none of them, and
  since delete keeps `<name>.bak`, without the filter deleting a site produced a second site.
- **A password file must be readable by the account that reads it.** nginx opens `auth_basic_user_file` in
  a *worker* (www-data/nginx/http), not as the root that wrote it, so a 0640 root:root file is a 403 for
  every visitor and "Permission denied" in the log — which reads exactly like a wrong password.
  `nginxWorkerGID` takes the account from this host's `nginx.conf`, falling back to the three defaults;
  where none resolves the file is 0644, which is what `htpasswd` itself produces. `authDir`/`streamDir`
  hang off `JD_NGINX_DIR`, which exists precisely for hosts whose nginx is elsewhere.
- **`import.go`** checks the key against the certificate **before** writing either: a mismatched pair is
  accepted by every text editor and refused by nginx at reload, which on a live server means finding out
  during an outage. Imports live in `/etc/ssl/just-dashboard`, so a renewal run can never prune a
  certificate it did not issue.
- **`streams.go`** — nginx's `stream` is a sibling of `http`, so a stream cannot live under
  sites-available. They go in `/etc/nginx/stream.d`, and the page says plainly when `nginx.conf` does not
  include it. nginx.conf itself is never edited from here: everything else on the host depends on it.
- **`htpasswd.go`** does bcrypt in process — `htpasswd` lives in apache2-utils, is not installed on a host
  running nginx, and would put the password in a world-readable argv.
- **`certbot.go`** issues, renews and revokes. `renewalScheduled` has its own field because it is the real
  story behind almost every expired certificate: not a forgotten renewal, a timer that stopped months ago.
  Issuance defaults to `--staging` in the UI (the real limit is five failures an hour). `dns.go` answers
  "does this domain point here yet" and recognises Cloudflare explicitly, since reporting a CDN as a
  misconfiguration is the commonest false alarm of this kind.

### Streaming, jobs, secrets, agent mode

**`internal/wsx`** wraps gorilla/websocket: origin check on upgrade (a WS handshake is not subject to
CORS, so this is what stops a malicious page using the operator's cookie), serialised writes, ping/pong,
1 MB read limit, `Envelope{type, data, error, ts}` frames. **Server-side filtering is the rule** — log
grep and level filters apply before lines are sent. A container log line is capped at 256 KB
(`dockerx.maxLogLine`), so a container emitting a gigabyte without a newline cannot exhaust memory.

**`internal/jobs`** runs what takes minutes — certbot, package upgrades, sshd applies — which as ordinary
handlers held a request open for the length of a certbot run and left a dropped VPN meaning nobody knew
whether their SSH config had applied. It is deliberately **not** the compose runner's shape:
`RunComposeStream` owns its command and refuses to reconnect, because re-issuing the GET runs the command
again. A job inverts that — `context.Background()`, a ring buffer with a sequence per line, and
`Subscribe(id, after)` resuming from what the client already has — so closing the tab leaves the work
running and the transcript complete.

- `Manager.Start(spec, run)` returns immediately; the API answers `202`. `Emitter` gives runners `Status`,
  `Line`, and `Run`/`RunEnv` through `hostexec`. A slow subscriber is skipped rather than allowed to stall
  the command — the buffer is the record, the channel only the tail.
- Bounded: 5000 lines a job, 64 KB a line, 50 jobs. `prune` runs on finish as well as on start, or a burst
  ending after the last `Start` sits over the cap until something else happens, which on an idle dashboard
  is never. A running job is never pruned.
- **Validation stays synchronous** — a bad email, a wildcard over HTTP, an sshd change that would lock the
  operator out — so a refusal answers the click rather than arriving a minute later as a failed job. That
  is why `certbot.go` exposes `IssueArgs`/`RenewArgs`/`RevokeArgs`, `netsec.PlanSSHSettings` is separate
  from `ApplySSHPlan`, and `updates.UpgradeCommand` returns an argv.
- `GET /jobs/{id}/stream` sends job, backlog, then batches every 120 ms. Cancelling is `service.control`.

**Secrets, four places, one goal.** `main.scrubSecretEnv` unsets boot secrets (`JD_` and `VPSD_`) once
consumed. `deploy.mergeEnv` strips every `JD_*`/`VPSD_*` from a deploy child's environment — the command
is content the repository owner controls — and is the half that keeps working when a new secret-bearing
variable is added later. `dbx` never puts a password in argv (`/proc/*/cmdline` is world-readable):
`PGPASSWORD` in the environment, a temporary defaults file for MySQL. `dockerx.RedactEnv` masks
credential-shaped container env, because that is where deployments keep secrets and container detail is
not a `system.admin` route.

**Agent mode** (`JD_AGENT_MODE` / `-agent`) swaps the human login surface for mutual TLS: no password
route, no session, no 2FA. `httpx.HubOnly` admits only the enrolled hub's certificate; `/agent/enrol` is
the one route reachable before enrolment, which is why TLS *asks* every caller for a certificate without
requiring one at the handshake and `HubOnly` enforces per route. The enrolment token is printed once per
boot while unclaimed. Feature routes are the same program either way. Not useful standalone yet.

### Configuration, version, release, self-update

**`internal/config`** resolves `JD_*` at boot and **fails closed**: no `JD_ALLOWED_CIDRS` with a
non-loopback bind is a startup error, a missing or malformed `JD_MASTER_KEY` is fatal. A `loader`
collects *every* malformed setting so `Load` refuses with the whole list rather than one error at a time.
`config.Env` falls back to the legacy `VPSD_*` prefix and `adoptLegacyData` picks up pre-rename
`/var/lib/vps-dashboard`. A handful of variables are **not** in `config.go`: `JD_LOG_LEVEL` and
`JD_BOOTSTRAP_USER`/`JD_BOOTSTRAP_PASSWORD` are read in `cmd/server/main.go`, and `JD_SITE` belongs to
`deploy/Caddyfile`. Wherever a variable is read, its documentation lives in four places that must stay in
step: **the reading site, `.env.example`, `docker-compose.yml`, and the README's configuration table.**

**Cutting a release.** When the user names a version ("make this 0.6", "release 0.6.1"), that is the
trigger. A release is a commit on the tracked branch, not a `git tag` — that is what every install
compares itself against. Four files carry it and `scripts/release.sh <version>` writes all of them:
`backend/internal/version/version.go`, `frontend/src/lib/version.ts`, `frontend/package.json`, and
`backend/internal/selfupdate/changelog.json`. Two tests fail the run if they drift (`version_test.go`,
which skips when the frontend is absent, and `TestChangelogHeadIsTheProductVersion`).

```bash
# 1. Write the release notes FIRST in backend/internal/selfupdate/changelog.json
#    (anywhere in the array; it is sorted on read).
# 2. Then:
scripts/release.sh 0.6
```

`release.sh` bumps the three version files, regenerates the root `CHANGELOG.md` from the same JSON, and
runs the two tests; it refuses before touching anything if the changelog does not already describe the
version, and prints the skeleton. **`CHANGELOG.md` is generated — never edit it by hand.** The ordering
is the point: a version bumped with no changelog entry is a release nobody is told about, and a changelog
entry with no version bump is every install permanently offering itself an update it already has.

A changelog entry is written for the person deciding whether to upgrade a root-equivalent panel on their
own server. Each change has a `kind` (`added`, `changed`, `fixed`, `removed`, `security`, `deprecated` —
closed set, parser-enforced) and reads as what they can now do; `detail` is for a non-obvious
consequence, and most changes need none. `breaking: true` requires a `breakingNote` naming what must be
done by hand, and the UI refuses to fold it away.

**`internal/selfupdate`** manages the product rather than the server.

- **The changelog is data, not prose**: embedded with `go:embed` *and* fetched from
  `raw.githubusercontent.com`, parsed by the same function both times — so a malformed file fails the test
  run before it can be a malformed file every install downloads. The compiled-in copy is what an install
  with `JD_UPDATE_CHECK=false` still shows.
- **The check is the only outbound request this product makes on its own initiative**: one unauthenticated
  GET, a user agent with product and version only, and a switch to turn it off. A failure keeps the
  previous good answer rather than blanking the banner — a dropped tunnel is a normal Tuesday here.
- Cadence is two floors, not a timer, because the moment somebody wants a current answer is the moment
  they open the page. `Freshness`: `Cached` nudges past `checkInterval` (2 h), `OnLoad` past
  `nudgeInterval` (5 min), `Forced` waits. Both nudges answer from cache **immediately** and check behind
  the request — blocking would turn one unreachable repository into page loads that hang for fifteen
  seconds.
- **Where the install lives is discovered, not configured**: which container bind-mounts our own data
  directory (decisive where a service name is not), then its compose `working_dir` label. `JD_UPDATE_DIR`
  is the escape hatch, and an unidentifiable install says so rather than showing a button that fails.
- **The upgrade runs in a sibling container**, and that is the load-bearing decision: `compose up -d
  --build` recreates the container running the command, so a child process is killed mid-way with the
  frontend and proxy never recreated and nothing able to report it. The backend creates a separate
  container through the socket, running its own image (which already carries git, docker and compose), and
  writes `self-update.json` + `self-update.log` into `JD_DATA_DIR` — mounted by both halves, so the *new*
  backend can read what the old one was doing. `Installer.Reconcile` at boot: alive → leave it; gone with
  the version moved → it worked (this process running is the proof); gone with the version unchanged → it
  stopped.
- It **fast-forwards, never resets** — unlike `internal/deploy`, this is the operator's own checkout and an
  edited compose file is normal, so a local change survives unless it genuinely collides. And it **waits
  for the health URL to answer** before calling itself finished, since `compose up -d` returns as soon as
  containers start and a backend that starts then dies looks identical from there.

### Deployment topology

```
browser ──(Tailscale / SSH tunnel)──▶ Caddy :8443
                                        ├─ /api/* ─▶ backend :8080  (loopback)
                                        └─ /*     ─▶ frontend :3000 (loopback)
```

Ports are variables — `JD_PORT` (8443), `JD_BACKEND_PORT` (8080), `JD_FRONTEND_PORT` (3000) — read by
`docker-compose.yml` and `deploy/Caddyfile` from one `.env` and chosen from what is free by `install.sh`,
which fills them into an older `.env` on a re-run. It never *moves* a recorded port: on a re-run against a
dashboard that is up, the process holding the port is this dashboard, and telling that apart from a
squatter is a guess that breaks a working install when wrong.

**The frontend is the one service not on the host network**, which is what makes a taken port survivable
rather than silent. On the host namespace Next failed to bind, the container restart-looped, and Caddy's
catch-all forwarded to whatever already held 3000 — the operator got a stranger's application over the
dashboard's own certificate, with nothing in any log saying so. Published on loopback, Docker refuses
first, before anything serves. Inside the container the port is always 3000; only the host side varies,
because only the host side can collide.

Caddy is the only listener on anything but loopback and binds `{$JD_SITE}` **plus** loopback explicitly —
site addresses alone would leave it listening on every interface. One origin for UI and API is
load-bearing: `SameSite=Strict` cookies and the WebSocket origin check both depend on it. Caddy rewrites
`X-Forwarded-For` to the real client address (what makes `JD_TRUSTED_PROXIES` safe); `flush_interval -1`
and zero read/write timeouts keep the long-lived streams alive. The backend container runs `privileged`,
`pid: host`, `network_mode: host` with the Docker socket and real host paths mounted **at their real
names** — remove a mount and the file manager silently browses the container's own empty filesystem.

## Frontend

App Router, all pages `"use client"`, one page per feature under `src/app/(dashboard)/<feature>/page.tsx`.
Eighteen routes plus `/login`.

### The shell

`(dashboard)/layout.tsx` owns `CommandPaletteProvider`, `SelfUpdateProvider`, `SidebarProvider` +
`AppSidebar`, `TopBar`, and `MetricsStream` — which renders nothing and exists to hold the metrics socket
open for the whole shell, so Overview's charts and the top bar's vitals keep filling from other pages. Its
redirect to `/login` is convenience, not a control; every API call behind it is authenticated server-side.

**The scroll container is on the `SidebarInset`, not the document.** That is what pins the top bar and
lets a page ask for the remaining height (`<Page fill>`) instead of growing past the viewport.

`components/app-sidebar.tsx` exports the nav registry (`NAV`, `PERSONAL_NAV`) so `command-palette.tsx`
offers the same destinations without a second list. Items may carry a `capability`, and the sidebar hides
what the role cannot use. ⌘K covers every page plus the theme toggle.

### The design system

Two files define the visual language, and pages compose them rather than hand-rolling layout:

- `components/page.tsx` — `Page` (one measure, gutter and rhythm; `fill` for terminal and logs, whose
  content *is* the viewport), `PageHeader`, `Section`, `Toolbar`, `SearchInput`, `Metric`/`MetricStrip`,
  `DetailList`/`Detail`.
- `components/panel.tsx` — `Panel`, `PanelHeader`, `PanelToolbar`, `PanelBody`, `PanelFooter`, `Well`. A
  panel is *the* content block: a framed surface with a tinted header strip and a hairline, so it reads as
  "chrome, then content" and a toolbar or full-bleed table sits flush beneath without a second edge.

**Reach for `Panel`/`Page`, not raw `Card`**, and add a variant there rather than a one-off in a feature
page — before these existed, fourteen pages read as fourteen products. `components/state.tsx` does the
same for the non-happy paths (`Spinner`, `LoadingRows`, `LoadingPanel`, `EmptyState`, `ErrorState`,
`Notice`). `min-w-0` on the frame and its children is load-bearing: a wide table's intrinsic width would
otherwise widen the flex column and take the whole shell sideways instead of scrolling inside its panel.

**Anything meant to sit in front of what is behind it stands up off the page.** `raised` (a `@utility` in
`globals.css`) draws three lines from the `--raise-*` tokens — a light hairline on top, a dark lip below,
a shadow under — inverting on `:active`. The tokens are translucent black and white rather than colours,
because the same three lines sit on a white primary, a red destructive and a near-black outline alike;
`--control`/`--control-hover` are the shared resting face. Hover goes *darker* and is named per mode
rather than computed, since mixing in more `--foreground` lights the button up on dark mode. Light mode
is not the dark values scaled — the gloss sits on the variant's colour, not on the page. A surface
wanting a deeper shadow overrides the token (`[--raise-drop:var(--shadow-lg)]`) rather than adding a
`shadow-*` utility, which would win the cascade and delete the shine and the lip.

The same class covers cards: a card is a very large button nobody presses, and the earlier split (a
gradient `.card-sheen` on panels, three lines on buttons) made one claim in two visual languages.

Two deliberate exceptions:

- **The nav stays flat.** The lift works by making one thing stand out from what is behind it, which stops
  meaning anything when forty-nine rows claim it at once; a nav is a *list*, and what should stand out is
  the item you are on, which the active item's solid primary fill does. What survived the experiment is
  the spacing: `SidebarMenu`/`SidebarMenuSub` at `gap-2`.
- **Ghost and link buttons stay flat**, and so do inputs and textareas. Ghost is 142 of ~400 buttons — the
  quiet action at the end of a table row — and giving it a face turns every row into a strip of controls
  competing with its own data. A page where fields and buttons are equally raised has no hierarchy left.
  Where a control is a *toggle*, the **unpressed** state is the raised one; pressing puts it down.

`components/logo.tsx` is the wordmark and nothing else — "Just" in `text-primary`, "Dashboard" in the text
colour, the version as small muted text beside it. No mark, no tile, no strapline. It is the only
rendering of the product's name, so sidebar, sign-in and splash agree and a rename is one file. `LogoMark`
is the single letter the collapsed rail falls back to.

`components/ui/*` is generated shadcn/ui (new-york, zinc, lucide, 35 primitives) — compose rather than
edit. Feature pieces live in `components/<feature>/`: `database/`, `docker/`, `files/`, `git/`, `logs/`,
`metrics/`, `packages/`, `procs/`, `proxy/`, `security/`, `terminal/`, `update/`.

### The editor is served from here, not from a CDN

`components/code-editor.tsx` wraps Monaco, and the line that matters is
`loader.config({ paths: { vs: "/monaco/vs" } })`. `@monaco-editor/react` otherwise fetches the editor from
`cdn.jsdelivr.net` at runtime — third-party JavaScript in the same origin as a session that drives the
Docker socket and a root shell, and a permanent spinner for an operator whose workstation has no egress,
which is the workstation this is meant to be reached from. `scripts/sync-monaco.mjs` copies
`monaco-editor/min/vs` into `public/monaco/vs` from `predev`/`prebuild` **and** explicitly in the
Dockerfile, because the image invokes next's entrypoint directly and never sees the npm hooks. The copy is
gitignored and excluded from eslint.

### Charts

`components/metrics/` is a third design-system file in all but name: every chart goes through it rather
than assembling its own recharts tree, which is how the old Overview page ended up with three tooltip
formats and no way to compare a moment across four charts. Adding a measurement should mean naming a
series.

- `metric-chart.tsx` — `MetricChart` + the `Series` descriptor. The x-axis is **numeric over time**, not a
  category axis of pre-formatted labels: a category axis spaces every bucket equally, which lies whenever
  the record has a hole in it. It owns the shared crosshair, drag-to-zoom, event markers, thresholds and
  the one tooltip listing every series at the hovered instant.
- `chart-panel.tsx` — the header/chart/legend shape and the empty state. A series with no numbers anywhere
  in the window is **dropped rather than drawn flat at zero**, which is what makes a kernel without PSI
  say so instead of reporting three healthy zeroes.
- `series-legend.tsx` — min/mean/max/last, plus **"At cursor"**: while the pointer is over any chart, every
  legend shows the value its series held at that instant. Max reads the peak column where a series has
  one, since the maximum of the *means* is exactly what a downsampled window hides.
- `sparkline.tsx` — a bare SVG path for a table cell, not recharts: forty containers would otherwise mount
  forty responsive containers and resize observers.
- `range-picker.tsx`, `health-panel.tsx` — the window control (pan and zoom-out appear only once a window
  has been dragged) and the verdict.

`lib/metrics-crosshair.ts` holds the hovered instant **outside React**, for the reason the live buffer is:
a pointer crossing a chart fires continuously, and a context above the router would re-render the terminal
and the log tail on every mousemove. The value is a **timestamp**, not a row index — charts on a page do
not share a row array.

### Feature panels

**Docker** (`components/docker/`) is where the product's opinion about newcomers lives.

- `explain.tsx` is the teaching layer — `Hint`, `Term` (a dotted underline, definition one hover away),
  `Field`, `GLOSSARY` — written for somebody who has never run a container and phrased around the decision
  rather than the mechanism. The rule it enforces is that **explanation is quiet**: a form that shouts
  every caveat is as unusable as one that explains nothing.
- `create-container.tsx`'s three entry points matter more than the form. **Paste a command** covers the
  common case (`lib/docker-run.ts` parses leniently and returns an honest list of what it could not
  represent). **Start from something common** is `lib/docker-templates.ts` — a dozen images almost every
  server runs, every one bound to 127.0.0.1; a set of starting points, not an app store, which is what
  goes stale and becomes the maintenance burden in Yacht and CasaOS. **From scratch** is for people who
  know what they want. The Command tab shows the server-rendered `docker run` and compose, live.
- `run-console.tsx` deliberately **does not** let `useSocket` reconnect: reconnecting re-issues the GET,
  and re-issuing the GET runs the command again — a redeploy fired twice because a VPN blinked is not a
  re-render. A dropped socket ends the run and says so.
- `diagnosis-panel.tsx` renders findings collapsed by default (a list, not a wall of argument) with the
  remedy as a button where the server named one; `ContainerFindings` filters the page's single pass.
- `stack-detail.tsx` is a stack as the application it is: clickable ports, the compose file editable in
  place (validated before saving — and saving is *not* deploying, which the UI says), one merged log feed
  tagged by service, links to Files, git and a shell in the stack's directory. `container-detail.tsx` adds
  the reachability join (published port + the proxy site pointing at it turns "running on 3000" into a
  URL), writable-layer view, editable limits, raw inspect, Update/Duplicate/Rename. `build-dialog.tsx` is
  where the git panel and Docker stop being two products: a repository we already pull is a build context.

Three deep links are worth preserving: `/files?path=`, `/git?repo=`, `/terminal?cwd=`. The first two are
read once as an initial value rather than kept in sync — the URL is where the reader arrived, not where
they are now — and the terminal one opens a session exactly once per mount, because a shell is a process.

**Security and proxy** (`components/security/`, `components/proxy/`) follow the same rule — teaching next
to the control, not in a banner above it. `posture-panel.tsx` turns a finding's `fix` into a button and
maps it to the request plus the confirmation it deserves. `rule-form.tsx` is why the catalogue lives on
the server: picking "Redis" fills in 6379 *and* raises the warning at the moment of choosing, not in a
report afterwards; the `ufw` line it would run is in the footer. `ssh-panel.tsx` stages every change and
applies them together, and its dialog says to keep the session open and check a second terminal — the one
piece of advice no error message can give afterwards. `site-form.tsx` shows the server-rendered nginx live
beside the form (same renderer that writes the file, so the form is not a black box), with `DNSCheck`
under the domain field because "the name does not point here yet" causes most certificate failures and
certbot reports it as "challenge failed". `tls-report.tsx` says out loud what `unknown` means for a
protocol row. `ConnectionsPanel` and `OffendersPanel` both offer a one-click firewall deny — the join that
makes the pages one product, since the address the ban log keeps naming deserves a rule outliving the ban.

**Packages.** `install-panel.tsx` updates as you type, which is not decoration: the reason people open a
terminal instead of a package page is that they do not know the name (`postgresql-client`, not `psql`;
`build-essential`, not `gcc`), and a form where you type a guess and press a button to find out you were
wrong is a form you use once. Install is one press on the row — there was a tray, and it cost a click and
a concept on every single-package install while protecting against an interruption a job survives anyway.
The command each button runs is its `title`. `package-sheet.tsx`'s second tab is the whole point of the
feature; the installed table caps at 400 rendered rows with the count said plainly underneath.

### The terminal panel

`components/terminal/` is the rail, strips and tags; `components/xterm-pane.tsx` is the emulator. The
split matters — the pane is reused by the compose runner and knows nothing about sessions.

- `session-rail.tsx` is the workspace tree. Four things carry the hierarchy, because the first version drew
  a folder and a session as the same row and put it in the data and nowhere on screen: a folder header is
  **chrome** (panel-header tint, icon in a tinted tile, uppercase label) while a session is content;
  children indent behind a rule in the folder's colour; colour is inherited; everything is draggable.
  Pinning sorts a session to the top *of its folder* — the earlier separate group made a starred session
  vanish from the folder it had been filed in.
- `dnd.ts` holds the in-flight payload **outside React** (`dragover` fires at pointer rate across the rail,
  and the browser will not let its handler read the payload — only MIME types). **The drag image is drawn
  by hand**: the browser's snapshot composites a transparent row onto an opaque white rectangle with hard
  corners and exposes no way to style it, so `dnd.ts` builds an off-screen chip in the theme's tokens,
  hands it to `setDragImage` and removes it next frame.
- `window-strip.tsx` is the window chips plus `PaneBar`. A pane's label is the command running in it:
  "pane 2" says nothing, `pg_dump` says which half of the screen not to close.
- `tags.tsx` is the colour vocabulary. `--tag-*` lives in `globals.css` and is the one deliberate exception
  to "compute it from the palette": a tag is a label the operator applied, and one that changed hue with
  the theme would stop being the same label. What *is* computed is everything drawn from it — row tint and
  edge rule are `color-mix` against the surface, so one lightness holds on a near-black card and a
  near-white one.
- `lib/terminal-settings.ts` keeps font, cursor, scrollback and behaviour in localStorage — on the screen,
  not the account, for the reason the theme is.
- `lib/terminal-keymap.ts` is every shortcut, all rebindable. A chord must get past the browser, the page
  and the shell, and no default annoys nobody — tmux settled that with a prefix key half the world
  rebinds. Ctrl+Alt is the default family (neither browser nor shell wants it); Ctrl+Shift is the
  emulator's own. Matching is on `event.code`, the **physical** key, so a binding recorded on QWERTY
  survives a Romanian layout. Actions carry a **scope** — `navigation` is the page's (it alone knows the
  sessions), `terminal` is the pane's (the compose runner needs copy/paste/search with no session at all)
  — and that split is what stops one keydown being handled twice. `shortcuts-dialog.tsx` is both cheatsheet
  and editor, because a read-only list is opened once and a hidden settings page never.

In `xterm-pane.tsx` and the page, load-bearing and easy to undo:

- **`forcePointerToSelect` takes the pointer back from tmux's mouse mode.** xterm gates mouse-report
  forwarding on one predicate (`shouldForceSelection`, asked by both its selection service and its
  forwarding, so answering once keeps them agreeing) and the pane inverts it: the drag belongs to the page
  unless **Alt** is held, left as the way through for vim, htop, less. The wheel is bound separately and
  does not consult it, so scrolling still belongs to tmux. Without this, a drag selected into tmux's copy
  buffer — which tmux clears on mouse-up, so text highlighted and unhighlighted inside one gesture and
  `getSelection()` stayed empty, which is why Copy reported nothing selected.
- **`clipboardKey`**: Ctrl+C copies **only when something is selected** and clears the selection as it
  goes, so the interrupt is never more than one keypress away. Ctrl+V returns false *without*
  `preventDefault`, so xterm leaves the key alone instead of sending ^V and the browser's own paste runs —
  arriving through `onData`, where the multi-line confirmation still sees it. Reading the clipboard there
  instead needs a permission Firefox does not grant at all.
- **Multi-line paste is confirmed, and the guard lives in `onData`.** A pasted block runs every line but
  the last immediately, and Ctrl+V, the context menu and the X11 middle click all arrive as one `onData`
  call — guarding only the Ctrl+Shift+V handler guarded the one route nobody uses. That handler must call
  `preventDefault`: returning false from `attachCustomKeyEventHandler` stops xterm, not the browser, so
  without it the confirmation opened *and* the native paste went through.
- **Replies are suppressed while the scrollback is replayed.** `CSI c` and friends are the shell asking the
  terminal a question, and xterm answers down the channel a keystroke uses — so replaying a buffer
  containing one typed `1;2c0;276` at whatever prompt exists now and left a column of "command not found".
  The server announces the replay with a `scrollback` frame before the binary snapshot (from the browser's
  side the bytes are identical either way) and the client drops its own output until xterm's write callback
  says the replay is parsed.
- `allowProposedApi` is on because the search addon's match count and highlight-all use xterm's decoration
  API, which is not frozen; without it `findNext` throws and the counter reads "none" over a scrollback
  full of matches.
- **Clicking inside a pane focuses it, and the arithmetic is the only way it can** — tmux composes every
  pane into one screen before the PTY sees a byte, so the browser has one terminal and no element to hang a
  handler on. `Panes` carries `pane_left/top/right/bottom`, `XtermPane` reports the clicked cell (the grid
  is uniform, and xterm publishes no pixel-to-cell mapping), the page finds the containing rectangle. Two
  details make it fire: it is a **native listener in the capture phase**, not a React `onMouseDown` (with
  tmux's mouse mode on, xterm's own `.xterm` handler calls `stopPropagation()` on exactly the clicks
  `forcePointerToSelect` hands back, and React binds at the root container — so a bubbling handler was
  never called and the pane bar was the only way to move focus); and the **focused pane's rectangle is
  tested first**, ending there, which is the cheap answer for the common case and the correct one for a
  zoomed window, where tmux resizes the zoomed pane to the whole window and leaves hidden panes' old
  rectangles where they were.
- **The navigation listener runs in the capture phase and must not skip the terminal.** Bubbling lands after
  xterm has forwarded the keystroke, so Ctrl+Alt+→ would switch the window *and* type an escape sequence.
  The usual "ignore keys while a text field has focus" guard needs an exception for `.xterm`, since xterm
  receives keystrokes through a hidden `.xterm-helper-textarea` — the plain form disables every shortcut
  exactly when the terminal has focus.
- **Shortcuts fire only where the shell has the keyboard.** These chords move sessions, close windows and
  kill panes; anywhere-in-the-workspace was too wide and closed a tmux window while the operator clicked
  around the file tree. The target must be inside `.xterm`, or nothing focused at all (`document.body` on a
  fresh load, which is the difference between "new session" having a shortcut and not). Any open dialog
  vetoes the lot, because focus sits on the body while one closes. The other half: **every switch hands the
  keyboard back** — the strips and pane bar are buttons and keep the focus they were given, so `XtermPane`
  takes a `focusRef` and the page calls it *before* the request (the switch is a round trip to tmux, the
  focus is not).

**The page has no header.** A terminal is the one screen whose content *is* the viewport, and a title band
plus a notice cost about a fifth of the pane on a laptop. The breadcrumb says where you are, "New session"
sits in the rail beside "New folder", and which account a shell runs as is on the pane's own header. The
one banner that stays is a missing login account — a broken feature rather than information.

### Data and theming

- `src/lib/api.ts` is the only fetch layer: `get/post/put/patch/del`, `credentials: "include"`,
  `X-Confirm` passthrough, `ApiError` with `needsConfirmation`/`isAuthProblem`/`needsTotp`; `wsUrl()` and
  `downloadUrl()` build the non-JSON URLs.
- `usePoll` — abort-per-run so a slow endpoint cannot stack requests, paused on a hidden tab.
- `useSocket` — reconnect with backoff (these sockets ride a tunnel that drops routinely), handlers in a
  ref so a fresh closure does not rebuild the socket.
- `useMetricsWindow` — the charts' window as a **stack**: zooming is exploratory, so the way out of five
  minutes is the hour it was inside, not the day you started from. Deliberately component state — a named
  range is a standing choice, a zoom is a question being asked now, and restoring yesterday's zoom shows an
  empty window with no obvious way out. `useMetricEvents`/`useHealth` poll on much slower cadences.
- `src/lib/types.ts` mirrors the backend's JSON by hand, including the `Capability` union — it drifts if
  backend types change without it. `useAuth`'s `can("capability")` hides controls a role cannot use:
  **affordance only**, the server re-decides every request.
- `ConfirmDialog` collects the typed phrase and the server re-checks it. Its `phrase` is optional and the
  absence is meaningful: a request without one is reversible but still deserves a pause (deleting a
  terminal folder loses a grouping and nothing else), and asking somebody to type "delete folder" teaches
  them to type phrases without reading — the one habit the typed confirmation exists to prevent.
- `lib/metrics-store.ts` keeps the live series **outside React**: owning five minutes of history in a route
  component threw it away on navigation, and pushing a 2 s frame through a context above the router
  re-rendered the terminal and log tail twice a second. Mirrored to sessionStorage so a reload keeps its
  chart, with points older than `STALE_MS` dropped on the way back in — a graph silently stitching this
  minute onto one from an hour ago is worse than one that starts empty.
- `lib/metrics-range.ts` defines the windows (`live`, `1h`, `6h`, `24h`, `7d`), their buckets and cadence,
  and the `MetricsWindow` a dragged span becomes (fixed in the past, fetched once, never re-polled).
  **Live and recorded data are never spliced into one line** — the cadences differ by two orders of
  magnitude, and a chart drawing twenty coarse points and a hundred fine ones at equal spacing lies about
  when things happened. Container charts offer only recorded ranges. `hooks/use-metrics.ts` and
  `hooks/use-metrics-history.ts` are the React surface over those two.
- `hooks/use-self-update.tsx` is one poll for the whole shell, and its gotcha is the feature's design
  problem: **the API goes away in the middle of the thing it is watching**. A failed poll during a run
  renders as "restarting", never an error, and the cadence is set from the fetch rather than an effect so a
  failure does not drop back to the five-minute interval. `components/update/` is the sidebar-footer notice
  (which renders *nothing* when there is nothing to say), the release-notes sheet, and the panel on
  `/dashboard` — that page is the dashboard's own version and nothing else, because the server's packages
  and the tool you look at it through are updated by completely different machinery.
- **Theming is light and dark, one palette**, in `globals.css`'s `:root` and `.dark`. `lib/themes.ts` holds
  only what does not belong in a component: `ThemeMode`, `DEFAULT_MODE` (dark), the storage key, and
  `themeBootstrapScript()`. That script is inlined in `<head>` so the stored choice applies **before first
  paint** — reading it after hydration flashes a screen of near-black at anyone who chose light, on every
  navigation that reloads the document; `<html>` carries `suppressHydrationWarning` for exactly that.
  `hooks/use-theme.tsx` treats the document as the store (`useSyncExternalStore` over the root class)
  rather than holding a second copy to sync in an effect. The choice is in localStorage, not on the
  account: it belongs to the screen you are sitting at. `/appearance` is the page; ⌘K is the shortcut.

## Conventions

- **Comments explain why, not what.** The prose in this codebase is unusually dense with rationale — match
  it, and when you change a behaviour a comment justifies, update the reasoning rather than deleting it.
- **Commit messages are imperative sentences describing intent**, not conventional-commit prefixes:
  "Report on the server, not on the container it runs in".
- Prettier for TS/TSX: no semicolons, double quotes, `printWidth: 100`, trailing commas. Go: standard
  formatting, no extra linter config.
- AGPL-3.0 with an additional grant to the owner — read CONTRIBUTING.md before touching licence headers or
  adding dependencies.

## Invariants that must not regress

A change that weakens any of these has to say so explicitly.

1. The network allowlist runs **before** authentication.
2. Two-factor is mandatory; a password-only session reaches nothing but the 2FA routes.
3. Every destructive action is behind `s.destructive` — capability, `destrLim`, audit entry — and pauses
   the operator with a confirmation dialog. A **subset** also requires the typed `X-Confirm` phrase,
   enforced server-side inside the handler. See below.
4. Capability checks live on the route, never in the UI alone. Where the answer depends on what is *in* the
   request, the handler checks by hand and fails closed: `dbx.Classify` for SQL, `api.authoriseSpec` for a
   container spec that is privileged or mounts a host path.
5. Every state-changing request lands in the audit log.
6. Client-supplied paths go through `files.Resolve` — including the ones that do not look like file
   operations (bind-mount source, build context, a new stack's directory). Host commands go through
   `hostexec` with an argv, never a shell string. The one shell is `deploy.Deployer.shell`, deliberately:
   those are pipelines an admin stored for their own project, not anything supplied per request. Do not add
   a second, and do not "fix" that one into an argv. `dockerx` invokes the `docker` binary in three places
   (compose, the streaming runner, `Build`) because the Engine API has no equivalent; all three build argv
   explicitly.
7. Nothing but Caddy binds a routable address.
8. Store schema changes are additive and tolerate an existing database. `CREATE TABLE IF NOT EXISTS` is a
   no-op against a table that exists, so a **column** added later also goes in `store.addedColumns`, which
   `applyAddedColumns` ALTERs in at open. Every entry needs a `DEFAULT` (SQLite refuses a NOT NULL column
   on a populated table without one) and **no entry is ever removed** — the list is the path from every
   shipped schema to the current one, not a description of the current one.

### Invariant 3: which routes take a typed phrase

**The test is frequency, not severity.** A phrase in front of something done a dozen times a day is not
read, it is typed — and the operator who has learned to type one table name without looking types the next
one the same way. That habit is exactly what the phrase protects on the routes that keep it, so every route
added to the typed set makes the set weaker. The question is not "is this dangerous" (they all are — that
is what `s.destructive` marks) but **"how often does somebody do this, and can they get it back"**.

**Typed — rare, and no way back:** `DROP DATABASE`, `DROP TABLE`, `DROP COLUMN`, `TRUNCATE`, an import that
truncates first, dropping a Mongo collection, a Mongo pipeline with `$out`/`$merge`, a `critical` statement
in the query runner, restoring a database or backup over live data, `compose down`, removing a Docker
volume, a prune that also sweeps volumes, deleting a dashboard or Linux account, a recursive directory
delete, `git discard` and `git reset --hard`, toggling the firewall, resetting it, switching the inbound
default to deny, changing sshd's configuration, revoking a certificate, applying package updates,
**purging** a package (removing it *and* deleting its /etc configuration), and installing a new version of
the dashboard itself.

The firewall and sshd entries are not about losing data: get one wrong and the way back into the machine is
gone, and no undo here helps, because reaching this UI is what you lost. They are also rare — an inbound
default is set once, an sshd hardening pass happens on the day the server is built. The self-update phrase
is the **version being installed** (`0.6`), not a fixed sentence: it names the object, as every other typed
route does, and *which version* is what has to be read before pressing a button in the sidebar.

**Not typed — routine, recoverable, or both:** deleting rows, documents and Redis keys; dropping an index;
forgetting a connection; stopping a database session; stopping/restarting/killing/removing/recreating a
container; removing an image or network; any prune that spares volumes; deleting one file; signalling a
process; ending an SSH session; stopping or restarting a service; revoking a token or SSH key; deleting a
backup job or deploy project; rolling back a deploy; disabling **or deleting** a vhost; deleting an nginx
stream or htpasswd file; deleting a git branch; adding, **editing** or deleting a firewall rule; tuning a
fail2ban jail; unbanning an address; stopping a running job; closing a terminal session, window or pane;
and **removing a package without purging it** — undone by installing it again, where the /etc files
somebody spent an afternoon on have no path back at all.

Editing a firewall rule is a write, not a destructive one, and is mounted accordingly: the replacement goes
in before the original comes out, so there is no moment the rule is missing. Stopping a job is the same
argument from the other side — interrupting is how you *avoid* a bad outcome, and a phrase in front of a
stop button is one somebody types while something is going wrong.

The 0.6.1 review narrowed the set: a prune sparing volumes (containers, networks and images come back from
a registry or a compose file), deleting a proxy site, nginx stream or htpasswd file (each recreated from
the same form), deleting a git branch (a pointer whose commits survive in the reflog and on the remote),
and ending an SSH session (a SIGHUP the operator reconnects past). All keep `s.destructive` and an ordinary
confirm dialog. `compose down` was reviewed and **kept** — it is the one compose action that removes rather
than stops containers, and on a host running several stacks typing the name guards against `down`-ing the
wrong one. `git discard` and `git reset --hard` were kept too: they overwrite uncommitted work, which the
reflog does not cover.

Several routes decide by content, and the narrowing lives at the call site: `handleDBQuery` types only for
`critical`, `handleFileDelete` only when `recursive`, `handleGitReset` only when `--hard`, `handleDBImport`
only when `truncate`, `handlePruneAll` only when `volumes=true`, `handlePackageRemove` only when `purge`,
`composeNeedsPhrase` only for `down` — with `requireComposePhraseWS` applying the same narrowing on the
socket, so the two entry points cannot disagree. The frontend mirrors each with a conditional `phrase`, and
the server re-decides regardless.

One relaxation: `httpx.RequireTypedConfirmationWS` also accepts the phrase as a query parameter, used only
by WebSocket routes where a browser cannot set a header at all and `wsx`'s origin check supplies what the
header guarded. **Do not reach for it from an ordinary handler.**

`api/handlers_db_test.go` pins both directions for the database surface
(`TestIrreversibleDatabaseRoutesDemandAPhrase`, `TestRoutineDatabaseRoutesDoNotAskForAPhrase`), because the
line has two failure modes and the second — a phrase creeping back onto routine work, one defensible route
at a time — is the quieter of the two.
