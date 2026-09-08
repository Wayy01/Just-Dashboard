# C7 review — automation and previews

Status: passed on 2026-09-08.

## Delivered

- GitHub, GitLab, Bitbucket and Gitea raw-body verification with closed event vocabularies and repository,
  ref, delivery, size and replay fences. Legacy HMAC, scoped generic hooks and authenticated API
  idempotency remain separate entry contracts.
- Webhook-only watch rules plus an admin simulator. Rejected and duplicate deliveries cannot enqueue.
- Five-field IANA-timezone schedules with DST-safe next-run calculation, atomic claiming, bounded ordered
  deploy/restart/backup/container-command chains, honest unavailable game-console actions, and bounded run
  history. Create/update/delete/test routes follow the frozen admin/session/audit matrix.
- Quota-bound PR previews whose source is pinned to the provider revision. Updates create new immutable
  source/build/runtime revisions; sealed variables and linked/observed dependencies are inherited, while
  production-managed resources are not. Close removes the exact preview runtime and route, then archives.
- Signed outbound JSON notifications with sealed headers/secrets, one-time secret creation, delivery
  history containing only response classes, and failure isolation from normal deployment outcomes.
- An Automation workspace using the established deployment UI patterns for trigger secrets, schedules,
  preview state, notification policies and delivery configuration.

## Gate evidence

- `go build ./...` and `go vet ./...` passed.
- Race tests passed for `internal/deploy`, `internal/proxysvc`, `internal/store`, and the C7 signed-in API
  route/security matrix.
- Provider rejection/replay, generic/API idempotency, queue supersession protection, preview isolation,
  cron timezone/DST, notification signing/redaction/failure, and required chain-stop tests passed.
- `bunx tsc --noEmit`, `bun run lint`, and `bun run build` passed.
- All 12 browser journeys passed against the current production frontend build, including the complete
  automation workspace journey.

The repository-wide backend run passed every non-terminal package. The existing host-dependent terminal
fixtures still fail on this environment because the tmux server/clipboard directory is owned outside the
test process; C7 does not change terminal code, and the focused API and deployment gates pass under race.

## Boundary

C8 operational diagnosis and C9 game-console execution remain intentionally unavailable. A scheduled
game command records `game_console_unavailable`; it is not simulated as success.
