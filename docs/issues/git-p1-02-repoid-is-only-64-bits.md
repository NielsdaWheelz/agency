# [p1][git][tech-debt] repo_id is only 64 bits

labels: `p1`, `type:tech-debt`, `area:git`

## summary
repo_id is only 64 bits

## context
- section: Audit: Git / Identity
- source: docs/issues.md
- details:
  - `RepoIDLen = 16` hex chars is collision-prone. use 128+ bits or full sha256 for “gold standard.”

## acceptance criteria
- [ ] define minimal fix + tests

