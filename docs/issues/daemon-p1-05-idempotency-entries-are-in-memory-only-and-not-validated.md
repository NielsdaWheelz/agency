# [p1][daemon][tech-debt] idempotency entries are in-memory only and not validated

labels: `p1`, `type:tech-debt`, `area:daemon`

## summary
idempotency entries are in-memory only and not validated

## context
- section: Audit: Daemon
- source: docs/issues.md
- details:
  - duplicate responses can return stale `worktree_id`/paths after deletions or daemon restarts.

## acceptance criteria
- [ ] define minimal fix + tests

