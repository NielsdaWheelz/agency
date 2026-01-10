# S1 PR-06 Report: meta.json writer + run dir creation

## Summary of Changes

This PR implements durable run state persistence immediately after worktree creation:

1. **New error codes** (`internal/errors/errors.go`):
   - `E_RUN_DIR_EXISTS` — run directory already exists (collision/stale state)
   - `E_RUN_DIR_CREATE_FAILED` — failed to create run directory
   - `E_META_WRITE_FAILED` — failed to write meta.json atomically

2. **Store package run directory helpers** (`internal/store/store.go`):
   - `RunsDir(repoID)` — returns `${AGENCY_DATA_DIR}/repos/<repo_id>/runs/`
   - `RunDir(repoID, runID)` — returns `.../runs/<run_id>/`
   - `RunMetaPath(repoID, runID)` — returns `.../runs/<run_id>/meta.json`
   - `RunLogsDir(repoID, runID)` — returns `.../runs/<run_id>/logs/`

3. **Run metadata types and persistence** (`internal/store/run_meta.go`):
   - `RunMeta` struct with all fields per spec (schema_version, run_id, repo_id, title, runner, runner_cmd, parent_branch, branch, worktree_path, created_at, etc.)
   - `RunMetaFlags` struct for optional boolean flags
   - `RunMetaSetup` struct for setup script details
   - `RunMetaArchive` struct for archive-related fields
   - `EnsureRunDir(repoID, runID)` — creates run dir with exclusive semantics (os.Mkdir)
   - `WriteInitialMeta(repoID, runID, meta)` — atomic write via temp+rename
   - `ReadMeta(repoID, runID)` — read and parse meta.json
   - `UpdateMeta(repoID, runID, updateFn)` — read-modify-write pattern
   - `NewRunMeta(...)` — constructor for initial meta with required fields

4. **Pipeline wiring** (`internal/runservice/service.go`):
   - Implemented `WriteMeta()` step that was previously stubbed
   - Validates worktree_path exists before writing meta
   - Creates run directory and logs subdirectory
   - Writes initial meta.json with all required fields
   - Runner name is now properly stored in `st.Runner` during `LoadAgencyConfig()`
   - Added `SetNowFunc()` for time injection in tests

5. **Comprehensive tests**:
   - `internal/store/run_meta_test.go` — 11 new tests for run dir creation, meta writing, collisions, atomic writes, JSON format
   - `internal/runservice/service_test.go` — replaced `TestService_WriteMeta_NotImplemented` with 3 real tests: success path, missing worktree, and collision handling

## Problems Encountered

1. **Runner name vs runner command confusion**: The spec distinguishes between `runner` (name like "claude") and `runner_cmd` (verbatim command string). The existing code in `LoadAgencyConfig` resolved the command but didn't store the name in pipeline state.

2. **Test expectations for not-implemented step**: The existing test `TestService_WriteMeta_NotImplemented` expected `E_NOT_IMPLEMENTED`, which now needed to test actual functionality.

3. **JSON field omission**: Needed to ensure optional fields like `tmux_session_name`, `flags`, `setup`, `archive`, `pr_*`, etc. are properly omitted from JSON when empty (using `omitempty` tags).

## Solutions Implemented

1. **Runner name storage**: Modified `LoadAgencyConfig()` to set `st.Runner = runnerName` after resolving the runner, so `WriteMeta()` has access to both the name and the command.

2. **Test migration**: Replaced the not-implemented test with three comprehensive tests that exercise the actual implementation: successful write, missing worktree error, and run dir collision.

3. **JSON omitempty**: All optional fields in `RunMeta` struct have `omitempty` JSON tags to ensure clean output.

## Decisions Made

1. **Exclusive directory creation**: Used `os.Mkdir()` directly with `O_EXCL` semantics (fails if exists) rather than `MkdirAll()` to ensure run_id collisions are detected.

2. **Worktree validation**: Added explicit check that `worktree_path` exists and is a directory before proceeding with meta write, returning `E_INTERNAL` with details if not.

3. **Time injection**: Added `SetNowFunc()` to Service for deterministic testing of timestamps.

4. **Separate file for run metadata**: Created `run_meta.go` in the store package to keep the run-specific persistence logic separate from the repo index/record logic.

## Deviations from Spec

None. The implementation follows the PR-06 spec exactly:
- Run dir created with exclusive semantics
- Logs subdirectory created
- meta.json written atomically with all required fields
- Error codes match spec (E_RUN_DIR_EXISTS, E_RUN_DIR_CREATE_FAILED, E_META_WRITE_FAILED)
- tmux_session_name is absent in initial meta (will be added in PR-08)

## How to Test

### Run all tests

```bash
go test ./...
```

### Run store package tests specifically

```bash
go test ./internal/store/... -v
```

### Run runservice tests specifically

```bash
go test ./internal/runservice/... -v
```

### Build and verify no compilation errors

```bash
go build ./...
```

## How to Verify Functionality

The WriteMeta step is now wired into the run pipeline. To see it in action (once PR-07/08/09 complete the remaining steps), running `agency run` will:

1. Create worktree (PR-05)
2. **Create run directory at `${AGENCY_DATA_DIR}/repos/<repo_id>/runs/<run_id>/`**
3. **Create logs subdirectory at `.../runs/<run_id>/logs/`**
4. **Write `meta.json` with required fields**
5. Run setup script (PR-07)
6. Start tmux (PR-08)

For now, the pipeline still fails at step 5 (RunSetup not implemented), but steps 1-4 complete successfully.

## Branch Name and Commit Message

**Branch:** `pr06/meta-json-writer-run-dir`

**Commit message:**

```
feat(s1): implement meta.json writer + run dir creation (PR-06)

Add durable run state persistence immediately after worktree creation.
This ensures that even if later steps (setup/tmux) fail, a debuggable
record exists at ${AGENCY_DATA_DIR}/repos/<repo_id>/runs/<run_id>/.

Changes:

- Add new error codes: E_RUN_DIR_EXISTS, E_RUN_DIR_CREATE_FAILED,
  E_META_WRITE_FAILED for run persistence failure modes

- Add store package path helpers: RunsDir, RunDir, RunMetaPath,
  RunLogsDir for constructing run storage paths

- Add RunMeta struct and related types (RunMetaFlags, RunMetaSetup,
  RunMetaArchive) with JSON omitempty tags for optional fields

- Implement EnsureRunDir with os.Mkdir exclusive semantics to detect
  run_id collisions and fail with E_RUN_DIR_EXISTS

- Implement WriteInitialMeta, ReadMeta, UpdateMeta for atomic meta.json
  operations using existing WriteJSONAtomic helper

- Implement WriteMeta step in runservice.Service, replacing the stub
  with full functionality: validates worktree exists, creates run dir
  and logs subdir, writes initial meta.json

- Fix runner name storage in LoadAgencyConfig to set st.Runner so
  WriteMeta has access to the runner name (not just runner_cmd)

- Add comprehensive tests for run dir creation, meta writing,
  collision handling, atomic write behavior, and JSON format

meta.json required fields per spec:
- schema_version: "1.0"
- run_id, repo_id, title, runner, runner_cmd
- parent_branch, branch, worktree_path, created_at (RFC3339 UTC)

tmux_session_name is correctly absent in initial meta (PR-08 will add).

Refs: s1_pr06.md
```
