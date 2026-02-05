# [p1][core][tech-debt] verify record writes are best-effort in error paths

labels: `p1`, `type:tech-debt`, `area:core`

## summary
verify record writes are best-effort in error paths

## context
- section: Audit: Core/Shared
- source: docs/issues.md
- details:
  - `verify.writeRecordBestEffort` drops errors. if verify records are required, this must fail hard.

## acceptance criteria
- [ ] define minimal fix + tests

