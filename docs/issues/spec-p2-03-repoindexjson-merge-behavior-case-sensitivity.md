# [p2][spec][design] repo_index.json merge behavior: case sensitivity

labels: `p2`, `type:design`, `area:spec`

## summary
repo_index.json merge behavior: case sensitivity

## context
- section: Open Issues / Notes
- source: docs/issues.md
- details:
  - “Paths de-duplicated case-sensitively” is wrong on macOS default FS (case-insensitive). You’ll get duplicates with different casing. V1 fix: normalize paths via `filepath.Clean` and maybe `EvalSymlinks`. If you don’t want FS calls, state “paths de-duplicated by exact string match” and accept duplicates rather than claiming principled behavior.

## acceptance criteria
- [ ] define minimal fix + tests

