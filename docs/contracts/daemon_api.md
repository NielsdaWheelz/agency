# daemon api contract

this file defines the daemon http api. it is normative.

## transport

- unix socket: `${AGENCY_DATA_DIR}/agencyd.sock`
- protocol: http/1.1 over unix domain socket
- all requests and responses are json
- endpoints that emit `request_id` in the body must also emit matching `X-Request-ID` response header
- inbound `X-Request-ID` values are accepted only if they are non-empty, <=128 chars, and match `[A-Za-z0-9][A-Za-z0-9._:-]*`; invalid values are replaced with a daemon-generated id

## versioning

- `api_version` is required on control-plane responses.
- any breaking change must bump `api_version` and update this doc.

## common error shape

errors return `ok=false` and must include:
- `error_code` (string)
- `message` (string)
- `hint` (string, optional)

for invocation-scoped mutation endpoints and review (`POST /invocations/start_headless`, `POST /invocations/start_headed`, `POST /invocations/{id}/start_headless`, `POST /invocations/{id}/stop`, `POST /invocations/{id}/kill`, `POST /invocations/{id}/checkpoints/apply`, `POST /invocations/{id}/land`, `POST /invocations/{id}/discard`, `POST /invocations/{ref}/chat`, `POST /invocations/{ref}/restart`, `POST /invocations/{ref}/pr/sync`, `POST /invocations/{ref}/merge`, `GET /invocations/{ref}/review`), responses must include daemon-issued `request_id` in both success and failure payloads.

## endpoints

### GET /health

response: `HealthResponse`
- `ok`, `api_version`, `build_version`, `git_sha`, `pid`, `daemon_instance_id`, `uptime_seconds`

### POST /shutdown

response: `ShutdownResponse`
- `ok` or `error_code=E_DAEMON_BUSY` with `running_invocations`

### POST /invocations/start_headless (control plane)

request: `ControlPlaneStartRequest`
- `repo_root`, `worktree_ref`, `runner`, `prompt`, `client_request_id`
- optional: `invocation_name`, `runner_args`, `env`, `no_include_untracked`

response: `ControlPlaneStartResponse`
- success fields: `ok`, `request_id`, `invocation_id`, `sandbox_path`, `repo_id`, `integration_worktree_id`, `integration_worktree_name`, `pid`, `pgid`, `daemon_instance_id`, `already_running`, `log_paths`, `api_version`, `build_version`, `client_request_id`
- error fields: `ok=false`, `request_id`, `api_version`, `build_version`, `error_code`, `message`, `hint`, `client_request_id`

### POST /invocations/start_headed (control plane)

request: `ControlPlaneStartHeadedRequest`
- `repo_root`, `worktree_ref`, `runner`, `client_request_id`
- optional: `invocation_name`, `runner_args`, `env`, `no_include_untracked`

response: `ControlPlaneStartHeadedResponse`
- success fields: `ok`, `request_id`, `invocation_id`, `sandbox_path`, `repo_id`, `integration_worktree_id`, `integration_worktree_name`, `tmux_session`, `daemon_instance_id`, `already_running`, `api_version`, `build_version`, `git_sha`, `client_request_id`
- error fields: `ok=false`, `request_id`, `api_version`, `build_version`, `git_sha`, `error_code`, `message`, `hint`, `client_request_id`

### POST /invocations/{id}/start_headless (legacy)

deprecated. no new features. remove in v2.
request: `StartHeadlessRequest`
response: `StartHeadlessResponse`
- success fields: `ok`, `request_id`, `pid`, `pgid`, `daemon_instance_id`, `already_running`, `orphaned`, `log_paths`
- error fields: `ok=false`, `request_id`, `error_code`, `message`, `hint`

### POST /invocations/{id}/stop

query:
- `repo_id` (required)

response: `StopResponse`
- success fields: `ok`, `request_id`, `invocation_id`, `api_version`, `build_version`, `client_request_id`
- error fields: `ok=false`, `request_id`, `api_version`, `build_version`, `client_request_id`, `error_code`, `message`, `hint`

### POST /invocations/{id}/kill

query:
- `repo_id` (required)

response: `KillResponse`
- success fields: `ok`, `request_id`, `invocation_id`, `api_version`, `build_version`, `client_request_id`
- error fields: `ok=false`, `request_id`, `api_version`, `build_version`, `client_request_id`, `error_code`, `message`, `hint`

### POST /invocations/{id}/checkpoints/apply

query:
- `repo_id` (required)

