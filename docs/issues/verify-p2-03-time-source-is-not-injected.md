# [p2][verify][tech-debt] time source is not injected

labels: `p2`, `type:tech-debt`, `area:verify`

## summary
time source is not injected

## context
- section: Audit: Verify
- source: docs/issues.md
- details:
  - `Run` uses `time.Now()` directly; makes tests nondeterministic.

## acceptance criteria
- [ ] define minimal fix + tests

