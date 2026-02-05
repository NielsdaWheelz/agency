# [p2][core][tech-debt] Config resolution scattered across leaf code

labels: `p2`, `type:tech-debt`, `area:core`

## summary
Config resolution scattered across leaf code

## context
- section: Quality Gaps (Global)
- source: docs/issues.md
- details:
  - Multiple commands resolve `paths.ResolveDirs` internally, which undermines the “resolve once at boundary” standard. Use injected `RuntimeDirs`/`CommandContext`.

## acceptance criteria
- [ ] define minimal fix + tests

