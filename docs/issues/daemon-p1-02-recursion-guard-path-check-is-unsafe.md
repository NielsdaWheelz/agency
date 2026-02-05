# [p1][daemon][tech-debt] recursion guard path check is unsafe

labels: `p1`, `type:tech-debt`, `area:daemon`

## summary
recursion guard path check is unsafe

## context
- section: Audit: Daemon
- source: docs/issues.md
- details:
  - `isInsideAgencyManagedWorktree` uses `strings.HasPrefix(cleanPath, reposDir)` without a path boundary check. `/data/reposX/...` is treated as inside `/data/repos`. use `filepath.Rel` or `fs.IsSubpath`.

## acceptance criteria
- [ ] define minimal fix + tests

