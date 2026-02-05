# [p1][daemon][tech-debt] pidfile read assumes trailing newline

labels: `p1`, `type:tech-debt`, `area:daemon`

## summary
pidfile read assumes trailing newline

## context
- section: Audit: Daemon
- source: docs/issues.md
- details:
  - `ReadPidFile` slices `data[:len-1]`; empty or newline-less files can panic or parse garbage.

## acceptance criteria
- [ ] define minimal fix + tests

