# [p2][core][tech-debt] fs abstraction is leaky and inconsistent

labels: `p2`, `type:tech-debt`, `area:core`

## summary
fs abstraction is leaky and inconsistent

## context
- section: Quality Gaps (Global)
- source: docs/issues.md
- details:
  - Many store operations call `os.*` directly, ignoring `fs.FS`. That makes stubbing incomplete and portability weaker.

## acceptance criteria
- [ ] define minimal fix + tests

