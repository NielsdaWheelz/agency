# Entrypoints

## Scope

This document covers entrypoints and side effects.

## Rules

- The only binary entrypoint is `cmd/agency`.
- Cobra commands in `internal/cli/cobra` should parse flags, construct dependencies, and delegate to `internal/commands`.
- `internal/commands` is the user-facing contract boundary for CLI behavior.
- The public CLI grammar is noun-scoped and target-first: `agency repo <repo-ref>`, `agency worktree <worktree-ref>`, and `agency agent <invocation-ref>` are the default show forms.
- The public CLI grammar has one canonical spelling per command. Do not add aliases, hidden compatibility commands, or argument-rewrite layers.
- Collection verbs stay explicit: `agency repo ls`, `agency worktree ls`, and `agency agent ls`.
- `agency context` is the current-context surface: `agency context` shows it, `agency context use <worktree-ref>` sets it, and `agency context unset` clears it.
- Target actions should stay target-first: `agency worktree <worktree-ref> open`, `agency worktree <worktree-ref> pr sync`, `agency agent <invocation-ref> kill`, and similar surfaces place the target before the action.
- `agency repo add` should accept an optional positional checkout path and use cwd when the path is omitted.
- Path-targeted commands such as `agency init` and `agency doctor` should use `--path <checkout-path>` and default to cwd when `--path` is omitted.
- `agency init --repo-config` should write repo-shared config at the registered repo canonical root, not in the current integration worktree or sandbox.
- Repo-aware commands such as `agency worktree create` and `agency agent start` should accept explicit `--repo` selectors from any cwd. When `--repo` is omitted, they should resolve the repo in this order: current directory, active context, then error.
- `agency worktree create <name>` should take the worktree name positionally and default an omitted `--base` to the current branch of the selected checkout chosen by that same precedence.
- `agency agent start` should take no positional worktree argument.
- `agency agent start --worktree <worktree-ref>` is the explicit worktree override and the scriptable surface from any cwd.
- When `--worktree` is omitted, `agency agent start` should resolve the worktree from cwd first, but only when cwd is inside a present agency integration worktree. Otherwise it should fall back to the active context and then error.
- `agency agent start` should default to headed mode.
- `agency agent start` should honor explicit `--agency-config` and otherwise resolve repo-scoped runner defaults through: canonical repo-root `agency.json`, then per-repo config under `AGENCY_CONFIG_DIR`.
- `agency agent start` should load user `config.json`, overlay repo-scoped runner-default fields from the selected `agency.json`, and apply explicit `--model` and `--effort` last.
- `agency agent start` should source `runner_defaults.claude-code.permission_mode` from user `config.json` only.
- For `claude-code`, headed starts should launch interactive Claude in tmux. Headless starts should launch daemon-backed Claude through the print/stream-json path.
- Integration worktrees and sandboxes are execution surfaces and do not own repo-shared config.
- Merge, verify, and archive entrypoints should resolve repo-shared config from the registered repo canonical root and should not write repo-shared files into agency-managed worktrees.
- Daemon HTTP handlers are service entrypoints and should delegate non-transport work into helper packages instead of accumulating policy inline.
- Bubble Tea programs and models are TUI entrypoints, not persistence owners.
- Only entrypoints should read terminals directly or decide final stdout and stderr rendering.
- Do not call `os.Chdir` in command flow. Pass working directories explicitly.
