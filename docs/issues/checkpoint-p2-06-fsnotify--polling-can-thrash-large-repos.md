# [p2][checkpoint][tech-debt] fsnotify + polling can thrash large repos

labels: `p2`, `type:tech-debt`, `area:checkpoint`

## summary
fsnotify + polling can thrash large repos

## context
- section: Audit: Checkpoint
- source: docs/issues.md
- details:
  - initial `WalkDir` and directory watching are unbounded and ignore .gitignore.

## acceptance criteria
- [ ] define minimal fix + tests

