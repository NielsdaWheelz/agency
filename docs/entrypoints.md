# Entrypoints

## Scope

This document covers entrypoints and side effects.

## Rules

- The only binary entrypoint is `cmd/agency`.
- Cobra commands in `internal/cli/cobra` should parse flags, construct dependencies, and delegate to `internal/commands`.
- `internal/commands` is the user-facing contract boundary for CLI behavior.
- The public CLI grammar is noun-scoped and target-first: `agency repo <repo-ref>`, `agency worktree <worktree-ref>`, and `agency agent <invocation-ref>` are the default show forms.
- `agency r`, `agency wt`, and `agency ag` are supported aliases for `repo`, `worktree`, and `agent`, but docs and help should keep the long noun forms as the primary surface.
- Collection verbs stay explicit: `agency repo ls`, `agency worktree ls`, and `agency agent ls`.
- Target actions should stay target-first: `agency worktree <worktree-ref> open`, `agency worktree <worktree-ref> pr sync`, `agency agent <invocation-ref> kill`, and similar surfaces place the target before the action.
- `agency repo add` should accept an optional positional checkout path and use cwd when the path is omitted.
- Path-targeted commands such as `agency init` and `agency doctor` should use `--path <checkout-path>` and default to cwd when `--path` is omitted.
- `agency init --repo-config` should write repo-shared config at the registered repo canonical root, not in the current integration worktree or sandbox.
- Repo-aware commands such as `agency worktree create` and `agency agent start` should accept explicit `--repo` selectors from any cwd and fall back to the current directory only when the selector is omitted.
- `agency worktree create <name>` should take the worktree name positionally and default an omitted `--base` to the current branch of the selected checkout.
- `agency agent start [<worktree-ref>]` should take the worktree ref positionally and may infer an omitted ref only when cwd is inside a present agency integration worktree.
- `agency agent start` should honor explicit `--agency-config` and otherwise resolve repo-scoped runner defaults through: canonical repo-root `agency.json`, then per-repo config under `AGENCY_CONFIG_DIR`.
- Integration worktrees and sandboxes are execution surfaces and do not own repo-shared config.
- Merge, verify, and archive entrypoints should resolve repo-shared config from the registered repo canonical root and should not write repo-shared files into agency-managed worktrees.
- Legacy verb-first target forms and the removed `--name` and `--worktree` flags should not retain compatibility paths.
- Daemon HTTP handlers are service entrypoints and should delegate non-transport work into helper packages instead of accumulating policy inline.
- Bubble Tea programs and models are TUI entrypoints, not persistence owners.
- Only entrypoints should read terminals directly or decide final stdout and stderr rendering.
- Do not call `os.Chdir` in command flow. Pass working directories explicitly.
