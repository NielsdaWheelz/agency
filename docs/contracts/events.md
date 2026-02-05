# events.jsonl contract

This file defines the contract for per-run and per-invocation events.

## locations

- run events: ${AGENCY_DATA_DIR}/repos/<repo_id>/runs/<run_id>/events.jsonl
- invocation events: ${AGENCY_DATA_DIR}/repos/<repo_id>/invocations/<invocation_id>/events.jsonl

## format

JSONL, one event per line. Each line is a single JSON object with this schema:

```json
{
  "schema_version": "1.0",
  "timestamp": "2026-01-19T12:00:00Z",
  "repo_id": "<repo_id>",
  "run_id": "<run_id>",
  "event": "cmd_start",
  "data": {}
}
```

## rules

1. schema_version is required and must match exactly.
2. timestamp is RFC3339 UTC.
3. event is a stable string. adding or changing event names requires updating this doc and adding tests.
4. data is a JSON object. keys must be stable and documented. avoid secrets. avoid large payloads.
5. ordering is file order. events are append-only; do not rewrite.
6. writes are locked per run/invocation. concurrent writers must not interleave JSON.
7. permissions are private: 0700 dirs, 0600 files.
8. events are required in contract flows. append failure must fail the operation.

## known event names (non-exhaustive)

- cmd_start, cmd_end
- stop, kill_session
- resume_attach, resume_create, resume_restart, resume_failed
- clean_started, clean_finished, clean_failed, dirty_allowed
- branch_deleted, pr_closed
- push_started, push_finished, push_failed
- git_fetch_finished, git_push_finished
- pr_created, pr_body_synced, pr_resolution_attempt
- merge_started, merge_finished, merge_failed
- merge_prechecks_passed
- merge_confirm_prompted, merge_confirmed, merge_already_merged
- verify_continue_prompted, verify_continue_rejected, verify_continue_accepted
- gh_merge_started, gh_merge_finished
- verify_started, verify_finished
- archive_started, archive_finished, archive_failed
