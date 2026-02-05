# run meta.json contract

this file defines the contract for run metadata.

## location

- `${AGENCY_DATA_DIR}/repos/<repo_id>/runs/<run_id>/meta.json`

## schema (v1)

required fields:
- `schema_version` (string, must equal `1.0`)
- `run_id` (string)
- `repo_id` (string)
- `name` (string)
- `runner` (string)
- `runner_cmd` (string)
- `parent_branch` (string)
- `branch` (string)
- `worktree_path` (string, absolute)
- `created_at` (rfc3339 utc)

optional fields:
- `tmux_session_name` (string)
- `flags` (object)
- `setup` (object)
- `pr_number` (int)
- `pr_url` (string)
- `last_push_at` (rfc3339 utc)
- `last_verify_at` (rfc3339 utc)
- `last_report_sync_at` (rfc3339 utc)
- `last_report_hash` (lowercase hex sha256)
- `archive` (object)

flags object:
- `setup_failed` (bool)
- `tmux_failed` (bool)
- `needs_attention` (bool)
- `needs_attention_reason` (string, one of: `verify_failed`, `stop_requested`, `user_marked`, `pr_not_mergeable`, `setup_failed`, `unknown`)
- `abandoned` (bool)

setup object:
- `command` (string)
- `exit_code` (int)
- `duration_ms` (int64)
- `timed_out` (bool)
- `log_path` (string, absolute)
- `output_ok` (bool)
- `output_summary` (string)

archive object:
- `archived_at` (rfc3339 utc)
- `merged_at` (rfc3339 utc)

## rules

1. schema_version is required and validated on read.
2. timestamps are rfc3339 utc.
3. required fields are immutable after creation.
4. optional fields are append-only or monotonic where applicable.
5. writes are atomic and use store helpers only.
6. permissions are private: 0700 dirs, 0600 files.

## stubs

- formal run_id format and validation rules
