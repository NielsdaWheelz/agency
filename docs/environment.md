# Environment

## Scope

This document covers config paths, directory overrides, and precedence.

## Paths

- Repository-wide directory overrides are `AGENCY_DATA_DIR`, `AGENCY_CONFIG_DIR`, and `AGENCY_CACHE_DIR`.
- If those overrides are unset, path resolution follows macOS defaults first and XDG defaults on Linux.
- `AGENCY_DATA_DIR` stores daemon-owned metadata, events, logs, checkpoints, and sockets. It does not determine managed checkout placement.
- User config path is `AGENCY_CONFIG_DIR/config.json`.
- Local per-repo config path is `AGENCY_CONFIG_DIR/repos/<repo_id>/agency.json`.
- Repo-shared config path is `<canonical-repo-root>/agency.json`, where the canonical repo root is the registered repo `PreferredRoot`, else `RepoRootLastSeen`.
- `agency init` writes the local per-repo config by default.
- `agency init --repo-config` writes the repo-shared config at the canonical repo root.
- `agency init --repo-config` must not write repo-shared config into an agency-managed integration worktree or sandbox.
- `agency init` and `agency doctor` default to cwd and accept `--path <checkout-path>` when you want to target a different repo checkout.
- The default managed checkout policy is `repo-sibling`, which resolves to `<canonical-repo-parent>/.agency/checkouts/<repo-id>/`.
- If `agency.json` sets an absolute `execution.checkout_root`, the repo-scoped checkout root is `<execution.checkout_root>/<repo-id>/`.
- Integration worktrees live under `<checkout-root>/worktrees/`; invocation sandboxes live under `<checkout-root>/sandboxes/`.
- Managed checkout roots must stay outside the canonical repo root.

## Precedence

- User config is loaded from `AGENCY_CONFIG_DIR/config.json`.
- Repo config resolution order is: explicit `--agency-config`, repo-shared `<canonical-repo-root>/agency.json`, then per-repo config under `AGENCY_CONFIG_DIR`.
- Ambient targeting uses this order: explicit flag, current directory, then error.
- For `agency task start`, omitted `--repo` resolves from the current directory; omitted `--base` resolves from the current branch of the selected checkout.
- For `agency worktree create`, omitted `--repo` resolves from the current directory; omitted `--base` resolves from the current branch of the selected checkout.
- For `agency agent start`, omitted `--repo` resolves from the current directory; omitted `--worktree` resolves from cwd only when cwd already identifies a present integration worktree. Otherwise `--worktree` is required. Omitted `--mode` defaults to `headed`.
- `agency task start`, `agency task <task-ref> retry`, and `agency agent start` load typed runner defaults from user `config.json`, overlay repo-scoped fields from the selected `agency.json`, and apply explicit `--model` and `--effort` last.
- `agency task start`, `agency task <task-ref> retry`, and `agency agent start` resolve Claude `permission_mode` from explicit `--permission-mode`, then user `config.json`.
- `agency task start`, `agency task <task-ref> retry`, and `agency agent start` resolve execution profile from explicit `--execution-profile`, selected `agency.json` `execution.profile`, then user `config.json` `defaults.execution_profile`.
- Checkout root resolution is selected `agency.json` `execution.checkout_root`, then `repo-sibling`.
- `agency worktree <worktree-ref> pr merge` and its verify/archive cleanup phases use the same canonical repo-root config resolution.
- Integration worktrees and sandboxes are execution surfaces only and do not contribute repo-shared config.
- If `--agency-config` is relative, `agency task start`, `agency task <task-ref> retry`, `agency agent start`, `agency doctor`, and `agency worktree <worktree-ref> pr merge` resolve it from the current directory before loading.
- Relative script paths resolve from the directory that contains the selected `agency.json`.
