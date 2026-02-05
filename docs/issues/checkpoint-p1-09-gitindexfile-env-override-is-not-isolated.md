# [p1][checkpoint][tech-debt] GIT_INDEX_FILE env override is not isolated

labels: `p1`, `type:tech-debt`, `area:checkpoint`

## summary
GIT_INDEX_FILE env override is not isolated

## context
- section: Audit: Checkpoint
- source: docs/issues.md
- details:
  - staged changes can race with other git operations without repo-level locking.

## acceptance criteria
- [ ] define minimal fix + tests

