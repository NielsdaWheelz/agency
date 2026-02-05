# repo_index.json contract

this file defines the contract for the repo index.

## location

- `${AGENCY_DATA_DIR}/repo_index.json`

## schema (v1)

root:
- `schema_version` (string, must equal `1.0`)
- `repos` (object map)

`repos` map:
- key: `repo_key` (string)
- value: entry object

entry object:
- `repo_id` (string)
- `paths` (array of absolute paths)
- `last_seen_at` (rfc3339 utc)

## rules

1. schema_version is required and validated on read.
2. `repo_key` is stable for a given repo root.
3. `paths[0]` is the most recently seen absolute path.
4. `paths` are de-duplicated and cleaned (no `.`/`..`).
5. writes are atomic and use store helpers only.
6. permissions are private: 0700 dirs, 0600 files.

## stubs

- repo_key definition and normalization
- case-sensitivity policy for `paths`
