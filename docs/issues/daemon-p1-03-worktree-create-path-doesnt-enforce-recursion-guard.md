# [p1][daemon][tech-debt] worktree create path doesn’t enforce recursion guard

labels: `p1`, `type:tech-debt`, `area:daemon`

## summary
worktree create path doesn’t enforce recursion guard

## context
- section: Audit: Daemon
- source: docs/issues.md
- details:
  - `handleWorktreeCreate` never calls `isInsideAgencyManagedWorktree`, so it can accept a repo root inside managed trees.

## acceptance criteria
- [ ] define minimal fix + tests

