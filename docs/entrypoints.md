# Entrypoints

## Scope

This document covers entrypoints and side effects.

## Rules

- The only binary entrypoint is `cmd/agency`.
- Cobra commands in `internal/cli/cobra` parse flags, construct dependencies, and delegate to `internal/commands`.
- `internal/commands` is the user-facing contract boundary for CLI behavior.
- The public CLI grammar is noun-scoped and target-first: `agency repo <repo-ref>`, `agency task <task-ref>`, `agency worktree <worktree-ref>`, and `agency agent <invocation-ref>` are the default show forms.
- The public CLI grammar has one canonical spelling per command. Do not add aliases, hidden alternate commands, or argument-rewrite layers.
- Collection verbs stay explicit: `agency repo ls`, `agency task ls`, `agency worktree ls`, and `agency agent ls`.
- Target actions stay target-first: `agency task <task-ref> watch`, `agency task <task-ref> retry`, `agency worktree <worktree-ref> open`, `agency worktree <worktree-ref> pr sync`, `agency agent <invocation-ref> kill`, and similar surfaces place the target before the action.
- `agency repo add` accepts an optional positional checkout path and uses cwd when the path is omitted.
- Path-targeted commands such as `agency init` and `agency doctor` use `--path <checkout-path>` and default to cwd when `--path` is omitted.
- `agency init --repo-config` writes repo-shared config at the registered repo canonical root, not in the current integration worktree or sandbox.
- Repo-aware commands such as `agency task start`, `agency worktree create`, and `agency agent start` accept explicit `--repo` selectors from any cwd. When `--repo` is omitted, they resolve the repo in this order: current directory, then error.
- `agency task start <name>` is the canonical high-level delegation surface. It creates one durable task, one integration worktree, and one primary invocation through the daemon task-start mutation.
- `agency task start <name>` defaults to `--mode headless` and requires `--prompt` or `--prompt-file`; `--mode headed` may use `--detached` and rejects prompt flags.
- `agency task start <name>` takes the task/worktree name positionally and defaults an omitted `--base` to the current branch of the selected checkout chosen by the same precedence as `agency worktree create`.
- `agency worktree create <name>` takes the worktree name positionally and defaults an omitted `--base` to the current branch of the selected checkout chosen by that same precedence.
- `agency agent start` takes no positional worktree argument.
- `agency agent start --worktree <worktree-ref>` is the explicit worktree override and the scriptable surface from any cwd.
- When `--worktree` is omitted, `agency agent start` resolves the worktree from cwd only when cwd is inside a present agency integration worktree. Otherwise `--worktree` is required.
- `agency agent start` defaults to `--mode headed`; `--mode headless` requires `--prompt` or `--prompt-file`.
- `agency task start`, `agency task <task-ref> retry`, `agency agent start`, and `agency worktree <worktree-ref> pr merge` honor explicit `--agency-config` and otherwise resolve repo-scoped runner defaults through: canonical repo-root `agency.json`, then per-repo config under `AGENCY_CONFIG_DIR`.
- `agency task start`, `agency task <task-ref> retry`, and `agency agent start` load user `config.json`, overlay repo-scoped runner-default fields from the selected `agency.json`, and apply explicit `--model` and `--effort` last.
- `agency task start`, `agency task <task-ref> retry`, and `agency agent start` source `runner_defaults.claude-code.permission_mode` from explicit `--permission-mode`, then user `config.json`.
- For `claude-code`, headed starts launch interactive Claude in tmux. Headless starts launch daemon-backed Claude through the print/stream-json path.
- Integration worktrees and sandboxes are execution surfaces and do not own repo-shared config.
- `agency worktree <worktree-ref> pr merge` and its verify/archive cleanup phases resolve repo-shared config from the registered repo canonical root and do not write repo-shared files into agency-managed worktrees.
- Daemon HTTP handlers are service entrypoints and delegate non-transport work into helper packages instead of accumulating policy inline.
- Bubble Tea programs and models are TUI entrypoints, not persistence owners.
- Only entrypoints should read terminals directly or decide final stdout and stderr rendering.
- Do not call `os.Chdir` in command flow. Pass working directories explicitly.
