# [p1][core][tech-debt] Lock staleness is pid-only

labels: `p1`, `type:tech-debt`, `area:core`

## summary
Lock staleness is pid-only

## context
- section: Quality Gaps (Global)
- source: docs/issues.md
- details:
  - PID reuse can make stale locks look alive and block operations forever. Add start-time verification or lock TTL with explicit override.

## acceptance criteria
- [ ] define minimal fix + tests

