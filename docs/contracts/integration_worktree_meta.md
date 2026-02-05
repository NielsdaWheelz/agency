# integration worktree meta.json contract

this file defines the contract for integration worktree metadata.

## location

- `${AGENCY_DATA_DIR}/repos/<repo_id>/integration_worktrees/<worktree_id>/meta.json`

## schema (v1)

required fields:
- `schema_version` (string, must equal `1.0`)
- `worktree_id` (string)
- `name` (string)
- `repo_id` (string)
- `branch` (string)
- `parent_branch` (string)
- `tree_path` (string, absolute)
- `created_at` (rfc3339 utc)
- `state` (string, `present|archived`)

optional fields:
- `last_used_at` (rfc3339 utc)

## rules

1. schema_version is required and validated on read.
2. timestamps are rfc3339 utc.
3. `state` is monotonic: present -> archived, never reversed.
4. required fields are immutable after creation.
5. writes are atomic and use store helpers only.
6. permissions are private: 0700 dirs, 0600 files.

## stubs

- formal worktree_id format and validation rules
