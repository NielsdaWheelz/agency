# [p1][paths][tech-debt] homeDir is assumed absolute but never validated

labels: `p1`, `type:tech-debt`, `area:paths`

## summary
homeDir is assumed absolute but never validated

## context
- section: Audit: Paths
- source: docs/issues.md
- details:
  - `ResolveDirs` docs require absolute; invalid input silently produces garbage paths. validate or normalize at the boundary.

## acceptance criteria
- [ ] define minimal fix + tests

