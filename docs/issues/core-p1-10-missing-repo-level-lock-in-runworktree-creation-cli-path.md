# [p1][core][tech-debt] Missing repo-level lock in run/worktree creation (CLI path)

labels: `p1`, `type:tech-debt`, `area:core`

## summary
Missing repo-level lock in run/worktree creation (CLI path)

## context
- section: Quality Gaps (Global)
- source: docs/issues.md
- details:
  - `run`/`worktree` creation mutate git state and repo.json without repo locks, risking races against push/merge/clean.

## acceptance criteria
- [ ] define minimal fix + tests

