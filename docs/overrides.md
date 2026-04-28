# Overrides

## Scope

This document covers configuration and code escape hatches.

## Configuration Overrides

- Directory overrides are explicit: `AGENCY_DATA_DIR`, `AGENCY_CONFIG_DIR`, and `AGENCY_CACHE_DIR`.
- CLI flags may override user defaults only on surfaces that document that precedence.
- `--repo` is a repo ref selector, not a filesystem path.
- `--base` is the canonical base-branch selector for `agency task start` and `agency worktree create`.
- `--worktree` is the explicit worktree selector for `agency agent start`.
- `--path` is the explicit checkout-path override for path-targeted commands such as `agency init` and `agency doctor`.
- Do not add or document alternate spellings such as `--parent` or `--wt`.
- `agency task start <name>` takes the task/worktree name positionally.
- `agency task <task-ref> retry` reuses the task's existing integration worktree and starts a new primary invocation.
- `agency worktree create <name>` takes the worktree name positionally.
- `agency agent start` takes no positional worktree argument. Use `--worktree <worktree-ref>` for an explicit target.
- `agency agent start` requires `--worktree` when cwd is not inside a present integration worktree.
- `--mode` is the canonical mode selector for `agency task start`, `agency task <task-ref> retry`, and `agency agent start`.
- `--agency-config` is the explicit override for the selected agency config file.
- `agency task start`, `agency task <task-ref> retry`, and `agency agent start` accept `--agency-config` and use it before repo-shared and per-repo agency config.
- Daemon APIs that accept an agency config override require an absolute path.
- Runner and editor command mappings must stay explicit in user config.
- Runner-specific model and effort defaults belong in `runner_defaults`, not in `defaults`.
- Claude `model`, `effort`, and `permission_mode` are Agency-owned surfaces.
- Set Claude `permission_mode` only in `config.json`.
- Use typed `runner_defaults` or `--model`/`--effort` on `agency task start`, `agency task <task-ref> retry`, or `agency agent start` for Claude settings. Do not pass Claude ownership fields through `--runner-arg`.

## Dead Code

- Delete dead code by default.
- Delete superseded spellings and branches once the canonical surface changes.

## Code Escape Hatches

- Keep test-only overrides injected through dependencies, not hidden global state.
- Any override that changes safety-sensitive behavior should be local, explicit, and documented in the owning package or doc.
