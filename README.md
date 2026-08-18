# VPS Dashboard

Self-hosted management for a single Linux server: live system metrics, Docker,
processes, logs, a real shell, files, the reverse proxy, databases, firewall,
backups and deploy hooks — behind one authenticated UI.

Go backend (REST + WebSocket), Next.js frontend, deployed with Docker Compose.

---

## Read this first

**This dashboard is root-equivalent.** It manages systemd units, the firewall,
host accounts, the Docker socket and a PTY. Anyone who reaches it with a valid
session effectively has root on the machine.

It is built to be run **behind a VPN or an SSH tunnel, never on the public
internet.** Several things enforce that rather than merely suggest it:

- The backend **refuses to start** if it is bound to a non-loopback address
  without an explicit `VPSD_ALLOWED_CIDRS` allowlist.
- The allowlist is checked **before authentication**, so an attacker off-network
  cannot even reach the login handler to brute-force it.
- Two-factor authentication is **mandatory**. A correct password alone gets a
  session that is rejected everywhere except the 2FA endpoints.
- Every irreversible action requires the operator to **type a confirmation
  phrase**, enforced server-side so it cannot be skipped by calling the API
  directly.

---

## Quick start

```bash
git clone https://github.com/Wayy01/vps-dashboard.git
cd vps-dashboard
cp .env.example .env
```

Edit `.env`. Two settings matter more than the rest:

```bash
# Encrypts TOTP seeds, database connection strings, deploy env vars and backup
# credentials at rest. Generate it once — losing it loses every stored secret.
VPSD_MASTER_KEY=$(openssl rand -hex 32)

# The single address the dashboard answers on, and the name on its certificate.
# localhost = reachable only through an SSH tunnel (the safe default).
VPSD_SITE=localhost
```

Then:

```bash
docker compose up -d --build
docker compose logs backend | grep "bootstrap admin"
```

That last line prints the generated admin password **once**:

```
"msg":"bootstrap admin created — change this password immediately",
"username":"admin","password":"xK3p…"
```

### Reaching it

With the default `VPSD_SITE=localhost`, the dashboard listens only on the
server's loopback interface. Forward a port to it:

```bash
ssh -L 8443:localhost:8443 you@your-server
```

Then open **https://localhost:8443**. The certificate is signed by Caddy's own
local CA, so your browser warns once — accept it. (The connection is already
inside your SSH tunnel; the TLS layer is there so the session cookie's `Secure`
flag works, not because the tunnel needs encrypting twice.)

To reach it over WireGuard instead, set `VPSD_SITE` to the tunnel address
(for example `10.8.0.1`) and set `VPSD_ALLOWED_CIDRS=10.8.0.0/24`.

### First login

1. Sign in with `admin` and the generated password.
2. You are required to enroll two-factor authentication before anything else
   works. Add the shown secret to your authenticator app and enter a code.
3. **Save the ten recovery codes.** Each works once in place of your
   authenticator, and they are shown only at this moment.
4. Sign in again with a fresh code, then change the password under
   **Account → Security**.

---

## Configuration

All settings are environment variables read by the backend at boot.

### Required

| Variable | Description |
| --- | --- |
| `VPSD_MASTER_KEY` | 64 hex characters (32 bytes). Encrypts every stored secret. Generate with `openssl rand -hex 32`. |

### Network perimeter

| Variable | Default | Description |
| --- | --- | --- |
| `VPSD_SITE` | `localhost` | The address the stack answers on, and the name on its certificate. Never `0.0.0.0`. |
| `VPSD_ALLOWED_CIDRS` | `127.0.0.1/32,::1/128` | Who may reach the API at all, checked before authentication. Required if `VPSD_ADDR` is not loopback. |
| `VPSD_TRUSTED_PROXIES` | — | Addresses allowed to set `X-Forwarded-For`. Without this a client could spoof its way past the allowlist. |
| `VPSD_ADDR` | `127.0.0.1:8080` | Where the API binds. Leave on loopback; the proxy is the entry point. |
| `VPSD_ALLOWED_ORIGINS` | — | Extra browser origins allowed to open WebSockets. Only needed if the UI is served from a different origin than the API. |

### Behaviour

| Variable | Default | Description |
| --- | --- | --- |
| `VPSD_REQUIRE_2FA` | `true` | Mandatory two-factor. Turning it off is refused for accounts that already enrolled. |
| `VPSD_TERMINAL_ENABLED` | `true` | The web terminal. Set to `false` if you do not need a browser shell. |
| `VPSD_TERMINAL_SHELL` | `/bin/bash` | Shell spawned for terminal sessions. |
| `VPSD_SESSION_TTL` | `12h` | Absolute session lifetime. |
| `VPSD_SESSION_IDLE_TTL` | `60m` | Idle timeout. |
| `VPSD_DOCKER_HOST` | `unix:///var/run/docker.sock` | Docker Engine endpoint. |

