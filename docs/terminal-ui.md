# Terminal UI

## Scope

This document covers Bubble Tea and terminal-facing workspace UI rules.

## Rules

- `internal/watch` owns the only Bubble Tea runtime in the codebase.
- `agency watch` and `agency agent history` should open explicit pages of that one runtime.
- `watch` is a terminal UI over daemon state, not a source of truth.
- The runtime should expose workspace, history, and logs pages over the same read model.
- Snapshot loading should compose daemon read APIs rather than reconstruct state from raw files.
- Interactive terminal checks belong at the command boundary before launching the UI.
- Invocation history UI should live in `internal/watch`, not in a second TUI package.
- UI actions should delegate to canonical command contracts instead of duplicating policy.
- Keep UI model state ephemeral and reconstructable from daemon reads.
