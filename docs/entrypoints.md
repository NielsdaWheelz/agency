# Entrypoints

## Scope

This document covers entrypoints and side effects.

## Rules

- The only binary entrypoint is `cmd/agency`.
- Cobra commands in `internal/cli/cobra` should parse flags, construct dependencies, and delegate to `internal/commands`.
- `internal/commands` is the user-facing contract boundary for CLI behavior.
- Repo-aware creation and start commands should resolve repository context from explicit selector flags, not by inferring it from the current directory.
- Daemon HTTP handlers are service entrypoints and should delegate non-transport work into helper packages instead of accumulating policy inline.
- Bubble Tea programs and models are TUI entrypoints, not persistence owners.
- Only entrypoints should read terminals directly or decide final stdout and stderr rendering.
- Do not call `os.Chdir` in command flow. Pass working directories explicitly.
