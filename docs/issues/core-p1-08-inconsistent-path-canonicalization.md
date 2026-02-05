# [p1][core][tech-debt] Inconsistent path canonicalization

labels: `p1`, `type:tech-debt`, `area:core`

## summary
Inconsistent path canonicalization

## context
- section: Quality Gaps (Global)
- source: docs/issues.md
- details:
  - Daemon start `EvalSymlinks` for data dir; other commands don’t. This can break equality checks and recursion guards.

## acceptance criteria
- [ ] define minimal fix + tests

