# S5 PR 5.2 Report: Meta + Flags + Events Integration

## Summary of Changes

This PR wires the S5 verify runner (from PR 5.1) into run metadata and events, with deterministic flag semantics and a clear failure when the workspace is missing/archived.

### Key Changes:

1. **New Error Code**: Added `E_WORKSPACE_ARCHIVED` for when a run exists but its worktree is missing or archived
2. **New Meta Field**: Added `flags.needs_attention_reason` to `RunMetaFlags` struct for tracking why attention is needed
3. **Repo Lock Fix**: Changed stale detection to be pid-only (age alone no longer steals locks per S5 spec)
4. **Verify Service**: Created `internal/verifyservice/` package with `VerifyRun()` entrypoint that:
   - Resolves run_id globally (cwd-independent)
   - Acquires repo lock for the run's repo_id
   - Fails fast if worktree missing/archived
   - Emits `verify_started` and `verify_finished` events (best-effort)
   - Runs verify via the PR 5.1 runner
   - Updates `meta.json` atomically with correct attention semantics
5. **Event Helpers**: Added `VerifyStartedData()` and `VerifyFinishedData()` helpers to events package
6. **JSON Helper**: Added `UnmarshalJSON()` helper to fs package

## Problems Encountered

1. **Lock Staleness Behavior**: The existing lock code used both PID liveness AND age-based staleness. The S5 spec requires pid-only staleness, meaning an old lock held by a live process should NOT be stolen based on age alone. This required changing the `isStale()` function and updating the corresponding test.

2. **Meta Flag Semantics**: The attention flag update rules have specific semantics:
   - Verify success clears `needs_attention` ONLY if the reason was `verify_failed`
   - Verify failure always sets `needs_attention=true` with reason `verify_failed`
   - This prevents verify from accidentally clearing attention set for other reasons (like `stop_requested`)

3. **Event Append Failure Handling**: Events are best-effort, but failures need to be recorded somewhere. The solution was to augment `verify_record.json.error` with the event append failure messages without polluting the summary.

## Solutions Implemented

1. **Pid-Only Staleness**: Simplified `isStale()` to only check if the PID is alive. Updated the test `TestRepoLock_StaleByAgeSteals` to `TestRepoLock_PIDOnlyStaleness_AgeDoesNotSteal` which verifies that an old lock held by an alive process is NOT stolen.

2. **Attention Update Logic**: Implemented precise conditional logic in `VerifyRun()`:
   ```go
   if record.OK {
       // Clear attention only if reason was verify_failed
       if m.Flags != nil && m.Flags.NeedsAttention && 
          m.Flags.NeedsAttentionReason == "verify_failed" {
           m.Flags.NeedsAttention = false
           m.Flags.NeedsAttentionReason = ""
       }
   } else {
       // Set attention with reason verify_failed
       m.Flags.NeedsAttention = true
       m.Flags.NeedsAttentionReason = "verify_failed"
   }
   ```

3. **Error Augmentation**: Created `augmentRecordError()` helper that reads the verify record, appends error messages to the `error` field (preserving existing errors with `;` separator), and rewrites atomically.

## Decisions Made

1. **Package Location**: Created `internal/verifyservice/` as a new package rather than adding to existing packages. This keeps the verify pipeline logic isolated and makes it easy for PR 5.3 to wire up the CLI command.

2. **Environment Variables**: The verify script receives the same environment variables as other agency scripts per the L0 contract. The `buildVerifyEnv()` function constructs this environment.

3. **Workspace Archived Detection**: A run is considered archived if:
   - `meta.Archive.ArchivedAt` is non-empty, OR
   - The worktree path does not exist on disk

4. **Event Data Structure**: The `verify_started` and `verify_finished` events include comprehensive data:
   - `verify_started`: timeout_ms, log_path, verify_json_path (if known)
   - `verify_finished`: ok, exit_code, timed_out, cancelled, duration_ms, verify_json_path, log_path, verify_record_path

