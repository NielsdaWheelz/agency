# Store

## Scope

This document covers `internal/store`.

## Rules

- `internal/store` owns path layout, JSON schema types, scans, and load/save behavior under `AGENCY_DATA_DIR`.
- Store code should not own git, tmux, or daemon mutation policy.
- Targeted loads fail closed on schema drift or invalid JSON.
- Broad scans may continue and mark records broken when that preserves user visibility.
