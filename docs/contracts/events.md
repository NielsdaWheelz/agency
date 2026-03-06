# events.jsonl contract

This file defines the contract for per-run and per-invocation events.

## locations

- run events: ${AGENCY_DATA_DIR}/repos/<repo_id>/runs/<run_id>/events.jsonl
- invocation events: ${AGENCY_DATA_DIR}/repos/<repo_id>/invocations/<invocation_id>/events.jsonl

## format

JSONL, one event per line.

### run event schema (legacy/v1 flows)

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

### invocation event schema (daemon-owned mutation flows)

```json
{
  "schema_version": "1.0",
  "seq": 42,
  "timestamp": "2026-01-19T12:00:00Z",
  "invocation_id": "<invocation_id>",
  "kind": "agency.followup_prompt",
  "data": {}
}
```

## rules

1. schema_version is required and must match exactly.
2. timestamp is RFC3339 UTC.
3. event kind is stable (`event` for run events, `kind` for invocation events). adding or changing event names requires updating this doc and adding tests.
4. data is a JSON object. keys must be stable and documented. avoid secrets. avoid large payloads.
5. ordering is file order. events are append-only; do not rewrite.
6. invocation event sequence allocation is invocation-scoped and monotonic (`seq`); concurrent producers must serialize through one writer contract.
7. writes are locked per run/invocation. concurrent writers must not interleave JSON.
8. permissions are private: 0700 dirs, 0600 files.
9. events are required in contract flows. append failure must fail the operation.
10. follow-up retries with the same `client_request_id` are idempotent and must not create duplicate invocation event lines.

## invocation event producers (v2.1 S3 PR-06)

All of the following append through one invocation-scoped writer contract:

- follow-up prompt writes (`agency.followup_prompt`)
- checkpoint lifecycle writes (`agency.checkpoint_created`, `agency.checkpoint_failed`, `agency.checkpoint_denylist_triggered`)
- checkpoint apply writes (`agency.checkpoint_apply_started`, `agency.checkpoint_applied`)
- land/discard lifecycle writes (`agency.land_*`, `agency.discard_*`, `agency.conflict_detected`)

## known run event names (non-exhaustive)

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

## known invocation event kinds (non-exhaustive)

- agency.followup_prompt
- agency.checkpoint_created
- agency.checkpoint_failed
- agency.checkpoint_denylist_triggered
- agency.checkpoint_apply_started
- agency.checkpoint_applied
- agency.land_started
- agency.land_failed
- agency.land_cleanup_warning
- agency.land_succeeded
- agency.discard_started
- agency.discard_stop_warning
- agency.discard_cleanup_warning
- agency.discard_succeeded
- agency.conflict_detected

