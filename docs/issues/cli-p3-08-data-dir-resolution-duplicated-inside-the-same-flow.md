# [p3][cli][tech-debt] data dir resolution duplicated inside the same flow

labels: `p3`, `type:tech-debt`, `area:cli`

## summary
data dir resolution duplicated inside the same flow

## context
- section: Audit: Commands
- source: docs/issues.md
- details:
  - several commands resolve `RunContext` then re-resolve `dataDir` again. choose one.

## acceptance criteria
- [ ] define minimal fix + tests

