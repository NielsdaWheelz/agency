# Commands

## Scope

This document covers `internal/commands`.

## Rules

- `internal/commands` is the canonical user-facing contract layer for CLI behavior.
- `internal/commands` owns the noun-scoped target-first command contract: `agency repo <repo-ref>`, `agency worktree <worktree-ref>`, and `agency agent <invocation-ref>` default to show, while collection verbs stay explicit.
- `internal/commands` also owns the current-context contract: `agency context`, `agency context use <worktree-ref>`, and `agency context unset`.
- Targeted actions should remain target-first in this layer, for example `agency worktree <worktree-ref> open` and `agency agent <invocation-ref> kill`.
- Removed public spellings such as verb-first target actions, `worktree create --name`, and positional `agency agent start <worktree-ref>` should not be preserved through compatibility branches here.
- Commands may resolve context, call the daemon or lower-level services, and render output.
- Commands should not own low-level filesystem schemas, git primitives, or tmux implementation details.
- When a daemon mutation surface exists, commands should call it instead of mutating daemon-owned state directly.
