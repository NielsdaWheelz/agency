# Agency S3 PR-02 Report — Preflight + Git Fetch/Ahead/Push + Report Gating

## Summary

This PR implements `agency push <run_id> [--force]` up through a successful git push, including all preflight checks, report gating, and git operations. This PR does NOT create/update GitHub PRs (deferred to PR-03).

### Changes Made

1. **Added `E_WORKTREE_MISSING` error code** to `internal/errors/errors.go`:
   - New error code for when run worktree path is missing on disk

2. **Created `internal/commands/push.go`** — full push command implementation:
   - `Push()` function implementing the complete preflight + git push flow
   - `PushOpts` struct with `RunID` and `Force` fields
   - `nonInteractiveEnv()` helper returning `GIT_TERMINAL_PROMPT=0`, `GH_PROMPT_DISABLED=1`, `CI=1`
   - `resolveRunForPush()` for run_id resolution with exact/prefix matching
   - `isReportEffectivelyEmpty()` for report gating (< 20 trimmed chars = empty)
   - `checkGhAuthForPush()` for gh installation and authentication check
   - `gitFetchOrigin()` for non-destructive fetch
   - `resolveParentRef()` for parent branch resolution (local preferred, then origin/)
   - `computeAhead()` for rev-list --count ahead computation
   - `gitPushBranch()` for `git push -u origin <branch>`
   - `appendPushEvent()` for events.jsonl logging
   - `computeReportHash()` for sha256 report hashing (used in PR-03)

3. **Created `internal/commands/push_test.go`** — comprehensive unit tests:
   - `TestIsReportEffectivelyEmpty` — 7 test cases for report gating logic
   - `TestPushOriginGating` — 7 test cases for origin host validation
   - `TestNonInteractiveEnv` — verifies required environment variables
   - `TestComputeReportHash` — hash computation tests
   - `TestPushErrorCodes` — compile-time error code verification
   - `TestResolveRunForPush_NotFound` — run not found error handling
   - `TestPushEventNames` — event name validation
   - `TestPushOptsDefaults` — options struct defaults
   - `TestPushForceDoesNotBypassEmptyDiff` — documentation test
   - `TestWorktreeMissingError` — new error code verification

4. **Updated `internal/cli/dispatch.go`**:
   - Added `push` to command list in usage text
   - Added `pushUsageText` constant with full help text
   - Added `push` case in switch statement
   - Added `runPush()` function with flag parsing

5. **Updated `README.md`**:
   - Marked PR-02 as complete in slice 3 progress
   - Updated next step to PR-03
   - Added comprehensive `agency push` command documentation

## Problems Encountered

1. **Duplicate `osEnv` type definition**: The `osEnv` type was already defined in `doctor.go`. Removed the duplicate from `push.go`.

2. **`checkGhAuth` function collision**: A `checkGhAuth` function already existed in `doctor.go`. Renamed the push-specific version to `checkGhAuthForPush` which includes non-interactive environment variables.

3. **`store.ScanAllRuns` signature**: The function takes a `dataDir` string, not a `*store.Store`. Fixed to use `st.DataDir`.

4. **Standard library `errors.As` vs internal `errors.As`**: The internal errors package doesn't expose `errors.As`. Added import alias `stderrors` for the standard library errors package.

## Solutions Implemented

1. **Removed duplicate type**: Deleted the redundant `osEnv` struct definition since it's package-scoped and already available.

2. **Renamed function**: Created `checkGhAuthForPush` as a separate function that sets non-interactive environment variables per the spec requirement.

3. **Fixed function call**: Changed `store.ScanAllRuns(st)` to `store.ScanAllRuns(st.DataDir)`.

4. **Added import alias**: Added `stderrors "errors"` import and used `stderrors.As()` for type assertions on lock errors and resolution errors.

## Decisions Made

1. **Preflight check ordering**: Followed the spec ordering exactly:
   1. Load metadata
   2. Check worktree exists
   3. Acquire repo lock
   4. Check origin exists
   5. Check origin is github.com
   6. Report gating
   7. Dirty worktree warning
   8. gh auth check

2. **Event emission timing**: Events are emitted at key points:
   - `push_started`: After metadata load, before lock acquisition
   - `push_failed`: On any failure after push_started
   - `git_fetch_finished`: After successful fetch
   - `git_push_finished`: After successful push
   - `push_finished`: On complete success

3. **Non-interactive enforcement**: All git/gh subprocesses use the non-interactive environment overlay, which is enforced in every `cr.Run()` call in push.go.

