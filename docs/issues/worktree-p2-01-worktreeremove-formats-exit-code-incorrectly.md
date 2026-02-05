# [p2][worktree][tech-debt] worktree.Remove formats exit code incorrectly

labels: `p2`, `type:tech-debt`, `area:worktree`

## summary
worktree.Remove formats exit code incorrectly

## context
- section: Audit: Worktree
- source: docs/issues.md
- details:
  - it uses `string(rune(exitCode+'0'))`, which breaks for any multi-digit exit. use `fmt.Sprintf("%d", exitCode)`.

## acceptance criteria
- [ ] define minimal fix + tests

