# [p2][runservice][tech-debt] time source is not injected consistently

labels: `p2`, `type:tech-debt`, `area:runservice`

## summary
time source is not injected consistently

## context
- section: Audit: Runservice
- source: docs/issues.md
- details:
  - `executeSetupScript` uses `time.Now()` directly instead of the service clock.

## acceptance criteria
- [ ] define minimal fix + tests

