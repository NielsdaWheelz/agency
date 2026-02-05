# [p2][verify][tech-debt] signal recording is inaccurate on timeout/cancel

labels: `p2`, `type:tech-debt`, `area:verify`

## summary
signal recording is inaccurate on timeout/cancel

## context
- section: Audit: Verify
- source: docs/issues.md
- details:
  - it unconditionally sets `SIGKILL` even if the process exited for another reason.

## acceptance criteria
- [ ] define minimal fix + tests

