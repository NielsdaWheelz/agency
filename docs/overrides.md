# Overrides

## Scope

This document covers configuration and code escape hatches.

## Configuration Overrides

- Directory overrides are explicit: `AGENCY_DATA_DIR`, `AGENCY_CONFIG_DIR`, and `AGENCY_CACHE_DIR`.
- CLI flags may override user defaults only on surfaces that document that precedence.
- `--agency-config` is the explicit override for the selected agency config file.
- `agency agent start` accepts `--agency-config` and uses it before repo-local and per-repo agency config.
- Daemon APIs that accept an agency config override require an absolute path.
- Runner and editor command mappings must stay explicit in user config.
- Runner-specific model and effort defaults belong in `runner_defaults`, not in `defaults`.

## Dead Code

- Delete dead code by default.
- Do not keep compatibility layers or aliases once the canonical surface has changed unless the compatibility is an intentional public contract.

## Code Escape Hatches

- Keep test-only overrides injected through dependencies, not hidden global state.
- Any override that changes safety-sensitive behavior should be local, explicit, and documented in the owning package or doc.
