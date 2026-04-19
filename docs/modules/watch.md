# Watch

## Scope

This document covers `internal/watch`.

## Rules

- `internal/watch` owns the single Bubble Tea runtime for workspace, history, and logs pages.
- It may expose explicit pages, but should not split into separate TUI runtimes.
- It should compose daemon read APIs into one snapshot.
- It should not own persistence or mutation policy.
- History and log views should be read-model pages over canonical daemon reads, not parallel UI stacks.
- Actions should forward into canonical command contracts such as `agency agent <invocation-ref> history` and `agency agent <invocation-ref> restore`.
