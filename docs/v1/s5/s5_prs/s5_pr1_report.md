# S5 PR 5.1 Report — Verify Runner Core

## Summary of Changes

This PR implements the core verify execution engine for `agency verify`, including:

1. **New `internal/store/verify_record.go`**: Defines the `VerifyRecord` struct—the canonical evidence schema for verify runs. Contains all fields per the S5 spec: timestamps, duration, exit code, signal, timeout/cancel flags, ok derivation result, and summary.

2. **New `internal/verify/` package** with three core files:
   - `verifyjson.go`: Parses the optional `<worktree>/.agency/out/verify.json` structured output from verify scripts. Implements "valid enough" rules (requires `schema_version` and `ok`, tolerates missing `summary`/`data`).
   - `derive.go`: Pure functions `DeriveOK()` and `DeriveSummary()` implementing the locked precedence rules for determining verification outcome.
   - `runner.go`: The main verify execution engine that runs scripts via `sh -lc`, manages process groups, handles timeout/cancellation with SIGINT→SIGKILL escalation, captures logs, reads verify.json, and writes `verify_record.json` atomically.

3. **Path helpers in `internal/store/store.go`**:
   - `VerifyRecordPath()`: Returns path to `verify_record.json`
   - `EventsPath()`: Returns path to `events.jsonl` (added for future use)

4. **Unit tests**:
   - `derive_test.go`: Table-driven tests for `DeriveOK()` precedence (11 cases) and `DeriveSummary()` rules (10 cases)
   - `verifyjson_test.go`: Tests for verify.json parsing including missing file, invalid JSON, missing/empty schema_version, valid minimal/full, and extra fields handling (11 cases)

## Problems Encountered

1. **Process group signal handling**: Go's `exec.CommandContext` only kills the main process on context cancellation, not child processes. Had to use `SysProcAttr{Setpgid: true}` to start the script in its own process group, then signal the negative pgid to kill the entire group.

2. **Exit code vs signal distinction**: When a process is terminated by a signal, the exit code is not meaningful. Had to carefully check `WaitStatus.Signaled()` to determine whether to record an exit code or a signal name.

3. **Verify.json validation semantics**: The spec says "valid enough" but needed to nail down exactly what that means. Decided: `schema_version` required and non-empty, `ok` implicitly required (Go unmarshals to false by default), `summary` and `data` optional.

## Solutions Implemented

1. **Process group management**: The runner creates scripts in their own process group (`Setpgid: true`) and kills via `syscall.Kill(-pgid, signal)` to target the entire group. Uses a 3-second grace period between SIGINT and SIGKILL.

2. **Clean exit code handling**: On signal termination, `ExitCode` is set to `nil` and `Signal` records the signal name (typically "SIGKILL" for timeout/cancel). The `DeriveOK()` function treats `nil` exit code as failure.

3. **Atomic writes with best-effort**: Uses `fs.WriteJSONAtomic()` for `verify_record.json`. Added `writeRecordBestEffort()` for cases where we want to record state before returning an error.

## Decisions Made

1. **Log file truncation**: Each verify invocation truncates (overwrites) `verify.log` rather than appending. This matches the setup.log pattern and avoids unbounded growth.

2. **Log header format**: Matches the existing setup.log style with timestamp, command, and cwd for consistency.

3. **Verify.json error handling**: Parse/validation errors are recorded in `VerifyRecord.Error` but only if no other internal error (exec/log) already occurred. This prevents overwriting more critical error information.

4. **Summary precedence**: `verify.json.summary` always wins if present and non-empty, even on failure. This lets scripts provide richer context than generic messages.

5. **ReadVerifyJSONResult struct**: Chose to use a result struct rather than multiple return values to make the exists/valid/error states clearer to callers.

## Deviations from Prompt/Spec

1. **Added `EventsPath()` helper**: The spec only asked for `VerifyRecordPath()`, but I added `EventsPath()` since it will be needed in PR 5.2 and follows the same pattern.

2. **Result struct for ReadVerifyJSON**: The spec suggested `(vj *VerifyJSON, exists bool, err error)` but I used a struct `ReadVerifyJSONResult` for clearer semantics. The behavior is identical.

