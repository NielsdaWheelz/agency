# daemon api contract

this file defines the daemon http api. it is normative.

## transport

- unix socket: `${AGENCY_DATA_DIR}/agencyd.sock`
- protocol: http/1.1 over unix domain socket
- all requests and responses are json

## versioning

- `api_version` is required on control-plane responses.
- any breaking change must bump `api_version` and update this doc.

## common error shape

errors return `ok=false` and must include:
- `error_code` (string)
- `message` (string)
- `hint` (string, optional)

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
- `ok`, `invocation_id`, `sandbox_path`, `repo_id`, `integration_worktree_id`, `integration_worktree_name`, `pid`, `pgid`, `daemon_instance_id`, `already_running`, `log_paths`, `api_version`, `build_version`, `client_request_id`

### POST /invocations/start_headed (control plane)

request: `ControlPlaneStartHeadedRequest`
- `repo_root`, `worktree_ref`, `runner`, `client_request_id`
- optional: `invocation_name`, `runner_args`, `env`, `no_include_untracked`

response: `ControlPlaneStartHeadedResponse`
- `ok`, `invocation_id`, `sandbox_path`, `repo_id`, `integration_worktree_id`, `integration_worktree_name`, `tmux_session`, `daemon_instance_id`, `already_running`, `api_version`, `build_version`, `git_sha`, `client_request_id`

### POST /invocations/{id}/start_headless (legacy)

deprecated. no new features. remove in v2.
request: `StartHeadlessRequest`
response: `StartHeadlessResponse`

### POST /invocations/{id}/stop

query:
- `repo_id` (required)

response: `StopResponse`

### POST /invocations/{id}/kill

query:
- `repo_id` (required)

response: `KillResponse`

### POST /invocations/{id}/checkpoints/apply

query:
- `repo_id` (required)

request: `CheckpointApplyRequest`
response: `CheckpointApplyResponse`

### POST /invocations/{id}/land

query:
- `repo_id` (required)

request: `LandRequest`
response: `LandResponse`

### POST /invocations/{id}/discard

query:
- `repo_id` (required)

request: `DiscardRequest`
response: `DiscardResponse`

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

## stubs

- complete error code catalog per endpoint
- http status code matrix
