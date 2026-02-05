# [p2][landing][tech-debt] landing uses raw os.* for file ops and patch perms are too open

labels: `p2`, `type:tech-debt`, `area:landing`

## summary
landing uses raw os.* for file ops and patch perms are too open

## context
- section: merged
- source: docs/issues.md (merged)
- merged items:
  - landing uses direct os.* and writes patch files with 0644
  - uses raw os.* for file ops
  - patch files use 0644
- details:
  - bypasses `fs.FS` and violates permission policy.
  -
  - `os.Stat`, `os.RemoveAll`, `os.WriteFile` bypass `fs.FS` and safety checks.
  -
  - landing writes patch artifacts world-readable.
  -

## acceptance criteria
- [ ] define minimal fix + tests

