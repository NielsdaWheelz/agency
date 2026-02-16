# [p1][checkpoint][enhancement] interactive history selector for checkpoint revert

labels: `p1`, `type:enhancement`, `area:checkpoint`

## summary
allow users to browse headless invocation history and revert to a selected point via
arrow-key terminal navigation, in addition to explicit checkpoint id selection.

## context
- section: v2.1 parity additions
- source: docs/v2.1/product-brief.md + docs/v2.1/parity-matrix.md
- details:
  - users should be able to navigate prior messages/tool activity and choose a revert point without manually mapping ids.
  - selector must remain terminal-only and lightweight (no full TUI requirement).
  - selected history point must map deterministically to a checkpoint restore + restart flow.
  - command UX must remain scriptable via non-interactive flags and JSON outputs.

## acceptance criteria
- [ ] design interactive terminal selector UX with arrow-key navigation
- [ ] map selected history event/turn to checkpoint id with deterministic rules
- [ ] implement integrated restore+restart behavior from selector choice
- [ ] preserve non-interactive fallback (`--checkpoint <id>`) and automation compatibility
