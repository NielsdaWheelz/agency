# [p3][cli][tech-debt] resolve.go creates a store.NewStore and then discards it

labels: `p3`, `type:tech-debt`, `area:cli`

## summary
resolve.go creates a store.NewStore and then discards it

## context
- section: Audit: Commands
- source: docs/issues.md
- details:
  - dead code; remove it or use it.

## acceptance criteria
- [ ] define minimal fix + tests

