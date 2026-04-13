# Terminal UI

## Scope

This document covers Bubble Tea and terminal-facing workspace UI rules.

## Rules

- `watch` is a terminal UI over daemon state, not a source of truth.
- Snapshot loading should compose daemon read APIs rather than reconstruct state from raw files.
- Interactive terminal checks belong at the command boundary before launching the UI.
- UI actions should delegate to canonical command contracts instead of duplicating policy.
- Keep UI model state ephemeral and reconstructable from daemon reads.
