# [p2][store][tech-debt] fs.FS completeness and consistent atomic writes in store

labels: `p2`, `type:tech-debt`, `area:store`

## summary
fs.FS completeness and consistent atomic writes in store

## context
- section: merged
- source: docs/issues.md (merged)
- merged items:
  - fs.FS is incomplete, forcing os.* calls in store and elsewhere
  - store mixes fs.FS and direct os.* calls
  - atomic write helpers are split and inconsistent
- details:
  - missing `ReadDir`, `RemoveAll`, `OpenFile`, `CreateTemp` helpers for common flows.
  -
  - scanning and directory creation bypass injected FS, breaking test isolation.
  -
  - `WriteFileAtomic` uses `fs.FS` while `WriteJSONAtomic` uses `os.*`. pick one and use it everywhere.
  -

## acceptance criteria
- [ ] define minimal fix + tests

