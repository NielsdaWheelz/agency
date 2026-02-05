# [p2][core][bug] Abstraction leaks: direct os.* calls bypass fs.FS

labels: `p2`, `type:bug`, `area:core`

## summary
Abstraction leaks: direct os.* calls bypass fs.FS

## context
- section: Code Smells / Bugs
- source: docs/issues.md
- details:
  - Some command paths bypass `fs.FS` and environment abstraction, reducing testability and portability.

## acceptance criteria
- [ ] define minimal fix + tests

