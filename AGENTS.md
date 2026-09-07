# Codex project guidance

Claude Code did most of the existing work on this project. Preserve that context rather than
re-deriving or replacing its conventions.

- Read and follow `CLAUDE.md` before making project changes. It is the canonical architecture,
  workflow, security-invariant, testing, and style guide for this repository.
- Treat the rationale and invariants in `CLAUDE.md` as current project requirements. If code and
  documentation disagree, investigate the history and call out the discrepancy instead of silently
  choosing one.
- Reuse compatible project-local Claude Code skills, commands, agents, hooks, and supporting files
  when they are added to the repository. Read their instructions before use and adapt tool-specific
  syntax where Codex and Claude Code differ.
- The global Claude Code installation has the official `frontend-design` plugin installed and
  enabled. Its skill is at
  `/home/ubuntu/.claude/plugins/cache/claude-plugins-official/frontend-design/unknown/skills/frontend-design/SKILL.md`.
  Read it when doing visual UI design or substantial interface reshaping, alongside any applicable
  Codex UI/UX skill; preserve this project's established design system when their generic advice
  conflicts with `CLAUDE.md` or the existing interface.
- Other entries under `/home/ubuntu/.claude/plugins/marketplaces/` are marketplace source material,
  not necessarily installed or enabled. Do not assume they were part of Claude's project workflow.
- Do not read, copy, print, or modify Claude credential/session/history files. They are not project
  context.
- Keep `CLAUDE.md` accurate when a change alters an architectural decision or documented invariant.
