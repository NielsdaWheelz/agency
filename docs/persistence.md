# Persistence

## Scope

This document covers on-disk state, schema contracts, atomic writes, scans, and file permissions.

## Rules

- Runtime state is file-backed under `AGENCY_DATA_DIR`.
- Persist JSON state with atomic temp-file-plus-rename writes.
- Persist JSON with stable formatting and a trailing newline.
- `schema_version` is strict. Reject missing, empty, or unknown versions.
- Treat directory names as canonical ids during scans.
- Targeted loads should fail closed on invalid JSON or schema drift.
- Broad scans may mark broken records and continue.
- Private runtime state uses private permissions.
- Daemon mutation event logs are append-only runtime records; append failures fail the mutation.
