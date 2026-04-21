# Daemon Subsystem

## Scope

This document covers `internal/daemon` and its owned subpackages.

## Rules

- The daemon owns mutable control-plane policy for integration worktrees and invocations.
- Handler packages should keep transport concerns local and delegate reusable logic into helper packages.
- Reconciliation, checkpointing, landing, streaming, and repo-lock usage belong here, not in Cobra wiring.
- Durable state must be sufficient for restart recovery without relying on process-local memory.
- The durable `worktree pr merge` lifecycle belongs here, including request acceptance, persisted merge state, execution, archive cleanup, and restart recovery.
- Explicit worktree merge-status reads belong in daemon read surfaces, not in transport-local state or Cobra glue.
- Transport disconnects do not cancel accepted daemon-owned worktree merge lifecycles.
