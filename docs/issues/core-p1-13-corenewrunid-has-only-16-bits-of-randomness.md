# [p1][core][tech-debt] core.NewRunID has only 16 bits of randomness

labels: `p1`, `type:tech-debt`, `area:core`

## summary
core.NewRunID has only 16 bits of randomness

## context
- section: Audit: Core/Shared
- source: docs/issues.md
- details:
  - 4 hex chars is collision-prone. increase entropy and add collision checks.

## acceptance criteria
- [ ] define minimal fix + tests

