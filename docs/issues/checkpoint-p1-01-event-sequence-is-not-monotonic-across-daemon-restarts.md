# [p1][checkpoint][tech-debt] event sequence is not monotonic across daemon restarts

labels: `p1`, `type:tech-debt`, `area:checkpoint`

## summary
event sequence is not monotonic across daemon restarts

## context
- section: Audit: Checkpoint
- source: docs/issues.md
- details:
  - `eventSeq` is in-memory; after restart, seq resets to 0 while old events remain.

## acceptance criteria
- [ ] define minimal fix + tests

