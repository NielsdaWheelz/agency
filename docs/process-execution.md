# Process Execution

## Scope

This document covers external command execution, runner startup, tmux control, and environment merging.

## Rules

- Do not call `os/exec` outside `internal/exec`.
- Prefer argv slices over shell command strings.
- Every process launch must accept a `context.Context`.
- Use `tmux.Client` for tmux operations instead of shelling out ad hoc.
- Resolve runner and editor executables through `internal/config`.
- Deterministic environment merging is required for noninteractive verify, merge, and archive flows.
- Archive scripts must be idempotent enough to tolerate cleanup retries after PR merge already succeeded.
- Long-lived runner lifecycle belongs to the daemon or tmux, not to detached goroutines in commands.
