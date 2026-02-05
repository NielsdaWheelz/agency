# [p1][merge][tech-debt] buildVerifyEnvForMerge sets AGENCY_REPO_ROOT to worktree path

labels: `p1`, `type:tech-debt`, `area:merge`

## summary
buildVerifyEnvForMerge sets AGENCY_REPO_ROOT to worktree path

## context
- section: Audit: Merge
- source: docs/issues.md
- details:
  - likely wrong; should be actual repo root.

## acceptance criteria
- [ ] define minimal fix + tests

