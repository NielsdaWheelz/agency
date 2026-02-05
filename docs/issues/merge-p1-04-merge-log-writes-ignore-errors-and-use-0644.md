# [p1][merge][tech-debt] merge log writes ignore errors and use 0644

labels: `p1`, `type:tech-debt`, `area:merge`

## summary
merge log writes ignore errors and use 0644

## context
- section: Audit: Merge
- source: docs/issues.md
- details:
  - `executeGHMerge` drops write errors; log file should be 0600.

## acceptance criteria
- [ ] define minimal fix + tests

