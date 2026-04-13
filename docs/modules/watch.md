# Watch

## Scope

This document covers `internal/watch`.

## Rules

- `internal/watch` owns the read-model and Bubble Tea workspace.
- It should compose daemon read APIs into one snapshot.
- It should not own persistence or mutation policy.
- Actions should forward into canonical command contracts.
