# [p2][cli][tech-debt] duplicate repo path validation logic

labels: `p2`, `type:tech-debt`, `area:cli`

## summary
duplicate repo path validation logic

## context
- section: Audit: Commands
- source: docs/issues.md
- details:
  - `run.go` validates `--repo` manually instead of reusing `normalizeRepoPath` or `ResolveRunContext`. This is drift waiting to happen.

## acceptance criteria
- [ ] define minimal fix + tests

