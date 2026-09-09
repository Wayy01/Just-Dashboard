# Frontend feature map

The App Router keeps route entry files thin where a feature has reusable panels and colocates page-only
orchestration where splitting it would hide the workflow. Backend capability checks remain authoritative;
hiding a control through `useAuth().can()` is affordance only.

| Route area | UI responsibility | Primary implementation |
| --- | --- | --- |
| `/login` | Password, mandatory TOTP enrollment/challenge, recovery, and partial-session states | `src/app/login/page.tsx`, auth hooks, shared logo/state/controls |
| `/` | Host overview, current health, capacity, recent events, and sparklines | dashboard root page plus `components/metrics/` |
| `/dashboard` | Dashboard self-update status and searchable release notes | `components/update/`, self-update provider |
| `/account` | Password, TOTP/recovery codes, dashboard users, roles, and API tokens | account page, auth hook, confirmation primitives |
| `/appearance` | Light/dark/system presentation choice | appearance page and theme hook; see [`data-theming.md`](data-theming.md) |
| `/audit` | Filtered/paginated mutation audit trail | audit page and shared page/panel/state controls |
| `/backups` | Job creation/editing, target test, schedules, runs, archive contents, and restore | backups page; backend contract in [`../backend/git-backups-users.md`](../backend/git-backups-users.md#backups) |
| `/databases/*` | Connection context plus browse, query, structure, monitor, cross-table find, ER diagram, ORM generation, import/export, and mutation confirmations | `components/database/` and database layout/pages |
| `/deploy`, `/deploy/new`, `/deploy/[id]`, `/deploy/[id]/runs/[run]` | Deployment list, guided creation, configuration workspace, automation, and resumable run evidence | `components/deploy/`; contract in [`../deployments/implementation.md`](../deployments/implementation.md) |
| `/docker/*` | Overview plus containers, images, networks, volumes, compose stacks, event history, creation, diagnostics, and resource detail | `components/docker/`; see [`features-terminal.md`](features-terminal.md#feature-panels) |
| `/files` | Places/tree/grid navigation, search/quick-open, preview, Monaco editing, image editing, archives, permissions, transfers, and Git/shell handoffs | files page and `components/files/` |
| `/git` | Repository discovery, status/diff/history/graph, branch and worktree actions, GitHub auth, and pull requests | `components/git/`; backend contract in [`../backend/git-backups-users.md`](../backend/git-backups-users.md#git-working-copies) |
| `/logs` | Source rail, live/history modes, common filtering, histogram, console, retention context, and export | logs page and `components/logs/` |
| `/metrics` | Live and recorded host/container series, storage/inodes, saturation, health findings, annotations, range stack, and synchronized cursor | metrics page and `components/metrics/` |
| `/packages` | Updates/reboot state, installed/manual filters, catalogue search/install/remove, package usage, and resumable jobs | packages page, `components/packages/`, shared job console |
| `/processes` | Process ownership/inventory, signals, systemd, PM2 logs, cron, and journal detail | processes page and `components/procs/` |
| `/proxy/*` | Proxy overview, sites, streams, certificates, TLS scans, and listener/port inventory | proxy layout/pages and `components/proxy/` |
| `/security/*` | Shared posture/exposure context plus firewall, SSH, logins, connections, intrusion/fail2ban, network inventory, and bounded tools | security layout/pages and `components/security/` |
| `/system-users` | Host account inventory, create/update/delete, groups, lock state, and SSH keys | system-users page; backend contract in [`../backend/git-backups-users.md`](../backend/git-backups-users.md#host-users-and-ssh-keys) |
| `/terminal` | Direct PTY sessions, folders, windows, side tools, replay, clipboard upload, renderer, and keyboard customization | terminal page, `components/terminal/`, `components/xterm-pane.tsx`; see [`features-terminal.md`](features-terminal.md#the-terminal-panel) |

Cross-feature navigation is intentional: Files and Docker can open Git or a terminal in context; Git can
open GitHub authentication; deployment runs link to their owning Docker, proxy, backup, metrics, logs,
files, and terminal surfaces. Query parameters that trigger a one-time terminal/file context are consumed
so refresh does not repeat an action.