request: `CheckpointApplyRequest`
response: `CheckpointApplyResponse`
- success fields: `ok`, `request_id`, `api_version`, `build_version`, `checkpoint_id`, `snapshot_commit`, `restored_at`
- error fields: `ok=false`, `request_id`, `api_version`, `build_version`, `error_code`, `message`, `hint`

### POST /invocations/{id}/land

query:
- `repo_id` (required)

request: `LandRequest`
response: `LandResponse`
- success fields: `ok`, `request_id`, `api_version`, `build_version`, `invocation_id`, `applied_mode`, `integration_head_before`, `integration_head_after`, `commits_landed`
- error fields: `ok=false`, `request_id`, `api_version`, `build_version`, `error_code`, `message`, `hint`, optional `conflict_files[]`

### POST /invocations/{id}/discard

query:
- `repo_id` (required)

request: `DiscardRequest`
response: `DiscardResponse`
- success fields: `ok`, `request_id`, `api_version`, `build_version`, `invocation_id`
- error fields: `ok=false`, `request_id`, `api_version`, `build_version`, `error_code`, `message`, `hint`

### POST /invocations/{ref}/chat

query:
- `repo_id` (required)

request: `ControlPlaneFollowUpPromptRequest`
- required: `prompt`, `client_request_id`

response: `ControlPlaneFollowUpPromptResponse`
- success fields: `ok`, `request_id`, `api_version`, `build_version`, `invocation_id`, `timeline_entry_id`, `already_applied`, `client_request_id`
- error fields: `ok=false`, `request_id`, `api_version`, `build_version`, `error_code`, `message`, `hint`, `client_request_id`

### POST /invocations/{ref}/restart

query:
- `repo_id` (required)

request: `RestartFromCheckpointRequest`
- required: `checkpoint_id`
- optional: `runner_args`, `env`

response: `RestartFromCheckpointResponse`
- success fields: `ok`, `request_id`, `api_version`, `build_version`, `invocation_id`, `checkpoint_id`, `snapshot_commit`, `restored_at`, `pid`, `pgid`, `daemon_instance_id`, `log_paths`
- error fields: `ok=false`, `request_id`, `api_version`, `build_version`, `error_code`, `message`, `hint`

### GET /invocations/{ref}/review

query:
- `repo_id` (optional)

response envelope: `APIResponse` with:
- `ok`, `api_version`, `build_version`, `git_sha`, `request_id`, `data`
- `data` is `InvocationReviewData`
- required review data fields: `invocation_id`, `repo_id`, `status`, `display_status`, `landing_status`, `readiness`, `ready`, `pr_sync_eligible`, `blocking_reasons[]`, `navigation`
- additive report fields (headless strict report contract): `report_source`, `report_diagnostics[]`

error envelope:
- `ok=false`, `request_id`, `error_code`, `message`, `hint`, optional `details`

### POST /invocations/{ref}/pr/sync

query:
- `repo_id` (required)

request: `PRSyncRequest`
- optional: `allow_dirty`, `force_with_lease`

response: `PRSyncResponse`
- success fields: `ok`, `api_version`, `build_version`, `request_id`, `invocation_id`, `repo_id`, `integration_worktree_id`, `branch`, `pr_number`, `pr_url`, `pr_action`, `report_source`, `report_fallback_used`, `report_diagnostics[]`
- error fields: `ok=false`, `api_version`, `build_version`, `request_id`, `error_code`, `message`, `hint`

report contract behavior:
- headless mode is strict and fail-closed: report contract violations return typed deterministic errors (`E_REPORT_MISSING`, `E_REPORT_MALFORMED`, `E_REPORT_OVERSIZED`, `E_REPORT_SCHEMA_INCOMPATIBLE`, `E_REPORT_INCOMPLETE`)
- headed mode is compatibility-first: progression remains allowed with fallback body generation and explicit report diagnostics

### POST /invocations/{ref}/merge

query:
- `repo_id` (required)

request: `MergeRequest`
- required: `confirmation_mode` (`yes` or `typed`), `confirmed=true`
- optional: `strategy` (`squash` default, `merge`, `rebase`), `no_delete_branch`

response: `MergeResponse`
- success fields: `ok`, `api_version`, `build_version`, `request_id`, `invocation_id`, `repo_id`, `integration_worktree_id`, `branch`, `pr_number`, `pr_url`, `strategy`, `delete_branch`, `merge_log_path`, `verify_log_path`, `report_source`, `report_fallback_used`, `report_diagnostics[]`
- error fields: `ok=false`, `api_version`, `build_version`, `request_id`, `error_code`, `message`, `hint`

