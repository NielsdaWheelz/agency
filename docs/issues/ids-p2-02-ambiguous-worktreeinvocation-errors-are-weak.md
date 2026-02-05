# [p2][ids][tech-debt] ambiguous worktree/invocation errors are weak

labels: `p2`, `type:tech-debt`, `area:ids`

## summary
ambiguous worktree/invocation errors are weak

## context
- section: Audit: IDs
- source: docs/issues.md
- details:
  - `ErrWorktreeAmbiguous` and `ErrInvocationAmbiguous` don’t include repo_id; collisions across repos are ambiguous and unhelpful to humans.

## acceptance criteria
- [ ] define minimal fix + tests

