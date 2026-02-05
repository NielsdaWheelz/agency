# [p2][invocation][tech-debt] Create assumes IntegrationWorktreeMeta is non-nil

labels: `p2`, `type:tech-debt`, `area:invocation`

## summary
Create assumes IntegrationWorktreeMeta is non-nil

## context
- section: Audit: Invocation
- source: docs/issues.md
- details:
  - it will panic if the caller passes nil; validate inputs and return `E_INTERNAL` or `E_WORKTREE_NOT_FOUND`.

## acceptance criteria
- [ ] define minimal fix + tests

