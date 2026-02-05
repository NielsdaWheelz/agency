# [p1][cli][tech-debt] all commands drop cmd.Context()

labels: `p1`, `type:tech-debt`, `area:cli`

## summary
all commands drop cmd.Context()

## context
- section: Audit: CLI (cobra)
- source: docs/issues.md
- details:
  - every cobra handler uses `context.Background()`; cancellation and deadlines never propagate.

## acceptance criteria
- [ ] define minimal fix + tests

