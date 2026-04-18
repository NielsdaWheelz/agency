# Configuration Cutover

## Scope

This note records the documentation cutover for config setup and schema references.

## Status

- The only command that creates `AGENCY_CONFIG_DIR/config.json` is `agency config init`.
- `docs/configuration.md` is the canonical config and setup doc.
- README keeps a short first-run example and quickstart.
- `docs/environment.md` owns config paths and precedence only.
- Repo docs use schema version `2` examples only.
- The repo-root `agency.json` example matches the version `2` schema.

## Ownership

- `docs/configuration.md` owns setup plus the `config.json` and `agency.json` schemas.
- `docs/environment.md` owns directory overrides, file locations, and config precedence.
- README owns the short operator-facing quickstart.

## Command Split

- `agency init` owns repo config and repo scripts only.
- `agency doctor` stays read-only.
