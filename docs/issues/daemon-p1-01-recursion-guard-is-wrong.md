# [p1][daemon][tech-debt] recursion guard is wrong

labels: `p1`, `type:tech-debt`, `area:daemon`

## summary
recursion guard is wrong

## context
- section: Audit: Daemon
- source: docs/issues.md
- details:
  - `isInsideAgencyManagedWorktree` ignores `worktrees` and relies on `HasPrefix`, which can misclassify. Fix path containment and include `worktrees`.

## acceptance criteria
- [ ] define minimal fix + tests

