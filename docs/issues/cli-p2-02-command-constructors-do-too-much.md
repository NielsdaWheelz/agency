# [p2][cli][tech-debt] command constructors do too much

labels: `p2`, `type:tech-debt`, `area:cli`

## summary
command constructors do too much

## context
- section: Audit: CLI (cobra)
- source: docs/issues.md
- details:
  - they assemble deps and resolve cwd inside handlers; move this to a shared command context/bootstrap.

## acceptance criteria
- [ ] define minimal fix + tests

