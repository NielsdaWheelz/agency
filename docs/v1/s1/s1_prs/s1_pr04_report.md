# S1 PR-04 Report: Run Pipeline Orchestration (Internal API)

## Summary of Changes

This PR introduces the run pipeline orchestrator for slice 1. The pipeline provides a stable internal integration seam that executes 6 fixed steps in order, short-circuits on first error, and preserves `AgencyError` codes—without implementing the actual step logic (worktrees, setup execution, tmux) in this PR.

### Files Created

1. **`internal/pipeline/pipeline.go`**
   - `RunService` interface with 6 step methods
   - `Pipeline` struct with `svc RunService` and `nowFunc` for time injection
   - `NewPipeline(svc RunService) *Pipeline` constructor
   - `(*Pipeline).Run(ctx, opts) (runID string, err error)` - executes steps in fixed order
   - `RunPipelineOpts` struct with `Title`, `Runner`, `Parent`, `Attach` fields
   - `PipelineState` struct to accumulate state during execution
   - `Warning` struct for non-fatal warnings
   - Step name constants: `StepCheckRepoSafe`, `StepLoadAgencyConfig`, `StepCreateWorktree`, `StepWriteMeta`, `StepRunSetup`, `StepStartTmux`
   - `wrapStepError()` internal function for error wrapping

2. **`internal/pipeline/pipeline_test.go`**
   - 8 unit tests with mock `RunService` implementations
   - `mockRunService` - configurable mock for success/failure scenarios
   - `stateCapturingMock` - captures state for runID timing verification
   - `optCapturingMock` - captures state for opts verification

3. **`README.md`** (modified)
   - Updated slice 1 progress to mark PR-04 complete
   - Added pipeline package to project structure

---

## Problems Encountered

### 1. Initial Over-Engineering

First implementation used a generic step injection pattern (`[]namedStep` with `pipelineStep` function type). This was over-abstracted for a pipeline with exactly 6 fixed steps.

**Resolution:** Refactored to use a `RunService` interface with explicit method names. This is cleaner, more idiomatic Go, and provides a clear contract.

### 2. Run ID Generation Timing

The spec requires run_id to be generated immediately and returned even on error. This means run_id must be available before any steps execute.

**Resolution:** `Run()` generates run_id at the very start, stores it in `PipelineState.RunID`, and returns it in all cases (success or error). Tests verify this with `TestRunIDGeneratedBeforeSteps`.

### 3. Error Wrapping vs Preservation

The pipeline must:
- Preserve `*AgencyError` exactly (no re-wrapping)
- Wrap non-`AgencyError` errors into `E_INTERNAL` with step name in details

**Resolution:** `wrapStepError()` checks if the error is already an `AgencyError` using `errors.AsAgencyError()`. If so, it returns unchanged. Otherwise, wraps with `errors.WrapWithDetails()`.

---

## Solutions Implemented

### 1. RunService Interface

```go
type RunService interface {
    CheckRepoSafe(ctx context.Context, st *PipelineState) error
    LoadAgencyConfig(ctx context.Context, st *PipelineState) error
    CreateWorktree(ctx context.Context, st *PipelineState) error
    WriteMeta(ctx context.Context, st *PipelineState) error
    RunSetup(ctx context.Context, st *PipelineState) error
    StartTmux(ctx context.Context, st *PipelineState) error
}
```

This provides:
- Explicit contract for step implementations
- Easy mocking for tests
- Clear documentation of what each step does
- No generic machinery needed

### 2. Fixed Step Order in Run()

```go
func (p *Pipeline) Run(ctx context.Context, opts RunPipelineOpts) (string, error) {
    // ... setup ...
    if err := p.svc.CheckRepoSafe(ctx, st); err != nil {
        return st.RunID, wrapStepError(err, StepCheckRepoSafe)
    }
    if err := p.svc.LoadAgencyConfig(ctx, st); err != nil {
        return st.RunID, wrapStepError(err, StepLoadAgencyConfig)
    }
    // ... etc for all 6 steps ...
}
```

Direct method calls instead of loop iteration makes the order explicit and debuggable.

### 3. Mock-Based Testing

