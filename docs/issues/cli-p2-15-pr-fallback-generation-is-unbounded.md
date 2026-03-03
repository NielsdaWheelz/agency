# [p2][cli][tech-debt] PR fallback generation is unbounded

labels: `p2`, `type:tech-debt`, `area:cli`

## summary
PR fallback generation is unbounded

## context
- section: Audit: Commands
- source: docs/issues.md
- details:
  - fallback PR-body generation must stay bounded across canonical and compatibility paths.
  - large commit/file histories must not cause unbounded in-memory ingestion during fallback assembly.
  - truncation/fallback behavior must be explicit and deterministic for automation and humans.

## acceptance criteria
- [ ] enforce bounded commit/file/stat reads for all fallback PR-body generation paths used by progression flows.
- [ ] define deterministic truncation/fallback signaling in generated bodies and warning/diagnostic paths.
- [ ] verify no fallback path performs unbounded in-memory ingestion under large repository inputs.
- [ ] add tests covering large commit/file inputs and deterministic bounded behavior.
