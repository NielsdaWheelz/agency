# [p2][spec][design] reports v2 json + markdown compatibility contract

labels: `p2`, `type:design`, `area:spec`

## summary
define the v2 reporting contract so `report.json` and markdown outputs stay
compatible, deterministic, and mode-aware.

## context
- section: v2.1 reports transition
- source: docs/v2.1/constitution.md + docs/v2.1/roadmap.md
- details:
  - v2.1 scope includes report friction reduction with JSON-compatible outputs.
  - without an explicit contract, strictness and fallback behavior can drift between commands.
  - automation requires stable JSON fields while humans still need readable markdown.
  - approved direction: `report.json` is authoritative when present; markdown remains compatibility input.
  - approved direction: strict mode applies to headless `agent review` / `agent pr sync` / `agent merge`; headed/compatibility paths remain deterministic fallback-first with explicit diagnostics.

## acceptance criteria
- [ ] define one canonical report model consumed by review/PR/merge progression.
- [ ] define deterministic precedence: `report.json` authoritative when present; markdown adapter/fallback behavior explicit.
- [ ] define mode-aware strictness contract: headless fail-closed, headed/compatibility fail-open with deterministic diagnostics.
- [ ] define typed deterministic error behavior for missing/malformed/oversized/schema-incompatible report inputs.
- [ ] define deterministic serialization guarantees across JSON and markdown inputs (additive/backward-compatible for automation).
- [ ] add tests proving precedence, strict/fallback behavior, deterministic errors, and markdown-only backward compatibility.
