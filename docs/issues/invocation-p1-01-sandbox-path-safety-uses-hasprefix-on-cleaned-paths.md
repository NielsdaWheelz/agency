# [p1][invocation][tech-debt] sandbox path safety uses HasPrefix on cleaned paths

labels: `p1`, `type:tech-debt`, `area:invocation`

## summary
sandbox path safety uses HasPrefix on cleaned paths

## context
- section: Audit: Invocation
- source: docs/issues.md
- details:
  - same bug as recursion guard: prefix checks can misclassify. use `filepath.Rel` and path boundary checks.

## acceptance criteria
- [ ] define minimal fix + tests

