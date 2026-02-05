# [p2][stream][tech-debt] no fs abstraction

labels: `p2`, `type:tech-debt`, `area:stream`

## summary
no fs abstraction

## context
- section: Audit: Stream
- source: docs/issues.md
- details:
  - uses `*os.File` directly; tests can’t stub stream sinks.

## acceptance criteria
- [ ] define minimal fix + tests

