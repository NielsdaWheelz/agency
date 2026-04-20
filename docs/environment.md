# Environment

## Scope

This document covers config paths, directory overrides, and precedence.

## Paths

- Repository-wide directory overrides are `AGENCY_DATA_DIR`, `AGENCY_CONFIG_DIR`, and `AGENCY_CACHE_DIR`.
- If those overrides are unset, path resolution follows macOS defaults first and XDG defaults on Linux.
- User config path is `AGENCY_CONFIG_DIR/config.json`.
- Local per-repo config path is `AGENCY_CONFIG_DIR/repos/<repo_id>/agency.json`.
- Repo-shared config path is `<repo>/agency.json`.
- `agency init` writes the local per-repo config by default.
- `agency init --repo-config` writes the repo-shared config in the target repo.
- `agency init` and `agency doctor` default to cwd and accept `--path <checkout-path>` when you want to target a different repo checkout.

## Precedence

- User config is loaded from `AGENCY_CONFIG_DIR/config.json`.
- Repo config resolution order is: explicit `--agency-config`, repo-local `<repo>/agency.json`, then per-repo config under `AGENCY_CONFIG_DIR`.
- `agency agent start` uses that repo config order for repo-scoped `runner_defaults`.
- If `--agency-config` is relative, `agency agent start` and `agency doctor` resolve it from the current directory before loading.
- Relative script paths resolve from the directory that contains the selected `agency.json`.
