# [p1][daemon][tech-debt] headless runner env lacks required defaults

labels: `p1`, `type:tech-debt`, `area:daemon`

## summary
headless runner env lacks required defaults

## context
- section: Audit: Daemon
- source: docs/issues.md
- details:
  - no `CI=1`, `GIT_TERMINAL_PROMPT=0`, or `AGENCY_*` values; headless runners can block or run without context.

## acceptance criteria
- [ ] define minimal fix + tests

