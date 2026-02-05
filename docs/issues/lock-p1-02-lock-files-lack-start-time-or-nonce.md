# [p1][lock][tech-debt] lock files lack start-time or nonce

labels: `p1`, `type:tech-debt`, `area:lock`

## summary
lock files lack start-time or nonce

## context
- section: Audit: Lock
- source: docs/issues.md
- details:
  - pid reuse can make stale locks look alive forever; store a start timestamp and verify.

## acceptance criteria
- [ ] define minimal fix + tests

