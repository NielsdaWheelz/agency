# Concurrency

## Scope

This document covers locking, TOCTOU handling, and cross-system mutation ordering.

## Locking

- Repo- and worktree-mutating git operations must take the repo-level lock.
- When a daemon mutation surface exists for a capability, that daemon surface is the single mutable owner.
- Do not add parallel mutation paths that bypass the daemon and write the same state directly.
- `task start` must serialize task, integration worktree, sandbox, runner/tmux, and event side effects through the daemon task-start mutation and repo-level lock.
- `worktree pr merge` must serialize daemon-owned merge-state transitions and git/archive side effects through the repo lock.
- Do not hold repo locks longer than the git and persistence sequence that requires serialization.

## TOCTOU

- If a decision depends on filesystem, git, tmux, or process state that may change concurrently, re-read the relevant state after ambiguous failures.
- Prefer canonical path and durable metadata checks over volatile process-local assumptions.
- Resume paths for `worktree pr merge` should consult durable merge state first and then reconcile git, PR, and filesystem reality.

## Multi-System Mutation Ordering

- When a workflow spans store updates, git state, tmux state, and daemon-owned process supervision, choose an order that leaves recovery possible from durable state.
- Any committed external side effect must be discoverable after daemon restart from persisted metadata, append-only events, or the filesystem state itself.
- Persist durable worktree merge state before external merge or archive side effects that may need restart recovery.