3. **Best-effort record writing**: Added `writeRecordBestEffort()` to attempt recording state before returning errors. This provides better debugging capability when things go wrong early in execution.

## How to Run New/Changed Commands

### Run Unit Tests

```bash
# Run verify package tests only
go test ./internal/verify/... -v

# Run all tests
go test ./...
```

### Expected Test Output

```
=== RUN   TestDeriveOK_Precedence
--- PASS: TestDeriveOK_Precedence (0.00s)
=== RUN   TestDeriveSummary
--- PASS: TestDeriveSummary (0.00s)
=== RUN   TestReadVerifyJSON_MissingFile
--- PASS: TestReadVerifyJSON_MissingFile (0.00s)
... (all 22 tests pass)
```

### Manual Smoke Test (Optional)

No CLI command is added in this PR. To manually test the runner:

```go
// In a scratch test file (do not commit):
package main

import (
    "context"
    "fmt"
    "os"
    "github.com/NielsdaWheelz/agency/internal/verify"
)

func main() {
    tmpDir, _ := os.MkdirTemp("", "verify-test")
    defer os.RemoveAll(tmpDir)
    
    cfg := verify.RunConfig{
        RepoID:         "test-repo-id",
        RunID:          "test-run-id",
        WorkDir:        tmpDir,
        Script:         "echo 'hello verify'",
        Env:            os.Environ(),
        LogPath:        tmpDir + "/verify.log",
        VerifyJSONPath: tmpDir + "/.agency/out/verify.json",
        RecordPath:     tmpDir + "/verify_record.json",
    }
    
    record, err := verify.Run(context.Background(), cfg)
    if err != nil {
        fmt.Printf("error: %v\n", err)
        return
    }
    fmt.Printf("ok=%v summary=%q\n", record.OK, record.Summary)
}
```

## How to Check New/Changed Functionality

1. **Verify precedence rules**: The unit tests in `derive_test.go` exhaustively test all precedence combinations. Run `go test ./internal/verify/... -run TestDeriveOK_Precedence -v` to see each case.

2. **Verify.json parsing**: The tests in `verifyjson_test.go` cover all validation scenarios. Run `go test ./internal/verify/... -run TestReadVerifyJSON -v` to see each case.

3. **Code coverage** (optional):
   ```bash
   go test ./internal/verify/... -cover
   # Expected: ~70-80% coverage (runner.go is not unit tested per spec)
   ```

## Branch Name and Commit Message

**Branch name:** `pr5/s5-pr1-verify-runner-core`

**Commit message:**

```
feat(verify): implement verify runner core (S5 PR1)

Add the core verify execution engine and canonical evidence recording
for the upcoming `agency verify` command. This PR is plumbing only—no
CLI command is added yet.

New files:
- internal/store/verify_record.go: VerifyRecord schema (public contract)
- internal/verify/verifyjson.go: Parse optional .agency/out/verify.json
- internal/verify/derive.go: DeriveOK + DeriveSummary pure functions
- internal/verify/runner.go: Subprocess runner with timeout/cancel handling
- internal/verify/derive_test.go: Table-driven tests for precedence rules
- internal/verify/verifyjson_test.go: Tests for verify.json parsing

Modified files:
- internal/store/store.go: Add VerifyRecordPath() and EventsPath() helpers
- README.md: Update status for S5, add verify package to project structure

Key behaviors implemented:
- Scripts run via `sh -lc <script>` in their own process group
- Timeout: SIGINT to process group, wait 3s, SIGKILL to process group
- Cancellation: same escalation pattern on parent context cancel
- ok derivation precedence: timeout/cancel > nil exit > non-zero exit >
  verify.json.ok > true
- Summary: prefer verify.json.summary, else generic message
- verify_record.json written atomically via temp+rename

Per spec, this PR does NOT:
- Add CLI command (PR 5.3)
- Update meta.json (PR 5.2)
- Append to events.jsonl (PR 5.2)
- Acquire repo locks (PR 5.2)
- Touch tmux code paths

Refs: docs/v1/s5/s5_prs/s5_pr1.md
```
