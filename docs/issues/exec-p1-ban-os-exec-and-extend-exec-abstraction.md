# [p1][exec][refactor] ban os/exec outside internal/exec and extend exec abstraction

labels: `p1`, `type:refactor`, `area:exec`

## summary
ban os/exec outside internal/exec and extend exec abstraction

## context
- section: merged
- source: docs/issues.md (merged)
- merged items:
  - open, attach, worktree, run use os/exec directly
  - os/exec usage violates the hard rule
  - os/exec usage violates the hard rule
  - uses os/exec and duplicates internal/exec behavior
  - uses os/exec and reimplements process-group control
  - tmux/capture.go ignores the shared CommandRunner
  - os/exec usage violates the hard rule
  - internal/exec API is too narrow for real processes
- details:
  - This bypasses `exec.CommandRunner`, undermines testability, and makes behavior inconsistent across commands.
  -
  - `handlers.go` spawns runner processes with `osexec.Command` instead of `internal/exec`. unify process management.
  -
  - setup script execution uses `osexec.CommandContext`; route through `internal/exec`.
  -
  - timeouts, env overrides, and cleanup diverge from the core runner.
  -
  - duplicates logic from `internal/exec` and violates the hard rule.
  -
  - it defines a separate `Executor` and uses `os/exec`, duplicating behavior and violating the hard rule.
  -
  - `autostart.go` spawns the daemon with `osexec.Command` and writes logs directly.
  -
  - lack of a spawn/streaming interface forces `os/exec` in daemon, tmux, and commands. extend CommandRunner or add a new interface.
  -

## acceptance criteria
- [ ] define minimal fix + tests

