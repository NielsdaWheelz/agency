# [p1][daemon][tech-debt] headless runner inherits stdin

labels: `p1`, `type:tech-debt`, `area:daemon`

## summary
headless runner inherits stdin

## context
- section: Audit: Daemon
- source: docs/issues.md
- details:
  - daemon spawns runners without `cmd.Stdin = nil` or `/dev/null`, so a headless runner can hang on stdin.

## acceptance criteria
- [ ] define minimal fix + tests

