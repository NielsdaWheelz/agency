# Daemon Subsystem

## Scope

This document covers `internal/daemon` and its owned subpackages.

## Rules

- The daemon owns mutable control-plane policy for integration worktrees and invocations.
- Handler packages should keep transport concerns local and delegate reusable logic into helper packages.
- Reconciliation, checkpointing, landing, streaming, and repo-lock usage belong here, not in Cobra wiring.
- Durable state must be sufficient for restart recovery without relying on process-local memory.
