# Environment

## Scope

This document covers config paths, directory overrides, and precedence.

## Paths

- Repository-wide directory overrides are `AGENCY_DATA_DIR`, `AGENCY_CONFIG_DIR`, and `AGENCY_CACHE_DIR`.
- If those overrides are unset, path resolution follows macOS defaults first and XDG defaults on Linux.
- User config path is `AGENCY_CONFIG_DIR/config.json`.
- Current context path is `AGENCY_CONFIG_DIR/current-context.json`.
- Local per-repo config path is `AGENCY_CONFIG_DIR/repos/<repo_id>/agency.json`.
- Repo-shared config path is `<canonical-repo-root>/agency.json`, where the canonical repo root is the registered repo `PreferredRoot`, else `RepoRootLastSeen`.
- `agency context use` and `agency context unset` update only `AGENCY_CONFIG_DIR/current-context.json`.
- `agency init` writes the local per-repo config by default.
- `agency init --repo-config` writes the repo-shared config at the canonical repo root.
- `agency init --repo-config` must not write repo-shared config into an agency-managed integration worktree or sandbox.
- `agency init` and `agency doctor` default to cwd and accept `--path <checkout-path>` when you want to target a different repo checkout.

## Precedence

- User config is loaded from `AGENCY_CONFIG_DIR/config.json`.
- Repo config resolution order is: explicit `--agency-config`, repo-shared `<canonical-repo-root>/agency.json`, then per-repo config under `AGENCY_CONFIG_DIR`.
- Ambient targeting uses this order: explicit flag, current directory, active context, then error.
- For `agency worktree create`, omitted `--repo` resolves from the current directory first and then from `AGENCY_CONFIG_DIR/current-context.json`; omitted `--base` resolves from the current branch of the selected checkout.
- For `agency agent start`, omitted `--repo` resolves from the current directory first and then from `AGENCY_CONFIG_DIR/current-context.json`; omitted `--worktree` resolves from cwd only when cwd already identifies a present integration worktree, then from `AGENCY_CONFIG_DIR/current-context.json`, then errors.
- `agency agent start` loads typed runner defaults from user `config.json`, overlays repo-scoped fields from the selected `agency.json`, and applies explicit `--model` and `--effort` last.
- `agency agent start` resolves Claude `permission_mode` from user `config.json` only.
- Merge, verify, and archive flows use the same canonical repo-root config resolution.
- Integration worktrees and sandboxes are execution surfaces only and do not contribute repo-shared config.
- If `--agency-config` is relative, `agency agent start` and `agency doctor` resolve it from the current directory before loading.
- Relative script paths resolve from the directory that contains the selected `agency.json`.
