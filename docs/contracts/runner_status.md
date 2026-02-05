# runner_status.json contract

this file defines the contract between agency and runners.

## location

- `<worktree>/.agency/state/runner_status.json`

## schema (v1)

required fields:
- `schema_version` (string, must equal `1.0`)
- `status` (string, `working|needs_input|blocked|ready_for_review`)
- `updated_at` (rfc3339 utc)
- `summary` (string)

optional fields:
- `questions` (array of strings)
- `blockers` (array of strings)
- `how_to_test` (string)
- `risks` (array of strings)

## rules

1. schema_version is required and validated on read.
2. timestamps are rfc3339 utc.
3. `summary` is required for all statuses.
4. if `status=needs_input`, `questions` must be non-empty.
5. if `status=blocked`, `blockers` must be non-empty.
6. if `status=ready_for_review`, `how_to_test` must be non-empty.
7. writes are atomic and must not be partial.
8. permissions are private: 0700 dirs, 0600 files.

## stubs

- max lengths for summary and arrays
