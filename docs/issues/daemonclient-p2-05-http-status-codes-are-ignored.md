# [p2][daemonclient][tech-debt] HTTP status codes are ignored

labels: `p2`, `type:tech-debt`, `area:daemonclient`

## summary
HTTP status codes are ignored

## context
- section: Audit: Daemonclient
- source: docs/issues.md
- details:
  - responses are decoded as JSON regardless of status; non-2xx bodies produce misleading decode errors.

## acceptance criteria
- [ ] define minimal fix + tests

