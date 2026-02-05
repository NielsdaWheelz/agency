# [p1][stream][tech-debt] normalized event seq is not persisted

labels: `p1`, `type:tech-debt`, `area:stream`

## summary
normalized event seq is not persisted

## context
- section: Audit: Stream
- source: docs/issues.md
- details:
  - `seq` resets each daemon run; ordering across restarts is not stable.

## acceptance criteria
- [ ] define minimal fix + tests

