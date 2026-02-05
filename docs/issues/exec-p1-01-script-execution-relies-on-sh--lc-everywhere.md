# [p1][exec][tech-debt] script execution relies on sh -lc everywhere

labels: `p1`, `type:tech-debt`, `area:exec`

## summary
script execution relies on sh -lc everywhere

## context
- section: Audit: Process Execution
- source: docs/issues.md
- details:
  - setup/verify/archive and runner spawn go through a shell even when the script path is known and executable. this adds injection risk, hides argv boundaries, and makes quoting bugs inevitable. prefer explicit argv execution and reserve shells for truly shell‑based commands.

## acceptance criteria
- [ ] define minimal fix + tests

