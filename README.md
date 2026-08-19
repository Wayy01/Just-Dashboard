# VPS Dashboard

One authenticated UI for a single Linux server — metrics, Docker, processes,
logs, a real shell, files, git, databases, the reverse proxy, the firewall,
backups and deploys. Go backend, Next.js frontend, one `docker compose` stack.

![The overview page](docs/overview.png)

---

## Install

```bash
git clone https://github.com/Wayy01/vps-dashboard.git
cd vps-dashboard && sudo ./install.sh
```

It asks four or five questions, and the one that matters is how you intend to
reach it. **Tailscale is the default** — your laptop and phone get in from
anywhere while the machine stays invisible to the internet — and the installer
will set it up for you. An SSH tunnel is the fallback: no extra software, no
account, still works if Tailscale is ever down.

Then it generates the master key and a first password, writes `.env`, builds,
waits for the stack to answer, and prints the exact command to get in.

Re-running it later keeps your `.env` and just rebuilds, so it is safe after a
`git pull`.

---

## Before you expose it

**This dashboard is root-equivalent.** It drives systemd, the firewall, host
accounts, the Docker socket and a PTY. Anyone who reaches it with a valid
session has root on the machine.

It is built to sit behind a VPN or an SSH tunnel, and that is enforced rather
than suggested:

- The backend **refuses to start** on a non-loopback address without an
  explicit `VPSD_ALLOWED_CIDRS` allowlist.
- The allowlist is checked **before authentication** — off-network, you cannot
  even reach the login handler to guess at it.
- Two-factor is **mandatory**. A correct password alone yields a session that
  is rejected everywhere except the 2FA endpoints.
- Irreversible actions require a **typed confirmation phrase**, enforced
  server-side so it cannot be skipped by calling the API directly.
- Every state-changing request lands in an **audit log**: who, what, when, from
  where, and whether it worked.

The **Security** page tells you how the dashboard is actually reachable and
says so plainly when that is broader than a private network — so a machine that
quietly became internet-facing announces itself instead of waiting to be found.

---

## What you get

| | |
| --- | --- |
| **Overview** | Live CPU per core, memory, swap, per-mount disk with an on-demand directory size scan, per-interface network rates. Pushed over WebSocket. |
| **Docker** | Containers with live stats, streaming logs, a shell in the browser, inspect — plus images, volumes, networks, compose stacks and prune. |
| **Processes** | PM2 apps with merged output tailing, systemd units with journal streaming, an htop-style table with guarded kill, and a crontab editor. |
| **Logs** | One viewer over files, container output, PM2 and the journal. Grep and level filters apply **server-side**, before lines are sent. |
| **Terminal** | A real PTY over WebSocket, tmux-backed — sessions survive a closed tab or a dashboard restart. |
| **Files** | Browse, upload, download, edit in Monaco, chmod/chown, search by name or content, archive and extract. |
| **Git** | Every repository under the configured roots: branch, working tree, ahead/behind, history with diffs, fetch/pull/push/stash. Runs as the account that owns each repo, so nothing changes ownership. |
| **Proxy & TLS** | nginx/Caddy editor that validates with the server's own test before writing or reloading, vhost toggles, certificate inventory, listening ports joined to owning processes. |
| **Databases** | Postgres, MySQL and MongoDB — schema browsing, paged table browser, a query runner that classifies destructive statements before running them, dumps and restores. |
| **Security** | How the dashboard is exposed, firewall rules, fail2ban jails, active SSH sessions. |
| **Updates** | What is behind, which of it is security, and whether a reboot is due. Upgrades only — never installs or removes. |
| **Deployments** | Git pull plus `compose up -d --build`, by hand or signed webhook, with history and rollback. |
| **Backups** | Scheduled archives to local disk, S3 or Backblaze B2, with retention and restore. |
| **System users** | Host accounts, SSH keys, lock and unlock. |
| **Audit log** | Every state-changing request, filterable. |

![The files page](docs/files.png)

---

## Roles

Enforced per capability on the API, never in the UI alone — the frontend hides
what a role cannot use, and the server re-decides every request anyway.

| | `readonly` | `limited` | `admin` |
| --- | :---: | :---: | :---: |
| View everything | ✅ | ✅ | ✅ |
| Start / stop / restart services | | ✅ | ✅ |
| Git fetch / pull / push / checkout | | ✅ | ✅ |
| Edit files | | ✅ | ✅ |
| Terminal and container shells | | | ✅ |
| Delete, prune, kill, restore, git reset | | | ✅ |
| Apply system updates | | | ✅ |
| Host accounts, firewall, users, tokens | | | ✅ |

