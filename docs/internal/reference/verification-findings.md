# Reverification findings

This page records implementation/documentation discrepancies found during the 2026-09-09 source audit.
They remain open engineering findings; documenting them does not relax
[`security/invariants.md`](../security/invariants.md).

## Host command authority is not uniform

Invariant 6 requires host commands to use `hostexec` with explicit argv. The normalized deployment and
proxy paths do, but the repository still contains direct `os/exec` calls in `gitx`, `ghx`, `linuxusers`,
`selfupdate`, `procs`, `dbx`, and `dockerx`, in addition to the documented stored deployment shell
exception. Many direct calls intentionally use binaries pinned in the backend image against bind-mounted
host paths or the Docker socket, and Git/GitHub apply `hostexec.AsOwner`, but they do not all pass through
`hostexec.Command*`. This boundary needs an architecture decision or implementation convergence; do not
cite the presence of this page as approval for another direct executor.

## Path containment has parallel implementations

The security contract names `files.Resolve`/`ResolveEntry` as the client-path choke point. Current Git
routes instead use `gitx.Resolve`, which independently cleans, resolves symlinks, and checks
`JD_GIT_ROOTS`. Backup restore correctly resolves its destination through `files.Resolve` before
`safepath` extraction, but backup job create/update currently persist source paths and local destination
paths without passing them through `files.Resolve`; `Runner` later walks or writes those paths directly.
This is a current-state discrepancy, not a new allowed exception. Changes at either boundary must preserve
configured roots and existing installs while converging on the invariant.

## Verified inventory drift corrected in this documentation change

- `backend/go.mod` requires Go 1.25.7, not 1.25.0.
- 26 `backend/internal` packages currently contain tests, not 22.
- `frontend/src/app` contains 48 page entry files, not 18; three deployment wrappers are server components.
- `api.moduleSet` contains ten deployment components, not two.
- The 0.6.7 delivery ledger has completed C0 through C7; C8 is next.
- The SQLite inventory now includes saved database queries/history and the expanded normalized deployment
  tables instead of describing only the earlier schema.
