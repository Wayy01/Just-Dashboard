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
- **Three rate limiters** on the `Server`: `loginLim`, `apiLim` (per principal),
  `destrLim` (destructive routes).
- **Capabilities, not roles** (`internal/auth/roles.go`): `read`, `service.control`,
  `file.write`, `terminal`, `destructive`, `system.admin`. Routes gate on
  `httpx.RequireCapability(...)` so adding a role later cannot silently widen an endpoint.
  `httpx.RequireSession` additionally blocks API-token principals from human-only routes
  (password change, minting tokens, account management).
- **`httpx.AuditMutations`** records every state-changing request. WebSocket routes are GET
  and long-lived, so they call `s.recordAudit(...)` at open time instead.

Two exceptions to the chain, both deliberate: `/healthz` (unauthenticated, reveals nothing)
and `/api/v1/hooks/deploy/{hookID}` (HMAC over the raw body, still behind the allowlist).

### Handler conventions (backend)

Handlers are `func(w http.ResponseWriter, r *http.Request) error`, i.e. `httpx.Handler`, whose
own `ServeHTTP` renders a returned error. `s.handle(...)` at the mount site is just the
conversion to `http.Handler` — it adds no behaviour. Return `httpx.Err/BadRequest/Internal/Wrap`
— never write an error body by hand; `httpx.WriteError` is the single renderer and is what
keeps internal error strings off the wire. Decode bodies with `httpx.DecodeJSON` (4 MB cap, unknown fields rejected).

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

### Server, modules, degradation

`api.Server` holds the dependency graph; `api/modules.go` (`moduleSet`) holds the feature
backends (docker, procs, logsx, term, files, gitx, updates, proxysvc, dbx, linuxusers,
netsec, backups, deploy, metrics). Each is optional by design: a host with no Docker socket, no
systemd or no fail2ban still serves everything else, and the affected routes return a
precise "unavailable on this host" code that the frontend renders as information rather than
an error (see `ErrorState` in `frontend/src/components/state.tsx`).

Handlers live in `api/handlers_*.go`, one file per feature, each with its own
`mount<Feature>Routes(r chi.Router)` called from `Routes()`.

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

### Auth, sessions and secrets

`internal/auth` owns users, sessions, TOTP, recovery codes and API tokens.
Session cookie is `vpsd_session` (HttpOnly, SameSite=Strict, Secure unless `JD_DEV`).
A password alone yields a *partial* session accepted only by the 2FA routes
(`AuthenticatePartial`); everything else rejects it with `totp_required` /
`totp_enrollment_required`. API tokens may narrow their creator's role, never widen it, and
are demoted with the account.

`auth.Sealer` (from the 64-hex `JD_MASTER_KEY`) encrypts every stored secret — TOTP seeds,
database connection strings, deploy env, backup credentials. State is SQLite in
`JD_DATA_DIR`; the schema is a single `CREATE TABLE IF NOT EXISTS` block in
`internal/store/store.go` with no migration tool, so schema changes must be additive and
tolerate an existing database.

### Metrics history

`internal/metrics` samples the host on the server's own timer and stores the result in
SQLite, because the live socket can only ever describe the time since a browser tab was
opened — a dashboard whose charts start empty on every visit cannot show the spike that
happened overnight, which is most of the reason to have charts. `GET
/system/metrics/history` aggregates a window into at most N buckets **in SQL**, and every
series carries its bucket's peak next to its mean: a 100% second inside a ten-minute
bucket averages away to nothing, so a chart drawn only from means reports a quiet night
that was not quiet.

Capacity is recorded per filesystem (`GET /system/metrics/storage`), not as one
worst-of line: when the fullest mount stops being the fullest, a single line drops to
whatever the runner-up was and reads as space freed on a disk that never changed.

The same recorder samples every running container (`GET
/docker/containers/{id}/stats/history`). That series is keyed by container **name**, not
id: a compose redeploy replaces the container with a new id, and seeing across the
restart is most of the point. Docker being unavailable is not an error there — the
recorder logs it once and carries on with the host metrics. The recorder keeps its own `sysinfo.Collector`, since rates are deltas
against the previous call and sharing one with the request handlers would let a one-shot
`GET /system/metrics` shorten the interval the next recorded rate is divided by.

