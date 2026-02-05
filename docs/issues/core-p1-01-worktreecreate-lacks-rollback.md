# [p1][core][tech-debt] worktree.Create lacks rollback

labels: `p1`, `type:tech-debt`, `area:core`

## summary
worktree.Create lacks rollback

## context
- section: Audit: Core/Shared
- source: docs/issues.md
- details:
  - if scaffolding fails after `git worktree add`, the branch/worktree is left behind.

## acceptance criteria
- [ ] define minimal fix + tests

