# [p1][cli][tech-debt] unbounded prompt file reads

labels: `p1`, `type:tech-debt`, `area:cli`

## summary
unbounded prompt file reads

## context
- section: Audit: Commands
- source: docs/issues.md
- details:
  - `agent` reads `--prompt-file` via `os.ReadFile` with no size limit; can blow memory.

## acceptance criteria
- [ ] define minimal fix + tests