report contract behavior:
- headless mode is strict and fail-closed: report contract violations return typed deterministic errors (`E_REPORT_MISSING`, `E_REPORT_MALFORMED`, `E_REPORT_OVERSIZED`, `E_REPORT_SCHEMA_INCOMPATIBLE`, `E_REPORT_INCOMPLETE`)
- headed mode is compatibility-first: merge progression remains allowed and diagnostics are returned in success payload when fallback behavior is used

### POST /worktrees/create

request: `WorktreeCreateRequest`
response: `WorktreeCreateResponse`

### POST /worktrees/{id}/rm

query:
- `repo_id` (required)

request: `WorktreeRmRequest`
response: `WorktreeRmResponse`

## rules

1. all request bodies are strict json (no unknown fields).
2. `client_request_id` is required and must be a uuid.
3. idempotency is required for control-plane starts and worktree create.
4. `prompt` max size is 256kb.
5. daemon never mutates store files outside documented contracts.
6. legacy endpoints are frozen and must not be used by new clients.

### GET /invocations/{ref}/logs

query:
- `repo_id` (optional)
- `kind`: `raw` (default), `stderr`, `stream`

**offset mode** (PR-B): when `offset` query param is present
- `offset` (int64, >= 0): byte offset from start of file
- `limit` (int, default 65536, max 1048576): max bytes returned

response (offset mode): `InvocationLogsOffsetData`
- `kind`, `data_b64` (base64-encoded bytes), `next_offset`, `total_bytes`

**tail mode** (legacy): when `offset` is absent
- `tail_bytes` (int, default 65536, max 1048576): bytes from end of file

response (tail mode): `InvocationLogsData`
- `kind`, `content`, `truncated`, `total_bytes`, `returned_bytes`, `starts_midline`, `ends_midline`

error codes:
- `E_INVOCATION_NOT_FOUND`: invocation ref not found
- `E_LOG_NOT_FOUND`: log file does not exist
- `E_INVALID_ARGUMENT`: bad offset or limit

### GET /worktrees

query:
- `repo_id` (optional), `state` (default `present`), `limit`, `cursor`

response: `ListWorktreesData`

### GET /worktrees/{ref}

query:
- `repo_id` (optional)

response: `WorktreeDTO`

### GET /invocations

query:
- `repo_id`, `worktree_id`, `worktree_ref`, `state`, `mode`, `limit`, `cursor`

response: `ListInvocationsData`

### GET /invocations/{ref}

query:
- `repo_id` (optional)

response: `InvocationDTO`

### GET /invocations/{ref}/diff

query:
- `repo_id`, `include_patch`, `max_patch_bytes`, `include_uncommitted`

response: `InvocationDiffData`

### GET /invocations/{ref}/checkpoints

query:
- `repo_id`, `limit`, `cursor`

response: `ListCheckpointsData`

### POST /repos/register

request: `RegisterRepoRequest`
response: `RegisterRepoResponse`

### GET /repos

response: `ListReposData`

### GET /spec/v2.1/s1/release/readiness

query:
- `repo_id` (required)

response: `S1ReleaseReadinessData`
- `slice`, `slice_ready`, `gate_a` (`S1GateStatusData`), `gate_b` (`S1GateStatusData`)

`S1GateStatusData`: `gate_id`, `status`, `total_items`, `closed_items`, `blocking_items`

error codes:
- `E_GATE_BLOCKED` (409): slice not ready, gates are blocked
- `E_GATE_SET_INVALID` (400): gate source unreadable/malformed
- `E_REPO_NOT_FOUND` (404): repo_id not found

### GET /spec/v2.1/s1/release/closure-report

query:
- `repo_id` (required)

response: `S1ClosureReportData`
- `slice`, `gate_a` (`S1GateClosureData`), `gate_b` (`S1GateClosureData`)

`S1GateClosureData`: `gate_id`, `status`, `total_items`, `closed_items`, `blocking_items`, `closed_evidence`

`S1ClosedItemEvidence`: `issue_path`, `implemented_refs`, `targeted_tests`, `suite_tests`

error codes:
- `E_GATE_SET_INVALID` (400): gate source unreadable/malformed

### GET /spec/v2.1/s1/release/freeze-readiness

query:
- `repo_id` (required)

response: `S1FreezeReadinessData`
- `freeze_ready`, `unresolved_count`, `spec_path`, `first_question`

error codes:
- `E_GATE_BLOCKED` (409): freeze blocked by unresolved defaults
- `E_GATE_SET_INVALID` (400): spec source unreadable/malformed

## stubs

- complete error code catalog per endpoint
- http status code matrix
