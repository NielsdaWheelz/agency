# [p1][core][tech-debt] CLI ignores cancellation and timeouts at the boundary

labels: `p1`, `type:tech-debt`, `area:core`

## summary
CLI ignores cancellation and timeouts at the boundary

## context
- section: Quality Gaps (Global)
- source: docs/issues.md
- details:
  - Cobra commands use `context.Background()` instead of `cmd.Context()`, so Ctrl+C and deadlines don’t propagate. Fix by using `cmd.Context()` everywhere and enforcing timeouts for external commands (git/gh/tmux), not just scripts.

## acceptance criteria
- [ ] define minimal fix + tests

