# [p1][cli][tech-debt] critical file writes ignore errors

labels: `p1`, `type:tech-debt`, `area:cli`

## summary
critical file writes ignore errors

## context
- section: Audit: Commands
- source: docs/issues.md
- details:
  - e.g. `merge.go` writes the merge log with `_ = os.WriteFile(...)`. if logs are part of the contract, errors must be handled.

## acceptance criteria
- [ ] define minimal fix + tests

