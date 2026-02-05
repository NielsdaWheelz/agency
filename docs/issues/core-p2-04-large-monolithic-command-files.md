# [p2][core][tech-debt] Large, monolithic command files

labels: `p2`, `type:tech-debt`, `area:core`

## summary
Large, monolithic command files

## context
- section: Quality Gaps (Global)
- source: docs/issues.md
- details:
  - `merge.go`, `push.go`, `agent.go` are 1000+ LOC with mixed concerns. Break into services (domain logic) + thin CLI adapters to improve testability and evolvability.

## acceptance criteria
- [ ] define minimal fix + tests

