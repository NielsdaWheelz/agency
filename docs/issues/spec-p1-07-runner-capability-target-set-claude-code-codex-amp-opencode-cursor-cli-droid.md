# [p1][spec][design] runner capability target set: claude-code, codex, amp, opencode, cursor-cli, droid

labels: `p1`, `type:design`, `area:spec`

## summary
define and enforce the v2.1 runner capability target set:
`claude-code`, `codex`, `amp`, `opencode`, `cursor-cli`, `droid`.

## context
- section: v2.1 parity additions
- source: docs/v2.1/constitution.md
- details:
  - v2.1 requires capability-driven runner support beyond current hardcoded `claude|codex`.
  - each runner needs explicit capability metadata (headed/headless, semantic stream parsing, resume behavior, default args safety).
  - unsupported semantic parsing must degrade to raw-log mode without breaking lifecycle or CLI contracts.
  - start/control-plane validation must reject unknown runner identifiers consistently with typed errors.

## acceptance criteria
- [ ] define canonical runner IDs and capability matrix contract
- [ ] remove hardcoded allowlists from CLI and daemon start paths
- [ ] document fallback behavior for runners without semantic adapters
- [ ] add tests proving all target runners pass validation and lifecycle startup paths
