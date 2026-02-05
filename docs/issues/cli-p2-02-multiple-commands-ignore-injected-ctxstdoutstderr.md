# [p2][cli][tech-debt] multiple commands ignore injected ctx/stdout/stderr

labels: `p2`, `type:tech-debt`, `area:cli`

## summary
multiple commands ignore injected ctx/stdout/stderr

## context
- section: Audit: Commands
- source: docs/issues.md
- details:
  - `open.go` and `path.go` accept parameters and then ignore them. That’s dishonest APIs and makes testing misleading.

## acceptance criteria
- [ ] define minimal fix + tests

