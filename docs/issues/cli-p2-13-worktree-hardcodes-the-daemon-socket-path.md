# [p2][cli][tech-debt] worktree hardcodes the daemon socket path

labels: `p2`, `type:tech-debt`, `area:cli`

## summary
worktree hardcodes the daemon socket path

## context
- section: Audit: Commands
- source: docs/issues.md
- details:
  - it uses `dirs.DataDir + "/agencyd.sock"` instead of `store.DaemonSocketPath`, and ignores symlink normalization.

## acceptance criteria
- [ ] define minimal fix + tests

