# [p2][runservice][tech-debt] direct os.* file writes ignore fs.FS

labels: `p2`, `type:tech-debt`, `area:runservice`

## summary
direct os.* file writes ignore fs.FS

## context
- section: Audit: Runservice
- source: docs/issues.md
- details:
  - setup logs and env handling are hardwired to the real filesystem, undermining testability.

## acceptance criteria
- [ ] define minimal fix + tests

