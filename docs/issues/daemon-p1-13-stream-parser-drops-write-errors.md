# [p1][daemon][tech-debt] stream parser drops write errors

labels: `p1`, `type:tech-debt`, `area:daemon`

## summary
stream parser drops write errors

## context
- section: Audit: Daemon
- source: docs/issues.md
- details:
  - `daemon/stream/parser.go` ignores failures writing to raw/stream files; if these are contractually required, errors must be surfaced.

## acceptance criteria
- [ ] define minimal fix + tests

