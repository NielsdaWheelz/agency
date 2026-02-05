# [p3][errors][tech-debt] error printing ignores write failures

labels: `p3`, `type:tech-debt`, `area:errors`

## summary
error printing ignores write failures

## context
- section: Audit: Errors
- source: docs/issues.md
- details:
  - `PrintWithOptions` drops `io.WriteString` errors; broken pipe exits 0 and hides real failure. propagate or surface.

## acceptance criteria
- [ ] define minimal fix + tests

