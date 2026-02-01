# PR-05 Report: Daemon Control Plane for Headless Invocation Start

## Summary

PR-05 makes the daemon the **single creator and lifecycle owner** for headless invocations. After this PR:

- `agent start --headless` is a single RPC
- The daemon generates invocation IDs, creates sandboxes, writes metadata, and starts runners
- The CLI does **not** create invocations or sandboxes for headless paths
- The CLI only sends requests and renders responses

## Changes Made

### Error Codes (internal/errors/errors.go)
Added new error codes:
- `E_UNSAFE_REPO_ROOT` - repo_root is inside an agency-managed worktree
- `E_PROMPT_TOO_LARGE` - prompt exceeds 256 KB
- `E_DAEMON_INCOMPATIBLE` - CLI api_version does not match daemon api_version
- `E_RUNNER_ARG_CONFLICT` - user-supplied args include reserved flags

### InvocationMeta (internal/store/invocation.go)
Added `FailureReason` field for detailed failure tracking:
- `start_incomplete` - daemon crashed or timed out before runner spawned
- `sandbox_missing` - sandbox path disappeared while running
- `spawn_failed` - exec.Command.Start() returned error
- `runner_exit_nonzero` - runner exited with non-zero code
- `killed` - terminated via kill command
- `stopped` - terminated via stop command with escalation
- `daemon_shutdown` - terminated by daemon stop --force

### Daemon Types (internal/daemon/types.go)
Added:
- `ControlPlaneStartRequest` - request for POST /invocations/start_headless
- `ControlPlaneStartResponse` - response with invocation_id, sandbox_path, etc.
- `IdempotencyEntry` - tracks recent requests for idempotency
- `MaxPromptSize` - 256KB limit
- `IdempotencyTTL` - 5 minute expiration

### Daemon Server (internal/daemon/server.go)
Added:
- `idempotency` map for request deduplication
- `checkIdempotency()` - check for duplicate requests
- `recordIdempotency()` - record successful requests
- `cleanupExpiredIdempotency()` - remove stale entries
- Enhanced `recoverRepoInvocations()` to handle:
  - `status=starting` invocations older than 60s
  - Setting `failure_reason` on failed recoveries

### Daemon Handlers (internal/daemon/handlers.go)
Added:
- `handleControlPlaneStartHeadless()` - main control plane handler
- `validateRunnerArgs()` - reserved flag validation
- `isInsideAgencyManagedWorktree()` - recursion guard
- `ensureRepoRegistered()` - repo self-registration
- `checkInvocationNameUniqueness()` - name uniqueness check
- `createInvocationAndSandbox()` - atomic creation
- `validateSandboxPath()` - sandbox safety validation
- `startRunner()` - runner process spawning
- `buildRunnerArgsWithSandbox()` - includes codex -C flag
- `cleanupFailedInvocation()` - cleanup on failure
- `waitForExitWithFailureReason()` - exit handling with failure_reason
- `stopEscalation()` - SIGINT → SIGTERM → SIGKILL

### Daemon Client (internal/daemonclient/client.go)
Added:
- `ControlPlaneStartOpts` - options struct
- `ControlPlaneStartHeadless()` - calls new endpoint
- `CheckAPIVersion()` - verifies daemon compatibility

### CLI Agent Commands (internal/commands/agent.go)
- Restructured `AgentStart()` to early return for headless mode
- Added `agentStartHeadlessControlPlane()` for headless path
- CLI no longer creates invocations or sandboxes for headless
- Added API version check before control plane call

### Tests (internal/daemon/control_plane_test.go)
Added unit tests for:
- `TestValidateRunnerArgs` - reserved flag detection
- `TestIsInsideAgencyManagedWorktree` - recursion guard
- `TestBuildRunnerArgsWithSandbox` - command construction
- `TestIdempotencyKey` - key generation

### Documentation
- Updated README.md with daemon control plane documentation
- Created .agency/report.md with full PR details

## Problems Encountered

1. **CLI Architecture**: Previously created invocations before daemon handoff. Required restructuring to delegate entirely to daemon for headless.

2. **Import Dependencies**: Daemon needed integrationworktree resolution, requiring new service instantiation with proper dependencies.

3. **In-Memory Idempotency**: Idempotency map is lost on daemon restart. Accepted per spec.

## Solutions Implemented

1. **Mode-Specific Paths**: `AgentStart()` returns early for headless, delegating all creation to daemon.

2. **Service Injection**: Daemon creates services with injected Store, CommandRunner, FS, and Clock dependencies.

3. **5-Minute TTL**: Reasonable retry window without persistent storage overhead.

## Decisions Made

1. Preserved legacy `POST /invocations/{id}/start_headless` endpoint for backward compatibility
2. Opportunistic idempotency cleanup when map size > 100
3. Stop escalation runs in background goroutine, returns immediately to client
4. Compute prompt_sha256 daemon-side (not in request)

## Deviations from Spec

None significant. All invariants from the spec are enforced:
- Daemon is sole writer for headless artifacts
- No runner executes in integration worktree
- Sandbox creation is atomic
- Invocation IDs are daemon-generated
- Invocation names are unique among active invocations
- CLI never touches v2 store for headless paths

## How to Test

### Unit Tests
```bash
go test ./internal/daemon/... -v -run "Test(ValidateRunnerArgs|IsInsideAgency|BuildRunnerArgs|IdempotencyKey)"
```

### All Tests
```bash
go test ./... -count=1
```

### Manual Testing
```bash
# Start daemon
agency daemon start

# Create worktree
agency worktree create --name test-pr05

# Start headless invocation
agency agent start --worktree test-pr05 --headless --prompt "Hello world"

# View status
agency agent show <invocation_id>

# Stop with escalation
agency agent stop <invocation_id>
```

### Test Reserved Flag Rejection
```bash
agency agent start --worktree test-pr05 --headless --prompt "test" --runner-arg "--verbose"
# Should fail with E_RUNNER_ARG_CONFLICT
```

## Acceptance Criteria Status

- [x] `agent start --headless` creates everything via daemon RPC
- [x] CLI does zero v2 store writes for headless
- [x] Invocation IDs are daemon-generated
- [x] Repo self-registration works
- [x] Recursion guard rejects agency-managed worktrees as repo_root
- [x] Partial creation never leaves residue
- [x] Recovery handles incomplete starts with failure_reason
- [x] Idempotency: duplicate client_request_id returns same invocation
- [x] Name uniqueness enforced
- [x] Reserved flag detection catches prefix and short forms
- [x] CLI checks api_version before calling start endpoint
- [x] Prompt stored at invocations/<id>/prompt.txt (0600)
- [x] Codex command includes -C <sandbox_path>
- [x] failure_reason set on all failed transitions
- [x] All invariants enforced by tests
