# Worktree

## Scope

This document covers `internal/worktree`.

## Rules

- `internal/worktree` owns low-level git worktree creation, removal helpers, and `.agency` scaffolding.
- It should remain a primitive layer.
- Repo locking, idempotency, active-invocation checks, and control-plane policy belong in the daemon or command layer, not here.
