# [p3][repo][tech-debt] runtime artifacts committed to source tree

labels: `p3`, `type:tech-debt`, `area:repo`

## summary
runtime artifacts committed to source tree

## context
- section: Audit: Repo Hygiene
- source: docs/issues.md
- details:
  - `internal/runservice/repos/runs/logs/setup.log` looks like generated data and should live under `testdata/` or be removed.

## acceptance criteria
- [ ] define minimal fix + tests