### History the host already keeps

Not everything worth showing needs recording. `netsec` reads three records the machine
writes on its own — wtmp (`GET /logins`), btmp (`GET /logins/failed`) and fail2ban's log
(`GET /fail2ban/history`) — rather than polling and remembering. Polling a jail for its
banned set would invent the events between samples and miss every ban shorter than the
interval; the log is the record. btmp sits behind `system.admin` because it holds whatever
was typed at a login prompt, which is sometimes a password in the username field.

### Streaming

`internal/wsx` wraps gorilla/websocket: origin check on upgrade (a WS handshake is not
subject to CORS, so this is what stops a malicious page using the operator's cookie),
serialised writes, ping/pong, 1 MB read limit. Frames are `Envelope{type, data, error, ts}`,
consumed by `useSocket` on the frontend. Server-side filtering is the rule — log grep and
level filters are applied *before* lines are sent.

### Agent mode

`JD_AGENT_MODE` / `-agent` swaps the human login surface for mutual TLS: no password route,
no session, no 2FA; `httpx.HubOnly` admits only the enrolled hub's certificate, and
`/agent/enrol` is the one route reachable before enrolment. The feature routes are the same
program either way — only who may reach them changes. Not useful standalone yet.

### Configuration

`internal/config` resolves everything from `JD_*` environment variables at boot and **fails
closed**: no `JD_ALLOWED_CIDRS` with a non-loopback bind is a startup error, and a missing or
malformed `JD_MASTER_KEY` is fatal. `config.Env` falls back to the legacy `VPSD_*` prefix,
and `adoptLegacyDir` picks up pre-rename `/var/lib/vps-dashboard` data — an install that
predates the rename must keep working.

Env vars are documented in four places that must stay in step: `config.go`, `.env.example`,
`docker-compose.yml` and the README's configuration table.

### Deployment topology

```
browser ──(Tailscale / SSH tunnel)──▶ Caddy :8443
                                        ├─ /api/* ─▶ backend :8080  (loopback)
                                        └─ /*     ─▶ frontend :3000 (loopback)
```

Caddy (`deploy/Caddyfile`) is the only listener bound to anything but loopback, and it binds
`{$JD_SITE}` **plus** loopback explicitly — site addresses alone would leave Caddy listening
on every interface. Serving UI and API from one origin is load-bearing: `SameSite=Strict`
cookies and the same-origin WebSocket check both depend on it. The backend container runs
`privileged`, `pid: host`, `network_mode: host` with the Docker socket and real host paths
mounted at their real names — remove a mount and the file manager silently browses the
container's own empty filesystem.

### Frontend

App Router, all pages `"use client"`, one page per feature under
`src/app/(dashboard)/<feature>/page.tsx`; `(dashboard)/layout.tsx` is the authenticated
shell (its redirect is convenience, not a control).

- `src/lib/api.ts` is the only fetch layer: `get/post/put/patch/del`, `credentials: "include"`,
  `X-Confirm` passthrough, `ApiError` with `needsConfirmation` / `isAuthProblem` / `needsTotp`.
  `wsUrl()` and `downloadUrl()` build the non-JSON URLs.
- Data comes from `usePoll` (abort-per-run, paused on a hidden tab) or `useSocket`
  (reconnect with backoff, handlers kept in a ref so a fresh closure does not rebuild the
  socket).
- `src/lib/types.ts` mirrors the backend's JSON shapes by hand, including the `Capability`
  union — it drifts if backend types change without it.
- `useAuth`'s `can("capability")` predicate hides controls a role cannot use. This is UI
  affordance only; the server re-decides every request.
- `ConfirmDialog` collects the typed phrase and passes it to the API call; the server
  re-checks it.
- `components/ui/*` is generated shadcn/ui (new-york, zinc, lucide). Prefer composing over
  editing these; feature-specific pieces live in `components/<feature>/`.

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
   with an argv, never a shell string.
7. Nothing but Caddy binds a routable address.