New accounts must change their password and enrol 2FA before anything works.

---

<details>
<summary><b>Configuration reference</b> — every environment variable</summary>

All settings are environment variables read by the backend at boot. The
installer writes the ones that matter; these are for tuning afterwards.

**Required**

| Variable | Description |
| --- | --- |
| `VPSD_MASTER_KEY` | 64 hex characters. Encrypts every stored secret. `openssl rand -hex 32`. |

**Network perimeter**

| Variable | Default | Description |
| --- | --- | --- |
| `VPSD_SITE` | `localhost` | The address the stack answers on and the name on its certificate. Your Tailscale address is the recommended value. Never `0.0.0.0`. |
| `VPSD_ALLOWED_CIDRS` | `127.0.0.1/32,::1/128` | Who may reach the API at all, checked before authentication. `100.64.0.0/10` for Tailscale. |
| `VPSD_TRUSTED_PROXIES` | — | Addresses allowed to set `X-Forwarded-For`. Without it a client could spoof past the allowlist. |
| `VPSD_ADDR` | `127.0.0.1:8080` | Where the API binds. Leave on loopback; the proxy is the entry point. |
| `VPSD_ALLOWED_ORIGINS` | — | Extra browser origins allowed to open WebSockets. Only if the UI is served from a different origin. |

**Behaviour**

| Variable | Default | Description |
| --- | --- | --- |
| `VPSD_REQUIRE_2FA` | `true` | Mandatory two-factor. Refused for accounts that already enrolled. |
| `VPSD_TERMINAL_ENABLED` | `true` | The web terminal. |
| `VPSD_TERMINAL_SHELL` | `/bin/bash` | Shell spawned for terminal sessions. |
| `VPSD_SESSION_TTL` | `12h` | Absolute session lifetime. |
| `VPSD_SESSION_IDLE_TTL` | `60m` | Idle timeout. |
| `VPSD_DOCKER_HOST` | `unix:///var/run/docker.sock` | Docker Engine endpoint. |
| `VPSD_AGENT_MODE` | `false` | Run as an agent managed by a hub: no login, mutual TLS only. Not useful on its own yet. |

**Where it looks**

| Variable | Default | Description |
| --- | --- | --- |
| `VPSD_FILE_ROOTS` | `/` | Directories the file manager may reach. |
| `VPSD_LOG_ROOTS` | `/var/log` | Directories the log viewer may read. |
| `VPSD_COMPOSE_ROOTS` | `/opt,/srv,/home` | Where compose stacks are discovered. |
| `VPSD_GIT_ROOTS` | `/opt,/srv,/home,/root` | Where the Git page looks for repositories. |
| `VPSD_NGINX_DIR` | `/etc/nginx` | nginx configuration root. |
| `VPSD_CADDYFILE` | `/etc/caddy/Caddyfile` | Caddy configuration file. |
| `VPSD_BACKUP_DIR` | `/var/backups/vps-dashboard` | Local backup destination and staging. |
| `VPSD_DATA_DIR` | `/var/lib/vps-dashboard` | The dashboard's own database. **Back this up.** |

**First run only**

| Variable | Default | Description |
| --- | --- | --- |
| `VPSD_BOOTSTRAP_USER` | `admin` | Account created on an empty database. |
| `VPSD_BOOTSTRAP_PASSWORD` | — | Leave empty for a generated one, logged once. |

</details>

<details>
<summary><b>Scripting it</b> — API tokens and deploy webhooks</summary>

**API tokens.** Create one under **Account → API tokens**:

```bash
curl -H "Authorization: Bearer vpsd_…" https://localhost:8443/api/v1/system/metrics
```

A token can narrow its creator's role but never widen it, and is demoted
automatically if that account is. Tokens cannot change a password, mint other
tokens or manage accounts — those need a real session.

**Deploy webhooks.** Create a project under **Deployments** for a hook URL and
a secret shown once:

```bash
BODY='{"ref":"refs/heads/main"}'
SIG="sha256=$(printf '%s' "$BODY" | openssl dgst -sha256 -hmac "$SECRET" | awk '{print $2}')"

curl -X POST "https://your-dashboard:8443/api/v1/hooks/deploy/$HOOK_ID" \
  -H "Content-Type: application/json" \
  -H "X-Hub-Signature-256: $SIG" \
  -d "$BODY"
```

