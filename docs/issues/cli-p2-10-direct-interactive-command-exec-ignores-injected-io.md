# [p2][cli][tech-debt] direct interactive command exec ignores injected I/O

labels: `p2`, `type:tech-debt`, `area:cli`

## summary
direct interactive command exec ignores injected I/O

## context
- section: Audit: Commands
- source: docs/issues.md
- details:
  - `open`, `attach`, and `worktree` force `os.Stdin/Stdout/Stderr` even when writers are passed. decide on a single I/O strategy and remove fake parameters.

## acceptance criteria
- [ ] define minimal fix + tests

