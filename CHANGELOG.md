# Changelog

Every release of Just Dashboard, newest first.

**This file is generated.** The source is [`backend/internal/selfupdate/changelog.json`](backend/internal/selfupdate/changelog.json), which is the same file the dashboard reads — both the copy compiled into your build and the one it fetches to find out whether a newer version exists. Edit that, then run `scripts/release.sh <version>`.

## 0.5.7 — 26 August 2026

**Selecting and copying in the terminal, as everywhere else**

Text dragged in a terminal pane unhighlighted itself the moment the mouse came up, and the Copy button and its shortcut both answered that nothing was selected. tmux owned the pointer; it now owns only the wheel.

### Added

- Ctrl+C copies the selection, and interrupts when there is nothing selected
  - Copying clears the selection as it goes, so the next Ctrl+C is an interrupt again. Ctrl+Shift+C still works and is still rebindable.
- Ctrl+V pastes
  - It used to send a literal ^V instead. The multi-line paste confirmation still sees it.

### Changed

- Hold Alt to hand the mouse to the program in the pane
  - For vim, htop, less and anything else that wants to be clicked. The wheel still scrolls the session's history with no modifier at all.

### Fixed

- A plain drag selects text, and it stays selected
  - tmux's mouse mode was taking the drag, drawing its own selection and clearing it on mouse-up — so the browser never had one, which is why every way of copying reported an empty selection.

## 0.5.6 — 26 August 2026

**Connect the database your application already uses**

A database on a Docker network with no published port — how nearly every application ships its own Postgres — was refused as unreachable. It was never unreachable: the dashboard shares the host's network namespace, and a Docker bridge is routable from there.

### Changed

- A published port is still preferred where there is one
  - It survives the container being recreated. A container address does not, so a connection made that way needs reconnecting after a redeploy — the Databases page marks which is which.

### Fixed

- A database with no published port now connects at its container address
  - The commonest database on any server this runs on was the one database the dashboard declined, while psql from the same machine worked fine.

## 0.5.5 — 26 August 2026

**The updater can build again**

Installing an update failed while building, with "the --mount option requires BuildKit". The updater's image was missing the buildx plugin, so it quietly built with the classic builder — which cannot read this project's own Dockerfile.

### Changed

- Builds are slower the first time and correct every time
  - The Go build cache mounts are gone, because they are a BuildKit extension the classic builder refuses outright rather than ignores.

### Fixed

- Installing an update no longer fails with "the --mount option requires BuildKit"
  - Compose delegates builds to buildx and falls back to the classic builder without saying so when it is absent. The image now carries buildx, and the Dockerfile no longer needs it — so an install already stuck on this can update its way out.

## 0.5.4 — 26 August 2026

**A taken port fails loudly**

A port already in use now stops the stack with a message naming it, instead of leaving the dashboard quietly serving whatever already held the port. Upgrading installs get their ports filled in automatically.

### Changed

- Re-running install.sh fills in ports missing from an older .env
  - No hand-editing to upgrade an install made before the ports were settings. A port already recorded is never moved — that was a deliberate choice, and guessing whether the process holding it is this dashboard is how a working install gets broken.

### Fixed

- A port already in use stops the stack instead of serving the wrong application
  - The frontend runs on its own network with its port published, so Docker refuses to start it and names the port. Previously it failed to bind, restarted in a loop, and the proxy forwarded to whatever already held the port.
- install.sh no longer exits early on an .env without the port settings
  - Reading an absent variable aborted the script under set -euo pipefail, so a re-run stopped before printing how to connect.

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

