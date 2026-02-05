# [p3][watchdog][tech-debt] time source is implicit

labels: `p3`, `type:tech-debt`, `area:watchdog`

## summary
time source is implicit

## context
- section: Audit: Watchdog
- source: docs/issues.md
- details:
  - `watchdog.CheckStall` uses `time.Since` internally; tests are nondeterministic and clock jumps can skew results. inject `now` or a clock interface.

## acceptance criteria
- [ ] define minimal fix + tests

