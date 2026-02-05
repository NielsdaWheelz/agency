# [p1][verifyservice][tech-debt] augmentRecordError is best-effort and uses raw os.*

labels: `p1`, `type:tech-debt`, `area:verifyservice`

## summary
augmentRecordError is best-effort and uses raw os.*

## context
- section: Audit: Verifyservice
- source: docs/issues.md
- details:
  - if events are required, this should be hard-fail and use `fs.FS`.

## acceptance criteria
- [ ] define minimal fix + tests

