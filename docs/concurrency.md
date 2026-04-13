# Concurrency

## Scope

This document covers locking, TOCTOU handling, and cross-system mutation ordering.

## Locking

- Repo- and worktree-mutating git operations must take the repo-level lock.
- When a daemon mutation surface exists for a capability, that daemon surface is the single mutable owner.
- Do not add parallel mutation paths that bypass the daemon and write the same state directly.
- Do not hold repo locks longer than the git and persistence sequence that requires serialization.

## TOCTOU

- If a decision depends on filesystem, git, tmux, or process state that may change concurrently, re-read the relevant state after ambiguous failures.
- Prefer canonical path and durable metadata checks over volatile process-local assumptions.

## Multi-System Mutation Ordering

- When a workflow spans store updates, git state, tmux state, and daemon-owned process supervision, choose an order that leaves recovery possible from durable state.
- Any committed external side effect must be discoverable after daemon restart from persisted metadata, append-only events, or the filesystem state itself.
