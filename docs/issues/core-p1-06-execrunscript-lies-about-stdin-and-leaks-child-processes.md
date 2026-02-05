# [p1][core][tech-debt] exec.RunScript lies about stdin and leaks child processes

labels: `p1`, `type:tech-debt`, `area:core`

## summary
exec.RunScript lies about stdin and leaks child processes

## context
- section: Audit: Core/Shared
- source: docs/issues.md
- details:
  - it claims stdin is `/dev/null` but `cmd.Stdin = nil` inherits parent stdin. also no process group kill on timeout; children can survive.

## acceptance criteria
- [ ] define minimal fix + tests

