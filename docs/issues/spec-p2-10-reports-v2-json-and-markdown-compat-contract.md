# [p2][spec][design] reports v2 json + markdown compatibility contract

labels: `p2`, `type:design`, `area:spec`

## summary
define the v2 reporting contract so `report.json` and markdown outputs stay
compatible, deterministic, and mode-aware.

## context
- section: v2.1 reports transition
- source: docs/v2.1/constitution.md + docs/v2.1/slice-roadmap.md
- details:
  - v2.1 scope includes report friction reduction with JSON-compatible outputs.
  - without an explicit contract, strictness and fallback behavior can drift between commands.
  - automation requires stable JSON fields while humans still need readable markdown.

## acceptance criteria
- [ ] define required/optional report fields across JSON and markdown modes
- [ ] define mode-aware strictness behavior and compatibility fallback rules
- [ ] define error behavior for malformed/oversized report inputs
- [ ] add tests ensuring deterministic serialization and backward compatibility
