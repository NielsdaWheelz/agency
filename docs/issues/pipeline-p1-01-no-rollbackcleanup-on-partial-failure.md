# [p1][pipeline][tech-debt] no rollback/cleanup on partial failure

labels: `p1`, `type:tech-debt`, `area:pipeline`

## summary
no rollback/cleanup on partial failure

## context
- section: Audit: Pipeline
- source: docs/issues.md
- details:
  - if a mid-step fails, worktrees, run dirs, or tmux sessions can be left behind. add compensating actions or a cleanup step.

## acceptance criteria
- [ ] define minimal fix + tests

