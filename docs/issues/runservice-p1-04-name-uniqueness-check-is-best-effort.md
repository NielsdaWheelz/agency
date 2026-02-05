# [p1][runservice][tech-debt] name uniqueness check is best-effort

labels: `p1`, `type:tech-debt`, `area:runservice`

## summary
name uniqueness check is best-effort

## context
- section: Audit: Runservice
- source: docs/issues.md
- details:
  - `checkNameUnique` ignores scan errors and can allow duplicate active names; should fail hard.

## acceptance criteria
- [ ] define minimal fix + tests

