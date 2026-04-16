# Process Execution

## Scope

This document covers external command execution, runner startup, tmux control, and environment merging.

## Rules

- Do not call `os/exec` outside `internal/exec`.
- Prefer argv slices over shell command strings.
- Agency script `path` values are executable paths, not shell snippets.
- Relative agency script paths resolve from the directory containing the selected agency config file.
- Setup, verify, and archive scripts must be launched directly by argv; do not wrap script paths in `sh -lc`.
- Every process launch must accept a `context.Context`.
- Use `tmux.Client` for tmux operations instead of shelling out ad hoc.
- Resolve runner and editor executables through `internal/config`.
- Deterministic environment merging is required for noninteractive verify, merge, and archive flows.
- Archive scripts must be idempotent enough to tolerate cleanup retries after PR merge already succeeded.
- Long-lived runner lifecycle belongs to the daemon or tmux, not to detached goroutines in commands.
- Recreating a headed invocation must keep the same invocation id and sandbox; only the missing tmux session and daemon supervision are recreated.
