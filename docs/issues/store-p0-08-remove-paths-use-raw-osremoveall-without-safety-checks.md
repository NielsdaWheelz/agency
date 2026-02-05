# [p0][store][tech-debt] remove paths use raw os.RemoveAll without safety checks

labels: `p0`, `type:tech-debt`, `area:store`

## summary
remove paths use raw os.RemoveAll without safety checks

## context
- section: Audit: Store/FS/Exec
- source: docs/issues.md
- details:
  - `store/invocation.go` and `store/integration_worktree.go` delete directories directly; enforce `SafeRemoveAll` or explicit subpath checks.

## acceptance criteria
- [ ] define minimal fix + tests

