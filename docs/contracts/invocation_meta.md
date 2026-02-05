# invocation meta.json contract

this file defines the contract for invocation metadata.

## location

- `${AGENCY_DATA_DIR}/repos/<repo_id>/invocations/<invocation_id>/meta.json`

## schema (v1)

required fields:
- `schema_version` (string, must equal `1.0`)
- `invocation_id` (string)
- `integration_worktree_id` (string)
- `sandbox_path` (string, absolute)
- `sandbox_branch` (string)
- `base_commit` (string)
- `runner` (string)
- `mode` (string, `headed` or `headless`)
- `started_at` (rfc3339 utc)
- `status` (string, `starting|running|finished|failed`)
- `checkpoint_include_untracked` (bool)

optional fields:
- `invocation_name` (string)
- `pid` (int)
- `pgid` (int)
- `tmux_session` (string)
- `finished_at` (rfc3339 utc)
- `exit_reason` (string, one of: `exited`, `killed`, `stopped`, `start_failed`, `unknown`)
- `failure_reason` (string, one of: `start_incomplete`, `sandbox_missing`, `spawn_failed`, `runner_exit_nonzero`, `killed`, `stopped`, `daemon_shutdown`)
- `exit_code` (int)
- `last_output_at` (rfc3339 utc)
- `landing_status` (string, `pending|landed|discarded`)
- `prompt_source` (string, one of: `file`, `string`, `editor`, `interactive`)
- `prompt_path` (string)
- `prompt_sha256` (lowercase hex sha256)
- `stop_requested_at` (rfc3339 utc)
- `daemon_pid` (int)
- `daemon_instance_id` (uuid)
- `claimed_at` (rfc3339 utc)
- `lifecycle_owner` (string, `daemon` or empty)
- `orphaned_at` (rfc3339 utc)
- `flags` (object)
- `semantic_status` (string, see runner_status contract)
- `semantic_status_updated_at` (rfc3339 utc)

flags object:
- `needs_attention` (bool)
- `orphaned` (bool)
- `checkpoint_degraded` (bool)

## rules

1. schema_version is required and validated on read.
2. timestamps are rfc3339 utc.
3. status transitions are monotonic and terminal states are immutable.
4. `pid` and `pgid` are set only for headless mode.
5. `tmux_session` is set only for headed mode.
6. daemon-owned fields are only mutated by the daemon once `lifecycle_owner == "daemon"`.
7. writes are atomic and use store helpers only.
8. permissions are private: 0700 dirs, 0600 files.

## stubs

- formal invocation_id format and validation rules
