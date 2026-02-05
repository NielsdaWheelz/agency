# [p1][store][tech-debt] meta updates are non-atomic under concurrency

labels: `p1`, `type:tech-debt`, `area:store`

## summary
meta updates are non-atomic under concurrency

## context
- section: Audit: Store/FS/Exec
- source: docs/issues.md
- details:
  - `UpdateMeta`/`UpdateInvocationMeta` do read-modify-write without file locks; concurrent updates can clobber.

## acceptance criteria
- [ ] define minimal fix + tests

