# [p1][core][tech-debt] Unbounded in-memory stdout/stderr capture for external commands

labels: `p1`, `type:tech-debt`, `area:core`

## summary
Unbounded in-memory stdout/stderr capture for external commands

## context
- section: Quality Gaps (Global)
- source: docs/issues.md
- details:
  - `exec.Run` / `RunScript` buffer all output in memory. Large git/gh output can blow memory. Stream to file or cap buffers.

## acceptance criteria
- [ ] define minimal fix + tests

