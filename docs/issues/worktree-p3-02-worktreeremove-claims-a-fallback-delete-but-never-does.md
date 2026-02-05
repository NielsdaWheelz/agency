# [p3][worktree][tech-debt] worktree.Remove claims a fallback delete but never does it

labels: `p3`, `type:tech-debt`, `area:worktree`

## summary
worktree.Remove claims a fallback delete but never does it

## context
- section: Audit: Worktree
- source: docs/issues.md
- details:
  - `FallbackUsed` is dead and no `rm -rf` fallback exists. implement or remove the field and docs.

## acceptance criteria
- [ ] define minimal fix + tests