That is the format GitHub sends by default, so a GitHub webhook needs no glue.
It is the only endpoint without a dashboard session — it authenticates by HMAC
over the raw body — and it still sits behind the network allowlist.

On delivery the project is fetched, hard-reset to its branch, its encrypted
environment is rendered into `.env`, and the stack is rebuilt.

</details>

<details>
<summary><b>How it fits together</b> — architecture and the privilege model</summary>

```
browser ──(Tailscale / SSH tunnel)──▶ Caddy :8443
                                        ├─ /api/* ─▶ backend :8080  (loopback)
                                        └─ /*     ─▶ frontend :3000 (loopback)
```

Only Caddy binds a routable address; the backend and frontend are unreachable
from outside the machine. Serving both from **one origin** is not cosmetic —
the session cookie is `HttpOnly; SameSite=Strict` and the API rejects
cross-origin WebSocket upgrades, so a split origin would break both by design.

Every request passes the same chain:

```
network allowlist → rate limit → authenticate → capability → handler
```

Destructive routes additionally require a typed confirmation and get a tighter
rate budget.

**On privileges.** The compose file grants the backend `privileged: true`,
`pid: host` and the Docker socket. That is what makes "restart this unit" and
"kill this process" mean anything — but it means the security boundary is the
network perimeter plus 2FA, **not** the container. Read `docker-compose.yml`
before deploying and narrow the mounts if your use case allows.

**Reaching the host, not the container.** The dashboard manages a server, so
everything it reports has to be about the server rather than the container it
runs in. Two mechanisms keep that true:

*The directories it manages are mounted at their real paths.* `/home`, `/opt`,
`/srv`, `/root`, `/etc` and `/var/log` appear inside the container under the
same names, because the dashboard addresses files by the path you would type
over SSH. Remove a mount and the file manager, git discovery and compose
scanning quietly browse the container's own empty filesystem instead. Narrow
them if you like — but narrow `VPSD_FILE_ROOTS`, `VPSD_GIT_ROOTS` and
`VPSD_COMPOSE_ROOTS` to match.

*Host tools run on the host.* nginx's config is readable here but its binary is
not, and shipping a second copy would validate your config against different
modules than the server actually uses. Anything in that category — `nginx`,
`caddy`, `ufw`, `fail2ban-client`, `who` — runs in the host's namespaces via
`nsenter`. It is also why the dashboard can tell you fail2ban is *not
installed* rather than reporting on a copy that shipped in its own image.

Where a tool writes files it runs as the account owning that directory, so a
`git pull` on a repo owned by `deploy` leaves files owned by `deploy`.

</details>

<details>
<summary><b>Developing on it</b></summary>

```bash
# Backend on :8080
cd backend && go run ./cmd/server

# Frontend on :3000
cd frontend && bun install && bun dev
```

The frontend proxies `/api` to the backend in development, so no CORS setup is
needed. WebSockets are the exception — Next does not proxy upgrades, so set
`NEXT_PUBLIC_WS_BASE=http://localhost:8080` to point them at the backend
directly.

Before opening a pull request:

```bash
cd backend  && go build ./... && go vet ./... && go test ./...
cd frontend && bun run lint && bun run build
```

</details>

<details>
<summary><b>Setting it up by hand</b> — without the installer</summary>

```bash
cp .env.example .env
```

Edit it. Two settings matter more than the rest:

```bash
# Encrypts TOTP seeds, database strings, deploy env and backup credentials.
# Generate once — losing it loses every stored secret.
VPSD_MASTER_KEY=$(openssl rand -hex 32)

# The address the dashboard answers on. Your Tailscale address is recommended;
# localhost means reachable only through an SSH tunnel.
VPSD_SITE=localhost
```

Then:

```bash
docker compose up -d --build
docker compose logs backend | grep "bootstrap admin"
```

That last line prints the generated admin password **once**.

</details>

---

## Backing it up

The dashboard's own state is a SQLite database in `VPSD_DATA_DIR`
(`/var/lib/vps-dashboard`). It holds accounts, TOTP enrolments, API tokens, the
audit log and every encrypted secret.

Back up that directory **and** keep `VPSD_MASTER_KEY` somewhere separate.
Either alone will not restore.

---

## Licence

[AGPL-3.0](LICENSE). Run it, change it, distribute it — but if you run a
modified version as a network service, publish your changes. Contributions are
welcome under the terms in [CONTRIBUTING.md](CONTRIBUTING.md).
