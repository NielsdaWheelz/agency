# [p2][archive][tech-debt] time source is not injected

labels: `p2`, `type:tech-debt`, `area:archive`

## summary
time source is not injected

## context
- section: Audit: Archive
- source: docs/issues.md
- details:
  - archive uses `time.Now()` directly (for duration) even though a service clock exists elsewhere.

## acceptance criteria
- [ ] define minimal fix + tests

