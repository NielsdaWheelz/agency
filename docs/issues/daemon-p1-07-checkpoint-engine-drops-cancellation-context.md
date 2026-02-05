# [p1][daemon][tech-debt] checkpoint engine drops cancellation context

labels: `p1`, `type:tech-debt`, `area:daemon`

## summary
checkpoint engine drops cancellation context

## context
- section: Audit: Daemon
- source: docs/issues.md
- details:
  - `doFinalCheckpoint(context.Background())` ignores caller cancellation; use the passed ctx or a derived one.

## acceptance criteria
- [ ] define minimal fix + tests

