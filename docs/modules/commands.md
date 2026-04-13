# Commands

## Scope

This document covers `internal/commands`.

## Rules

- `internal/commands` is the canonical user-facing contract layer for CLI behavior.
- Commands may resolve context, call the daemon or lower-level services, and render output.
- Commands should not own low-level filesystem schemas, git primitives, or tmux implementation details.
- When a daemon mutation surface exists, commands should call it instead of mutating daemon-owned state directly.
