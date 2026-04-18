# Environment

## Scope

This document covers environment variable rules.

## Rules

- Repository-wide directory overrides are `AGENCY_DATA_DIR`, `AGENCY_CONFIG_DIR`, and `AGENCY_CACHE_DIR`.
- If those overrides are unset, path resolution follows macOS defaults first and XDG defaults on Linux.
- User config lives under `AGENCY_CONFIG_DIR/config.json` and uses schema version `2` only.
- Per-repo agency config lives under `AGENCY_CONFIG_DIR/repos/<repo_id>/agency.json` and uses schema version `2` only.
- User config owns global defaults such as `defaults.runner`, `defaults.editor`, and explicit runner/editor command mappings.
- User config and agency config may both define `runner_defaults`.
- Agency config resolution order is: explicit `--agency-config`, repo-local `<repo>/agency.json`, then per-repo config under `AGENCY_CONFIG_DIR`.
- `agency agent start` uses that agency config precedence to resolve repo-scoped `runner_defaults`.
- `agency init` writes per-repo version `2` config under `AGENCY_CONFIG_DIR` by default.
- `agency init --repo-config` writes shareable version `2` `agency.json` and scripts into the target repo.
- Environment reads should stay near the boundary that owns them.
- Production environment variables read by code must be documented in repo docs or in the owning package doc.
- Script and verify flows may export additional `AGENCY_*` variables as part of the workspace contract. Keep those names stable once published.
- Prefer explicit dependency injection in tests over hidden environment lookups.
