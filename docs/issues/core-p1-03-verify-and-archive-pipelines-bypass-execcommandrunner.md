# [p1][core][tech-debt] verify and archive pipelines bypass exec.CommandRunner

labels: `p1`, `type:tech-debt`, `area:core`

## summary
verify and archive pipelines bypass exec.CommandRunner

## context
- section: Audit: Core/Shared
- source: docs/issues.md
- details:
  - they use `os/exec` directly and duplicate timeout/cleanup logic, diverging from `internal/exec` conventions.

## acceptance criteria
- [ ] define minimal fix + tests

