# [p1][spec][design] Missing constraint to prevent accidental bad roots

labels: `p1`, `type:design`, `area:spec`

## summary
Missing constraint to prevent accidental bad roots

## context
- section: Open Issues / Notes
- source: docs/issues.md
- details:
  - Add invariants: refuse to run if repo root is inside `${AGENCY_DATA_DIR}` (avoid recursion weirdness) and refuse to run if worktree path already exists (should be impossible but worth asserting).

## acceptance criteria
- [ ] define minimal fix + tests

