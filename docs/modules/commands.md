# Commands

## Scope

This document covers `internal/commands`.

## Rules

- `internal/commands` is the canonical user-facing contract layer for CLI behavior.
- `internal/commands` owns the noun-scoped target-first command contract: `agency repo <repo-ref>`, `agency task <task-ref>`, `agency worktree <worktree-ref>`, and `agency agent <invocation-ref>` default to show, while collection verbs stay explicit.
- `internal/commands` also owns the current-context contract: `agency context`, `agency context use <worktree-ref>`, and `agency context unset`.
- `internal/commands` owns ambient target inference and its precedence: explicit flag, current directory, active context, then error.
- Targeted actions should remain target-first in this layer, for example `agency task <task-ref> retry`, `agency worktree <worktree-ref> open`, and `agency agent <invocation-ref> kill`.
- `internal/commands` should implement and document only the canonical spellings it owns.
- Commands may resolve context, call the daemon or lower-level services, and render output.
- Commands should not own low-level filesystem schemas, git primitives, or tmux implementation details.
- When a daemon mutation surface exists, commands should call it instead of mutating daemon-owned state directly.
- `agency task start` should call the daemon task-start mutation rather than composing worktree and invocation mutations in the CLI.
