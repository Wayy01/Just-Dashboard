# ADR 0002: Canonical path containment and command execution

- Status: accepted
- Date: 2026-09-02
- Release: 0.6.7

## Context

`deploy.Project.Validate` currently carries a second root-prefix implementation while invariant 6 names
`files.Resolve` as the authority. The deployer also constructs Git, Docker, and shell commands directly.
That happens to work when the dashboard image contains the binary and the host path is mounted at the same
name, but the fallback-to-host behavior loses the working directory when `nsenter` changes namespaces.

0.6.7 adds Git mirrors/worktrees, uploads, archives, Compose files, build contexts, bind mounts, imports,
and generated release directories. Leaving the boundary implicit would multiply both discrepancies.

## Decision

### Paths

The deployment module receives a dedicated `files.Service` built from `JD_DEPLOY_ROOTS`. Every
client-selected existing path and every client-selected destination is resolved by that service. The
legacy `withinRoots` implementation is removed when its compatibility callers move to the resolver.

- `Resolve` is used for checkouts, Compose files, build contexts, bind sources, import roots, and paths the
  operation reads or writes through.
- `ResolveEntry` is used only when the operation acts on the entry itself, such as deleting a temporary
  worktree symlink.
- Uploaded archives are first placed in a dashboard-owned staging directory and unpacked with `safepath`
  into a resolver-approved destination. Archive member names never become host paths directly.
- Dashboard-owned internal paths are derived from `JD_DATA_DIR` plus database ids/random tokens; callers
  never supply their absolute value. They still use safe archive handling and restrictive modes.
- Compose-relative files are cleaned, must remain relative, and are joined only below an already-resolved
  source root. A later resolution check catches symlink escapes.

An empty `JD_DEPLOY_ROOTS` value is rejected by configuration rather than interpreted as `/`. The shipped
default remains `/opt,/srv,/home,/root`, so existing installs retain their effective access.

### Commands

`hostexec` gains `CommandInDir(ctx, dir, name, args...)`: use the image's pinned binary with `cmd.Dir` when
present; otherwise enter the host namespaces with `--wd=<dir>`. Deployment code uses it for Git, Docker,
Compose, and builder commands. Arguments are never joined into a shell string.

The stored operator-command runner uses the image's `/bin/sh -c` in a resolver-approved working directory.
It accepts only the immutable command string copied from an admin-authored plan revision. It receives an
explicit scoped environment assembled by the deployment module.

Owner dropping is applied after the working directory is set. Process-group setup and owner credentials
are merged into one `SysProcAttr`; neither helper may overwrite the other. Cancellation sends TERM to the
group, waits the step's cleanup grace, then sends KILL. The runner records both signals and the final exit.

### Environment

Every child starts from the dashboard environment with all `JD_*` and `VPSD_*` names removed. Each adapter
adds only its declared variables:

- Git: non-interactive prompt/host-key policy and the selected credential mechanism;
- build: variables scoped to `build`, using BuildKit secret mounts where the recipe supports them;
- release task: variables scoped to `release-task`;
- runtime: passed to Docker/Compose configuration, not inherited by build or host commands;
- Docker/Compose: progress and connection settings required by the adapter.

Secrets are forbidden in argv. Temporary credential/config files are mode `0600`, live in a
dashboard-owned directory, and are removed after the command.

## Command boundary matrix

| Operation | Namespace/binary | Working directory | Owner |
| --- | --- | --- | --- |
| Git mirror/worktree/local inspection | `hostexec.CommandInDir` | resolved mirror/worktree/checkout | path owner for operator-owned checkout; dashboard owner for managed cache |
| Docker build/pull/inspect/Compose | `hostexec.CommandInDir` | resolved build/Compose root | dashboard process; Docker daemon owns runtime effects |
| Stored release task | local `/bin/sh -c` | immutable resolved worktree | worktree owner |
| HTTP/TCP checks | in-process Go | none | dashboard process |
| Proxy/backups/firewall | owning feature service | decided by that service | decided by that service |
| Blueprint operations | closed adapter argv or feature call | generated managed root | dashboard process |

## Consequences

- Deployment and Files share one symlink-aware containment authority.
- Host fallback no longer silently starts commands in `/`.
- The one allowed shell remains visible, reviewable, stored, and admin-authored.
- Existing project paths continue to work, but a stored path that now resolves outside its configured root
  becomes an honest migration finding rather than an implicit exception.

