# [p2][cli][tech-debt] PR fallback generation is unbounded

labels: `p2`, `type:tech-debt`, `area:cli`

## summary
PR fallback generation is unbounded

## context
- section: Audit: Commands
- source: docs/issues.md
- details:
  - `writeFallbackPRBody` calls `git log` and `git diff --name-only` without limits, then discards most lines.

## acceptance criteria
- [ ] define minimal fix + tests

