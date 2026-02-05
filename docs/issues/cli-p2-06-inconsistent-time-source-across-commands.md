# [p2][cli][tech-debt] inconsistent time source across commands

labels: `p2`, `type:tech-debt`, `area:cli`

## summary
inconsistent time source across commands

## context
- section: Audit: Commands
- source: docs/issues.md
- details:
  - `time.Now()` sprinkled everywhere for events and timestamps; no injected clock; no deterministic tests.

## acceptance criteria
- [ ] define minimal fix + tests