4. **Report hash computation**: Implemented `computeReportHash()` for future use in PR-03, even though it's not used in this PR.

5. **Error messages**: Included actionable hints in error messages (e.g., "run `gh auth login` first", "make at least one commit").

## Deviations from Spec

None. The implementation follows the spec exactly:
- All preflight checks implemented in specified order
- Report gating with 20-char threshold
- `--force` bypasses report gate but NOT `E_EMPTY_DIFF`
- Non-interactive environment enforced
- Events logged per spec
- `last_push_at` updated on success

## How to Run New/Changed Commands

### Running the Push Command

```bash
# Basic usage (requires existing run with commits)
agency push <run_id>

# With empty report (warns but proceeds)
agency push <run_id> --force

# Using unique prefix
agency push 20260110

# View help
agency push --help
```

### Expected Workflow

```bash
# 1. Create a run
agency run --title "my feature"

# 2. In the runner, make commits on the workspace branch
# (attach to runner, make changes, commit)

# 3. Ensure report is non-empty
# Edit <worktree>/.agency/report.md

# 4. Push the branch
agency push <run_id>
# Output: pushed agency/my-feature-a3f2 to origin
```

## How to Check New/Changed Functionality

### Run Tests

```bash
# Run all tests
go test ./... -count=1

# Run push-specific tests
go test ./internal/commands/... -v -run "Push"
go test ./internal/commands/... -v -run "Report"

# Check compilation
go build ./...
```

### Manual Verification

```bash
# 1. Verify push command appears in help
agency --help | grep push

# 2. Verify push help text
agency push --help

# 3. Test error cases:

# No run_id
agency push
# Expected: error_code: E_USAGE

# Non-existent run
agency push nonexistent-run
# Expected: error_code: E_RUN_NOT_FOUND

# Run with empty report (no --force)
# Create a run, then:
agency push <run_id>
# Expected: error_code: E_REPORT_INVALID

# Run with no commits
# Create a run, push without making commits:
agency push <run_id> --force
# Expected: error_code: E_EMPTY_DIFF
```

### Verify Events

After a push attempt, check `events.jsonl`:
```bash
cat "${AGENCY_DATA_DIR}/repos/<repo_id>/runs/<run_id>/events.jsonl"
# Should contain push_started, push_failed/push_finished, etc.
```

### Verify Metadata

After a successful push:
```bash
cat "${AGENCY_DATA_DIR}/repos/<repo_id>/runs/<run_id>/meta.json" | jq '.last_push_at'
# Should show RFC3339 timestamp
```

## Branch Name and Commit Message

**Branch name:** `pr3/s3-pr02-push-preflight-git-push`

**Commit message:**

```
feat(s3): implement agency push preflight + git fetch/ahead + git push

Implement `agency push <run_id> [--force]` up through successful git push:

Preflight checks (in order):
- resolve run_id and load metadata
- verify worktree exists on disk (E_WORKTREE_MISSING)
- acquire repo lock
- verify origin exists (E_NO_ORIGIN)
- verify origin is github.com (E_UNSUPPORTED_ORIGIN_HOST)
- report gating: missing/empty requires --force (E_REPORT_INVALID)
- warn if worktree has uncommitted changes
- verify gh auth status (E_GH_NOT_AUTHENTICATED)

Git operations:
- git fetch origin (non-destructive)
- resolve parent ref (local preferred, else origin/<parent>)
- compute ahead via rev-list --count
- refuse if ahead==0 (--force does NOT bypass E_EMPTY_DIFF)
- git push -u origin <branch> (no force push)

Non-interactive enforcement:
- all git/gh subprocesses set GIT_TERMINAL_PROMPT=0
- all git/gh subprocesses set GH_PROMPT_DISABLED=1
- all git/gh subprocesses set CI=1

Events logged to events.jsonl:
- push_started, git_fetch_finished, git_push_finished
- push_finished (success) or push_failed (with error_code + step)

Metadata persistence:
- update last_push_at on success

New error code:
- E_WORKTREE_MISSING: run worktree path missing on disk

PR creation/update deferred to PR-03.
```

## Files Modified

- `internal/errors/errors.go` — added `E_WORKTREE_MISSING` error code
- `internal/commands/push.go` — new file with push command implementation
- `internal/commands/push_test.go` — new file with unit tests
- `internal/cli/dispatch.go` — added push command routing and help text
- `README.md` — updated progress and added push command documentation