### Where it looks

| Variable | Default | Description |
| --- | --- | --- |
| `VPSD_FILE_ROOTS` | `/` | Directories the file manager may reach. Narrow this if you want it scoped. |
| `VPSD_LOG_ROOTS` | `/var/log` | Directories the log viewer may read. |
| `VPSD_COMPOSE_ROOTS` | `/opt,/srv,/home` | Where compose stacks are discovered. |
| `VPSD_GIT_ROOTS` | `/opt,/srv,/home,/root` | Where the Git page looks for repositories. |
| `VPSD_NGINX_DIR` | `/etc/nginx` | nginx configuration root. |
| `VPSD_CADDYFILE` | `/etc/caddy/Caddyfile` | Caddy configuration file. |
| `VPSD_BACKUP_DIR` | `/var/backups/vps-dashboard` | Local backup destination and staging area. |
| `VPSD_DATA_DIR` | `/var/lib/vps-dashboard` | The dashboard's own SQLite database. **Back this up.** |

### First run only

| Variable | Default | Description |
| --- | --- | --- |
| `VPSD_BOOTSTRAP_USER` | `admin` | Username of the account created on an empty database. |
| `VPSD_BOOTSTRAP_PASSWORD` | — | Leave empty to have a strong one generated and logged once. |

---

## Roles

Roles are enforced per capability on the API, not in the UI. The frontend hides
controls a role cannot use, but the server re-decides every request on its own.

| | `readonly` | `limited` | `admin` |
| --- | :---: | :---: | :---: |
| View everything | ✅ | ✅ | ✅ |
| Start / stop / restart services | | ✅ | ✅ |
| Git fetch / pull / push / checkout / stash | | ✅ | ✅ |
| Edit files | | ✅ | ✅ |
| Web terminal and container shells | | | ✅ |
| Delete, prune, kill, restore, git discard / reset | | | ✅ |
| Apply system updates | | | ✅ |
| Host accounts, firewall, dashboard users, tokens | | | ✅ |

Manage accounts under **Account → Dashboard users**. New users must change
their password and enroll 2FA before anything works.

---

## What each page does

| Page | What it gives you |
| --- | --- |
| **Overview** | Live CPU per core, memory, swap, per-mount disk with an on-demand directory size scan, per-interface network rates, uptime and load. Pushed over WebSocket. |
| **Docker** | Containers with live `docker stats`-equivalent figures, streaming logs, a shell in the browser, inspect; plus images, volumes, networks, compose stacks and prune. |
| **Processes** | PM2 apps with merged stdout/stderr tailing; systemd units with journal streaming and enable/disable; an htop-style table with guarded kill; a crontab editor. |
| **Logs** | One viewer over files, container output, PM2 processes and the journal. Grep and level filters are applied **server-side** before lines are sent. Export by date range. |
| **Terminal** | A real PTY over WebSocket. tmux-backed, so sessions survive a closed tab or a dashboard restart, and detached ones can be reattached. |
| **Files** | Browse, upload, download, in-browser editing (Monaco), chmod/chown, search by name or content, archive create and extract. |
| **Git** | Every repository under the configured roots: branch, working tree, ahead/behind, history with per-commit diffs, branch switching, and fetch/pull/push/stash. Git runs as the account that owns each repository, so nothing it writes changes ownership. |
| **Proxy & TLS** | nginx/Caddy config editor that validates with the server's own test before anything is written or reloaded, vhost enable/disable, certificate inventory, live TLS checks, listening ports joined to owning processes. |
| **Databases** | Postgres, MySQL and MongoDB: schema browsing, paged table browser, a query runner that classifies destructive statements before running them, dumps and restores. |
| **Security** | Firewall rules, fail2ban jails with unban, active SSH sessions. |
| **Updates** | Which OS packages are behind, which of those are security updates, and whether the machine needs a reboot. Applies them with `apt-get upgrade` — never installing new packages or removing any. |
| **Deployments** | Git pull plus `docker compose up -d --build`, triggered by hand or a signed webhook, with history and rollback to any previous commit. |
| **Backups** | Scheduled archives to local disk, S3 or Backblaze B2, with retention, history and restore. |
| **System users** | Host accounts with SSH key management, lock/unlock, add and remove. |
| **Audit log** | Every state-changing request: who, what, when, from where, and whether it succeeded. |

---

## API tokens

For scripting. Create one under **Account → API tokens**, then:

```bash
curl -H "Authorization: Bearer vpsd_…" https://localhost:8443/api/system/metrics
```

A token can narrow its creator's role but never widen it, and it is demoted
automatically if that account is demoted. Tokens cannot be used for interactive
actions — changing a password, minting other tokens, managing accounts — which
require a real session.

