# [p1][render][tech-debt] conflict action cards are not shell-safe

labels: `p1`, `type:tech-debt`, `area:render`

## summary
conflict action cards are not shell-safe

## context
- section: Audit: Render
- source: docs/issues.md
- details:
  - `render.WriteConflictCard` prints commands with raw `ref` and `worktreePath`. spaces/quotes break copy-paste and enable injection. use `core.ShellEscapePosix` for arguments.

## acceptance criteria
- [ ] define minimal fix + tests

