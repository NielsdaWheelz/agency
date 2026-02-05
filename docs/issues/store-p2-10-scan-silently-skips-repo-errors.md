# [p2][store][tech-debt] scan silently skips repo errors

labels: `p2`, `type:tech-debt`, `area:store`

## summary
scan silently skips repo errors

## context
- section: Audit: Store/FS/Exec
- source: docs/issues.md
- details:
  - `store.ScanAllRuns` ignores per-repo scan errors; this hides corruption and makes failures non-obvious.

## acceptance criteria
- [ ] define minimal fix + tests