---

## Deploy webhooks

Create a project under **Deployments**. You get a hook URL and a secret shown
once. Point CI at it:

```bash
BODY='{"ref":"refs/heads/main"}'
SIG="sha256=$(printf '%s' "$BODY" | openssl dgst -sha256 -hmac "$SECRET" | awk '{print $2}')"

curl -X POST "https://your-dashboard:8443/api/hooks/deploy/$HOOK_ID" \
  -H "Content-Type: application/json" \
  -H "X-Hub-Signature-256: $SIG" \
  -d "$BODY"
```

This is the format GitHub sends by default, so a GitHub webhook works with no
extra glue. The endpoint is the only one that does not use a dashboard session —
it authenticates by HMAC over the raw body — and it still sits behind the
network allowlist.

On each delivery the project is fetched, hard-reset to the configured branch,
its encrypted environment is rendered into `.env`, and the stack is rebuilt.

---

## Local development

```bash
# Backend on :8080
cd backend
VPSD_MASTER_KEY=$(openssl rand -hex 32) \
VPSD_ADDR=127.0.0.1:8080 \
VPSD_DATA_DIR=./data \
VPSD_DEV=true \
VPSD_ALLOWED_ORIGINS=http://127.0.0.1:3000 \
go run ./cmd/server

# Frontend on :3000
cd frontend
bun install
cp .env.example .env.local   # points the API and WebSocket at :8080
bun dev
```

`VPSD_DEV=true` drops the `Secure` flag from the session cookie so it works over
plain HTTP. Never set it in production.

`VPSD_ALLOWED_ORIGINS` is needed in development because the UI (`:3000`) and the
API (`:8080`) are different origins, and the API refuses cross-origin WebSocket
upgrades by default. In production both sit behind one proxy and this is unset.

### Checks

```bash
cd backend  && go build ./... && go vet ./... && go test ./...
cd frontend && bun run lint && bun run build
```

---

## How it fits together

```
browser ──(WireGuard / SSH tunnel)──▶ Caddy :8443
                                        ├─ /api/* ─▶ backend :8080  (loopback)
                                        └─ /*     ─▶ frontend :3000 (loopback)
```

Only Caddy binds a routable address. The backend and frontend listen on
loopback and are unreachable from outside the machine.

Serving the UI and the API from **one origin** is not cosmetic: the session
cookie is `HttpOnly; SameSite=Strict` and the API rejects cross-origin
WebSocket upgrades, so a split origin would break both by design.

Every API request passes through the same chain:

```
network allowlist → rate limit → authenticate → capability check → handler
```

Destructive routes additionally require a typed confirmation phrase and get a
tighter rate budget.

### A note on privileges

The compose file grants the backend `privileged: true`, `pid: host` and the
Docker socket. That is what makes "restart this unit", "kill this process" and
"manage these containers" mean anything — but it means the security boundary is
the network perimeter plus 2FA, **not** the container. Read
`docker-compose.yml` before deploying and narrow the mounts if your use case
allows it.

### Reaching the host, not the container

The dashboard manages a server, so everything it reports has to be about the
server rather than about the container it happens to run in. Two mechanisms
keep that true, and both matter if you change the compose file:

**The directories it manages are mounted at their real paths.** `/home`,
`/opt`, `/srv`, `/root`, `/etc` and `/var/log` appear inside the container
under the same names they have outside it, because the dashboard addresses
files by the path you would type over SSH. Remove one of those mounts and the
file manager, git discovery and compose-stack scanning will quietly browse the
container's own empty filesystem instead of your server's. Narrow the mounts if
you like, but narrow `VPSD_FILE_ROOTS`, `VPSD_GIT_ROOTS` and
`VPSD_COMPOSE_ROOTS` to match.

**Host tools run on the host.** Some things cannot be mounted. nginx's config
is readable here but its binary is not, and shipping a second copy would
validate your config against different modules than the server actually uses.
Anything in that category — `nginx`, `caddy`, `ufw`, `fail2ban-client`, `who` —
is executed in the host's namespaces through `nsenter`, using the privilege the
container already holds. This is also why the dashboard can tell you fail2ban
is *not installed* rather than reporting on a copy that came with its own
image.

Where a tool writes files, it runs as the account that owns the directory it is
working in, so a `git pull` on a repository owned by `deploy` leaves files
owned by `deploy` rather than by root.

---

## Backing it up

The dashboard's own state lives in `VPSD_DATA_DIR` (`/var/lib/vps-dashboard` by
default) as a SQLite database. It holds accounts, TOTP enrollments, API tokens,
the audit log, and the encrypted database/deploy/backup secrets.

Back up that directory **and** store `VPSD_MASTER_KEY` somewhere separate.
Either one alone is not enough to recover.
