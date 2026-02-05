# [p2][archive][tech-debt] filesystem abstraction is ignored

labels: `p2`, `type:tech-debt`, `area:archive`

## summary
filesystem abstraction is ignored

## context
- section: Audit: Archive
- source: docs/issues.md
- details:
  - `Archive` and `runArchiveScript` use `os.*` instead of `fs.FS`.

## acceptance criteria
- [ ] define minimal fix + tests

