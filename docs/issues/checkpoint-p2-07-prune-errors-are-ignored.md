# [p2][checkpoint][tech-debt] prune errors are ignored

labels: `p2`, `type:tech-debt`, `area:checkpoint`

## summary
prune errors are ignored

## context
- section: Audit: Checkpoint
- source: docs/issues.md
- details:
  - `update-ref -d` failures are dropped, leaving orphaned refs.

## acceptance criteria
- [ ] define minimal fix + tests

