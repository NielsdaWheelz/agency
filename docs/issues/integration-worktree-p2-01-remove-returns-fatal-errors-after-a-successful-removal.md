# [p2][integration-worktree][tech-debt] Remove returns fatal errors after a successful removal

labels: `p2`, `type:tech-debt`, `area:integration-worktree`

## summary
Remove returns fatal errors after a successful removal

## context
- section: Audit: Integration Worktree
- source: docs/issues.md
- details:
  - if meta update fails, you report failure even though the worktree is gone; decide on rollback or downgrade to warning + repair.

## acceptance criteria
- [ ] define minimal fix + tests

