# Changelog

Every release of Just Dashboard, newest first.

**This file is generated.** The source is [`backend/internal/selfupdate/changelog.json`](backend/internal/selfupdate/changelog.json), which is the same file the dashboard reads — both the copy compiled into your build and the one it fetches to find out whether a newer version exists. Edit that, then run `scripts/release.sh <version>`.

## 0.5.3 — 26 August 2026

**Say why a database was not connected**

A database running on this server that the dashboard recognises but cannot reach — almost always a container on a Docker network with no published port — used to be skipped in silence. The reconcile now names it and says what is in the way.

### Added

- The reason a database was not connected, on the Databases page
  - Most often that its port is published only inside its own Docker network. Publish it on 127.0.0.1 and it connects itself on the next visit.

### Fixed

- A database that cannot be reached is named instead of silently skipped
  - Its credentials were read and its container recognised, then it was dropped without a word — so the Databases page appeared to do nothing about a database sitting in plain sight on the Docker page.

## 0.5.2 — 26 August 2026

**Ports that do not collide**

The dashboard's three ports — 8443, 8080 and 3000 — are the three most contested numbers on a Linux server, and a machine already using one of them got a dashboard that silently served somebody else's application. All three are now settings, and the installer picks free ones.

### Added

- JD_PORT, JD_BACKEND_PORT and JD_FRONTEND_PORT
  - Change any of the three in .env. The compose file and the proxy read the same variables, so they cannot drift apart.

### Changed

- install.sh checks all three ports and picks free ones
  - It says which port was taken and what it used instead, and the connection details it prints at the end carry the ports it actually chose.
- `bun dev` follows JD_BACKEND_PORT
  - A developer who moved the backend off 8080 no longer has to find a second variable to keep the dev proxy working.

### Fixed

- A port already in use no longer serves you the wrong application
  - Only the frontend and backend failed to bind; the proxy came up clean and forwarded to whatever already held the port, so you reached another app over the dashboard's own certificate with nothing anywhere saying why.

## 0.5.1 — 26 August 2026

**Updates, in the dashboard**

Just Dashboard now tells you when a new version exists, shows you exactly what is in it, and installs it for you. Until now the only way to find out was to visit the repository, and the only way to upgrade was to ssh in and rebuild by hand.

### Added

- A notice above your account in the sidebar when a newer version is out
  - It carries the version, what the release is called, and two buttons: install it, or read what changed first. It is not there at all when you are up to date.
- Release notes for every version between the one you run and the newest
  - An install three versions behind is upgrading past three sets of changes, so all three are shown rather than only the last.
- One-click install: pull, rebuild and restart the whole stack
  - The upgrade runs in a separate container so it survives the dashboard being rebuilt underneath it, and it waits for the dashboard to answer again before calling itself finished.
- A live transcript of the upgrade, and the outcome once it is done
  - The page keeps watching while the backend restarts, so you see the build output rather than a spinner and a guess.
- The dashboard's own version and history on the Updates page, beside the host's packages
- JD_UPDATE_CHECK, JD_UPDATE_REPO, JD_UPDATE_BRANCH and JD_UPDATE_DIR
  - Turn the online check off entirely, follow a fork, follow a different branch, or name the directory you installed into.

### Changed

- Local changes in the install directory are kept, not discarded
  - The upgrade fast-forwards rather than resetting, so an edited compose file or Caddyfile survives — and when one genuinely collides, the upgrade stops and says what is in the way instead of deleting it.

## 0.5 — 22 August 2026

**One panel for one server**

Metrics with history, Docker, processes, logs, a real terminal, files, git, eight database engines, the reverse proxy, the firewall, backups and deploys — behind one login with mandatory two-factor and a network allowlist in front of it.

### Added

- Overview with recorded history, saturation signals and a health verdict
- Docker: containers, stacks, images, volumes, networks, events and a diagnosis for each container
- A persistent terminal with tmux windows, panes, folders and colours
- Databases for PostgreSQL, MySQL, MariaDB, SQLite, SQL Server, ClickHouse, Oracle, MongoDB and Redis
- Files, git, logs, processes, proxy and TLS, firewall, backups and deployments
- Capability-based roles, API tokens, an audit trail and twelve themes

