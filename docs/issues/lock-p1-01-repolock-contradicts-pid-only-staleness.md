# [p1][lock][tech-debt] RepoLock contradicts pid-only staleness

labels: `p1`, `type:tech-debt`, `area:lock`

## summary
RepoLock contradicts pid-only staleness

## context
- section: Audit: Lock
- source: docs/issues.md
- details:
  - it steals locks by age when the lock file is unreadable, violating the spec and risking concurrent writers.

## acceptance criteria
- [ ] define minimal fix + tests

