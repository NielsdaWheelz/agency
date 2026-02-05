# [p2][checkpoint][tech-debt] temp index location is unscoped

labels: `p2`, `type:tech-debt`, `area:checkpoint`

## summary
temp index location is unscoped

## context
- section: Audit: Checkpoint
- source: docs/issues.md
- details:
  - `os.CreateTemp("", ...)` writes to global temp; should be sandbox-scoped for safety and predictability.

## acceptance criteria
- [ ] define minimal fix + tests