```go
type mockRunService struct {
    checkRepoSafeErr    error
    loadAgencyConfigErr error
    // ...
    called []string
}
```

Tests configure which step should fail and verify:
- Correct error code returned
- Short-circuit behavior (later steps not called)
- runID still returned on error

---

## Decisions Made

1. **Interface over function injection**: Used `RunService` interface instead of generic `[]namedStep`. More idiomatic, clearer contract, easier to understand.

2. **Exported `PipelineState`**: Made state struct exported so step implementations can access and populate it.

3. **Time injection via `SetNowFunc()`**: Added for deterministic testing. Tests use a fixed time to ensure predictable run_id format.

4. **Step name constants**: Defined constants for the 6 step names. Used in error wrapping and test assertions.

5. **Multiple mock types in tests**: Created specialized mocks (`stateCapturingMock`, `optCapturingMock`) for specific test scenarios rather than one complex mock.

---

## Deviations from Spec

1. **`SetNowFunc()` added**: The spec doesn't mention time injection, but it's necessary for deterministic testing.

2. **Additional tests**: Wrote 8 tests instead of the 3 required. Extra tests cover step order verification, middle-step failures, and opts passing.

---

## How to Run Commands

### Build

```bash
go build -o agency ./cmd/agency
```

### Test

```bash
# All tests
go test ./...

# Pipeline package tests only
go test ./internal/pipeline/... -v

# Specific test
go test ./internal/pipeline/... -v -run "TestShortCircuitPreservesErrorCode"
```

---

## How to Check New Functionality

### 1. Verify Short-Circuit Behavior

```bash
go test ./internal/pipeline/... -v -run "TestShortCircuitPreservesErrorCode"
```

Verifies:
- First step returns `E_PARENT_DIRTY`
- Subsequent steps are NOT called
- Error code is preserved exactly
- runID is still returned

### 2. Verify Stub Step Returns E_NOT_IMPLEMENTED

```bash
go test ./internal/pipeline/... -v -run "TestReachesThirdStepReturnsNotImplemented"
```

Verifies:
- First two steps succeed
- Third step returns `E_NOT_IMPLEMENTED`
- Details contain `{"step": "CreateWorktree"}`
- Fourth+ steps are NOT called

### 3. Verify Non-AgencyError Wrapping

```bash
go test ./internal/pipeline/... -v -run "TestWrapsNonAgencyError"
```

Verifies:
- Step returns `errors.New("boom")` (plain Go error)
- Pipeline wraps it as `E_INTERNAL`
- Message is "internal error"
- Cause is preserved
- Details contain step name

---

## Branch Name and Commit Message

**Branch:** `pr04/s1-run-pipeline-orchestration`

**Commit Message:**

```
feat(pipeline): add run pipeline orchestrator for S1

Introduce the run pipeline framework with a RunService interface that
defines the 6 fixed steps: CheckRepoSafe, LoadAgencyConfig, CreateWorktree,
WriteMeta, RunSetup, StartTmux.

The pipeline executes steps in order, short-circuits on first error, and
preserves AgencyError codes. Non-AgencyError errors are wrapped into
E_INTERNAL with the step name in details.

New package: internal/pipeline/
- RunService interface with 6 step methods
- Pipeline struct with Run(ctx, opts) (runID, error) method
- RunPipelineOpts: Title, Runner, Parent, Attach fields
- PipelineState: accumulates RepoRoot, RepoID, RunnerCmd, etc.
- Step name constants

Pipeline behavior (per spec):
- Generates run_id immediately before step execution
- Executes steps in fixed order via RunService interface
- Short-circuits on first error, preserving AgencyError codes
- Wraps non-AgencyError into E_INTERNAL with step name in details
- Returns runID even on error (after generation)

Tests (8 total) use mock RunService implementations:
- Short-circuit preserves error codes
- Stub step returns E_NOT_IMPLEMENTED with step name
- Non-AgencyError wrapped into E_INTERNAL
- Success path executes all 6 steps
- RunID generated before steps execute
- Opts copied to state correctly
- Steps execute in fixed order
- Middle step failure short-circuits correctly

No CLI wiring, no git/tmux operations, no persistence in this PR.

Implements: s1_pr04 (run pipeline orchestration)
```
