# PR-12 Implementation Report: Daemon Read API + CLI Read-Through-Daemon

## Status: Complete

## Implementation Summary

PR-12 establishes the daemon as the sole read and derive authority for v2 flows, eliminating all CLI-side filesystem scanning, resolution, reconciliation, and status derivation.

## Components Implemented

### 1. Read API Types (`internal/daemon/read_types.go`)

- **APIResponse envelope**: Standard response wrapper with `ok`, `api_version`, `build_version`, `git_sha`, `request_id`, and typed data/error fields
- **WorktreeDTO**: Full worktree representation with `worktree_id`, `name`, `repo_id`, `branch`, `parent_branch`, `tree_path`, `state`, `created_at`, `last_used_at`
- **InvocationDTO**: Full invocation representation with derived fields (`display_status`, `attention_flags`, `sort_key`) in addition to raw lifecycle fields
- **CheckpointDTO**: Checkpoint summary with `id`, `created_at`, `diffstat`, `snapshot_commit`, `includes_untracked`, `degraded`
- **DiffRange/InvocationDiffData**: Structured diff response with commit list, diffstat, optional patches
- **InvocationLogsData**: Log content with truncation metadata (`starts_midline`, `ends_midline`, `total_bytes`)
- **Pagination cursors**: Base64-encoded cursor structures for each list type

### 2. Status Derivation (`internal/daemon/status_derive.go`)

**Precedence rules implemented:**
1. lifecycle == failed → "failed"
2. landing_status == landed → "landed"
3. landing_status == discarded → "discarded"
4. needs_attention flag → "needs attention"
5. semantic == needs_input → "needs input"
6. semantic == blocked → "blocked"
7. semantic == ready_for_review → "ready for review"
8. running + semantic working → "working"
9. running → "running"
10. finished → "finished"
11. starting → "starting"

**Attention flags:**
- `needs_attention`: Set when invocation flag is true
- `stalled`: No output for >5 minutes while running
- `orphaned`: Invocation flag indicates orphaned state
- `landable`: Finished but not yet landed/discarded

### 3. Read Handlers (`internal/daemon/read_handlers.go`)

| Endpoint | Description |
|----------|-------------|
| `GET /worktrees` | List with `repo_id`, `state` filter, pagination |
| `GET /worktrees/{ref}` | Resolve by name/id/prefix |
| `GET /invocations` | List with `repo_id`, `worktree_ref`, `state`, `mode` filters, pagination |
| `GET /invocations/{ref}` | Resolve by name/id/prefix |
| `GET /invocations/{ref}/diff` | Structured diff with commits, patches |
| `GET /invocations/{ref}/logs` | Tail-based log retrieval |
| `GET /invocations/{ref}/checkpoints` | Checkpoint list |

### 4. Daemon Client (`internal/daemonclient/client.go`)

New methods:
- `ListWorktrees()`, `GetWorktree()`
- `ListInvocations()`, `GetInvocation()`
- `GetInvocationDiff()`, `GetInvocationLogs()`
- `ListCheckpoints()`

### 5. CLI Migration

**Migrated commands:**
- `worktree ls` → `ListWorktrees`
- `worktree show` → `GetWorktree`
- `worktree path` → `GetWorktree`
- `agent ls` → `ListInvocations`
- `agent show` → `GetInvocation`
- `agent diff` → `GetInvocationDiff`
- `checkpoint ls` → `ListCheckpoints`

## Architectural Invariants Satisfied

1. ✅ CLI never reads v2 store files directly
2. ✅ CLI never derives status (daemon returns `display_status`)
3. ✅ Daemon resolves refs (CLI passes name/id/prefix verbatim)
4. ✅ All daemon responses are versioned and enveloped
5. ✅ Read paths are side-effect free

## API Version

- `api_version: 1` (unchanged)
- Request ID support via `X-Request-ID` header

## Testing

All existing tests pass. Comprehensive test suite added covering all 3 tiers.

### New Test Files

**`internal/daemon/status_derive_test.go`** (4 tests):
- `TestDeriveDisplayStatus_Precedence` — 13-row table test covering all precedence rules
- `TestDeriveDisplayStatus_AttentionFlags` — 10-row table test covering all attention flag combinations
- `TestInvocationMetaToDTO` — full field mapping verification
- `TestWorktreeMetaToDTO` — full field mapping verification

**`internal/daemon/read_handlers_test.go`** (38 tests):

Tier 1 (core handlers):
- Worktree list/get: happy path, state filter, empty result, by name, not found
- Invocation list/get: happy path, state filter, mode filter, not found
- Checkpoint list: happy path, empty
- Response envelope: request ID (custom + generated), error format

Tier 2 (filters, pagination, logs, diff):
- Filter helpers: `matchesWorktreeState`, `matchesInvocationState`, `matchesInvocationMode`
- Pagination: worktrees, invocations, checkpoints (empty, within limit, exceeds limit, cursor continuation)
- Log reading: full file, truncated, ends midline/newline
- Log handler: happy path, missing file, kind param (raw/stderr/stream)
- `extractDiffstat`: summary line, multi-line, empty
- Parameter parsing: list worktrees, list invocations, get diff, get logs
- Diff integration test: real git repo with commits, verifies structured diff response

Tier 3 (pagination through handlers, routing):
- Worktree ref filter on invocations
- Handler-level pagination: checkpoints, worktrees, invocations
- Method not allowed routing
- Checkpoint sub-action routing

### Production Bug Fixes Found by Tests

1. **Pagination cursor overlap**: All 3 `paginate*` functions used inclusive cursor boundaries (`>=`/`<=`), causing the last item of page N to also appear as the first item of page N+1. Fixed to exclusive (`>`/`<`).

2. **CheckpointLS daemon auto-start crash**: `CheckpointLS` tests triggered `daemonclient.EnsureDaemonRunning()` which calls `os.Executable()` and spawns the test binary as a daemon process. Fixed by adding `DaemonSocketOverride` to `CheckpointLSOpts` for test injection.

3. **Dead code**: Removed unused `EncodeCursor`/`DecodeCursor` from `read_types.go` and unused `var _ = bytes.Buffer{}` from `read_handlers.go`.

### Modified Test Files

- `internal/commands/checkpoint_test.go` — Added `startTestDaemonForCheckpoint` helper; updated all 5 `TestCheckpointLS_*` tests to use `DaemonSocketOverride`; fixed assertions for PR-12 behavior (SnapshotCommit, mode restrictions, JSON format)

## Known Limitations

1. `agent attach` and `agent open` still resolve locally to get sandbox paths
2. No dedicated `agent logs` CLI command (daemon endpoint exists)

## Files Changed

| File | Change |
|------|--------|
| `internal/daemon/read_types.go` | New - API types; dead code removed |
| `internal/daemon/status_derive.go` | New - Status derivation |
| `internal/daemon/status_derive_test.go` | New - 4 tests |
| `internal/daemon/read_handlers.go` | New - HTTP handlers; pagination bug fixes; dead code removed |
| `internal/daemon/read_handlers_test.go` | New - 38 tests |
| `internal/daemon/server.go` | Modified - Route registration |
| `internal/daemonclient/client.go` | Modified - Client methods |
| `internal/commands/agent.go` | Modified - Daemon migration |
| `internal/commands/worktree.go` | Modified - Daemon migration |
| `internal/commands/checkpoint.go` | Modified - Daemon migration; `DaemonSocketOverride` |
| `internal/commands/checkpoint_test.go` | Modified - Test daemon helper; fixed assertions |
| `internal/daemon/worktree_handlers_test.go` | Modified - Test fix |
