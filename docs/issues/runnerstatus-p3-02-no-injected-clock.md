# [p3][runnerstatus][tech-debt] no injected clock

labels: `p3`, `type:tech-debt`, `area:runnerstatus`

## summary
no injected clock

## context
- section: Audit: Runnerstatus
- source: docs/issues.md
- details:
  - `NewInitial` and `Age` use `time.Now()` directly, making tests nondeterministic.

## acceptance criteria
- [ ] define minimal fix + tests

