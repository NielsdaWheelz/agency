# Environment

## Scope

This document covers environment variable rules.

## Rules

- Repository-wide directory overrides are `AGENCY_DATA_DIR`, `AGENCY_CONFIG_DIR`, and `AGENCY_CACHE_DIR`.
- If those overrides are unset, path resolution follows macOS defaults first and XDG defaults on Linux.
- User config lives under `AGENCY_CONFIG_DIR/config.json`.
- Per-repo agency config lives under `AGENCY_CONFIG_DIR/repos/<repo_id>/agency.json`.
- Agency config resolution order is: explicit `--agency-config`, repo-local `<repo>/agency.json`, then per-repo config under `AGENCY_CONFIG_DIR`.
- `agency init` writes per-repo config under `AGENCY_CONFIG_DIR` by default.
- `agency init --repo-config` writes shareable `agency.json` and scripts into the target repo.
- Environment reads should stay near the boundary that owns them.
- Production environment variables read by code must be documented in repo docs or in the owning package doc.
- Script and verify flows may export additional `AGENCY_*` variables as part of the workspace contract. Keep those names stable once published.
- Prefer explicit dependency injection in tests over hidden environment lookups.