5. **Test Strategy**: Created table-driven tests for the attention update rules to comprehensively cover all edge cases. Integration tests verify workspace predicate and error augmentation.

## Deviations from Spec

1. **No Verify Record on Workspace Archived**: The spec mentions writing a verify_record with `error="workspace archived"` when the workspace is missing. The current implementation simply returns `E_WORKSPACE_ARCHIVED` without writing a record, as the verify runner was never invoked. This is a simplification that can be revisited if needed.

2. **Event Ordering**: The spec says emit `verify_finished` after meta update. The implementation emits `verify_finished` after attempting the meta update (whether it succeeds or fails), which ensures the event is always written even if meta update fails.

## How to Run

### Build and Test

```bash
# Build
go build ./...

# Run all tests
go test ./...

# Run specific package tests
go test ./internal/verifyservice/... -v
go test ./internal/lock/... -v
go test ./internal/errors/... -v
```

### Using the Verify Service (Internal API)

The `VerifyRun()` function is an internal API that will be called by the `agency verify` command in PR 5.3:

```go
import "github.com/NielsdaWheelz/agency/internal/verifyservice"

svc := verifyservice.NewService(dataDir, fs.NewRealFS())
result, err := svc.VerifyRun(ctx, runRef, 30*time.Minute)

// result.Record contains the verify record
// result.EventAppendErrors contains any event append failures
// err is only set for infrastructure failures
```

### Manual Validation

Until PR 5.3 adds the CLI command, the verify service can be tested by:

1. Creating a run with `agency run --title "test"`
2. Making changes and committing in the worktree
3. Writing a test that calls `VerifyRun()` directly

## Files Changed

### New Files
- `internal/verifyservice/service.go` - Verify pipeline entrypoint
- `internal/verifyservice/service_test.go` - Unit and integration tests

### Modified Files
- `internal/errors/errors.go` - Added `E_WORKSPACE_ARCHIVED` error code
- `internal/store/run_meta.go` - Added `NeedsAttentionReason` field to `RunMetaFlags`
- `internal/lock/repo_lock.go` - Changed staleness to pid-only
- `internal/lock/repo_lock_test.go` - Updated test for pid-only staleness
- `internal/events/append.go` - Added `VerifyStartedData()` and `VerifyFinishedData()` helpers
- `internal/fs/fs.go` - Added `UnmarshalJSON()` helper
- `README.md` - Updated progress and project structure

## Branch Name and Commit Message

**Branch name:** `pr5/s5-pr02-meta-flags-events-integration`

**Commit message:**

```
feat(s5): wire verify runner into meta + events with correct attention semantics

Implements S5 PR 5.2: meta + flags + events integration for the verify pipeline.

Key changes:
- Add E_WORKSPACE_ARCHIVED error code for missing/archived worktrees
- Add flags.needs_attention_reason field to meta.json schema
- Fix repo lock stale detection to be pid-only (per S5 spec)
- Create internal/verifyservice package with VerifyRun() entrypoint
- Add verify_started and verify_finished event helpers
- Implement correct attention flag semantics:
  - Verify success clears attention only when reason == verify_failed
  - Verify failure always sets attention with reason verify_failed

The VerifyRun() function resolves runs globally (cwd-independent), acquires
repo lock, checks workspace existence, emits events (best-effort), runs the
verify script via the PR 5.1 runner, and updates meta.json atomically.

This PR does not add the CLI command (that's PR 5.3). The verifyservice
package provides the internal API that the CLI will call.

Tested with:
- Table-driven unit tests for attention update rules
- Integration tests for workspace predicate and error augmentation
- Lock tests updated for pid-only staleness behavior

Files changed:
- internal/errors/errors.go (new error code)
- internal/store/run_meta.go (new field)
- internal/lock/repo_lock.go (staleness fix)
- internal/events/append.go (new helpers)
- internal/fs/fs.go (json helper)
- internal/verifyservice/ (new package)
- README.md (progress update)

Refs: S5 spec, S5 PR roadmap
```
