# [p2][core][tech-debt] verify/archive use real filesystem directly

labels: `p2`, `type:tech-debt`, `area:core`

## summary
verify/archive use real filesystem directly

## context
- section: Audit: Core/Shared
- source: docs/issues.md
- details:
  - `verify/runner.go` and `archive/pipeline.go` use `os.*` for logs and paths, bypassing `fs.FS`.

## acceptance criteria
- [ ] define minimal fix + tests

