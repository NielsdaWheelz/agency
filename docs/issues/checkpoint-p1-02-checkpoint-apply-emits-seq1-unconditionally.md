# [p1][checkpoint][tech-debt] checkpoint apply emits Seq=1 unconditionally

labels: `p1`, `type:tech-debt`, `area:checkpoint`

## summary
checkpoint apply emits Seq=1 unconditionally

## context
- section: Audit: Checkpoint
- source: docs/issues.md
- details:
  - breaks monotonic ordering within a single events.jsonl.

## acceptance criteria
- [ ] define minimal fix + tests

