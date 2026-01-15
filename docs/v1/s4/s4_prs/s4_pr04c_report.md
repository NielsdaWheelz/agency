# PR-04c Report: Resume Command Implementation

## Summary of Changes

This PR implements the `agency resume` command as specified in the s4 slice for lifecycle control. The resume command provides reliable session management:

1. **New `resume` command** (`internal/commands/resume.go`):
   - Attach to existing tmux session if present
   - Create new tmux session (and start runner) if missing
   - Support `--detached` mode to ensure session exists without attaching
   - Support `--restart` to kill and recreate session with confirmation prompting
   - Support `--yes` to skip confirmation in non-interactive mode
   - Validates worktree existence before any tmux operations
   - Distinguishes archived vs corrupted/missing worktree failures

2. **New `E_CONFIRMATION_REQUIRED` error code** (`internal/errors/errors.go`):
   - Used when `--restart` is attempted in non-interactive mode without `--yes`

3. **New TTY detection helpers** (`internal/tty/tty.go`):
   - `IsTTY(f *os.File) bool` - check if a file is a TTY
   - `IsInteractive() bool` - check if stdin+stderr are both TTYs (for prompts)

4. **Shared runner resolution helper** (`internal/config/runner.go`):
   - `ResolveRunnerCmd(cfg *AgencyConfig, runnerName string) (string, error)`
   - Extracted from runservice to be shared between `run` and `resume`
   - Refactored runservice/service.go to use the shared helper

5. **Resume events helpers** (`internal/events/append.go`):
   - `ResumeData()` - for resume_attach, resume_create, resume_restart events
   - `ResumeFailedData()` - for resume_failed events with reason

6. **CLI wiring** (`internal/cli/dispatch.go`):
   - Added `resume` subcommand with `--detached`, `--restart`, `--yes` flags
   - Added usage text with examples

7. **Comprehensive test suite** (`internal/commands/resume_test.go`):
   - 11 test cases covering all resume paths
   - Uses fake tmux client (no tmux required in CI)
   - Tests session existence detection, creation, restart, locking, worktree validation

8. **Updated README.md**:
   - Added complete `agency resume` documentation section
   - Added `tty` package to project structure
   - Updated slice 4 progress status
   - Fixed attach error code documentation

## Problems Encountered

1. **FS interface limitation**: The `fs.FS` interface didn't have a `DirExists` method. Solved by using `Stat()` and checking `IsDir()` on the returned `FileInfo`.

2. **Pointer vs value for runner resolution**: The `LoadAndValidateForS1` returns `AgencyConfig` by value, but `ResolveRunnerCmd` initially expected a pointer. Fixed by passing `&cfg` to the helper.

3. **Interactive testing**: Testing TTY-dependent confirmation prompts required overriding the `isInteractive` package-level var. This pattern allows tests to control interactive behavior without actual TTY detection.

## Solutions Implemented

1. **Double-check locking pattern**: Resume uses double-check pattern for session creation:
   - Check session existence without lock
   - If mutation needed (create/restart), acquire lock
   - Re-check session existence under lock
   - Proceed with mutation only if still needed

2. **TTY detection for prompts**: Used `os.ModeCharDevice` check on file stats to determine if stdin/stderr are TTYs. Prompts only shown when both are TTYs.

3. **Worktree validation before tmux**: Check worktree exists before any tmux operations. This prevents confusing errors where tmux operations fail because the cwd doesn't exist.

4. **Event invariant**: On any successful resume command (attach/create/restart), exactly one event is appended. Failed resumes (worktree missing) also append `resume_failed` event with reason.

## Decisions Made

1. **No meta.json mutations**: Per spec, resume does not mutate meta.json to avoid unknown-field loss. Only events are appended.

2. **Prompt text on stderr**: The restart confirmation prompt is printed to stderr (not stdout) following Unix conventions for interactive prompts.

3. **User decline exits 0**: When user declines restart confirmation, exit code is 0 (not an error). This matches the spec's "canceled with exit 0" behavior.

4. **Race handling**: If another process creates the session between initial check and lock acquisition (race condition), resume treats it as an attach path and appends `resume_attach` event.

5. **Runner resolution shared**: Extracted runner resolution to a shared helper in `internal/config/runner.go` rather than duplicating logic. This ensures `run` and `resume` use identical resolution semantics.

## Deviations from Prompt/Spec

None. The implementation follows the s4_pr04c.md spec precisely:

- All error codes implemented as specified
- All event names match spec
- Locking behavior matches spec (only for create/restart)
- TTY detection matches spec (stdin+stderr for prompts)
- Output format matches spec (`ok: session <name> ready` for detached)

## How to Run New/Changed Commands

### Resume a run (attach or create session)
```bash
# Basic resume - attach if session exists, create if missing
agency resume <run_id>

# Detached mode - ensure session exists but don't attach
agency resume <run_id> --detached

# Force restart - kill existing session and recreate
agency resume <run_id> --restart

# Non-interactive restart (skip confirmation)
agency resume <run_id> --restart --yes
```

### Example workflow
```bash
# Create a run
agency run --title "test feature" --runner claude

# Detach from tmux (Ctrl+B, D)
# Later, system restarts or session is killed

# Resume the run - session will be recreated
agency resume <run_id>

# Or check session exists without attaching
agency resume <run_id> --detached
```

## How to Test

### Run all tests
```bash
go test ./... -count=1
```

### Run resume tests only
```bash
go test ./internal/commands/... -v -run "Resume" -count=1
```

### Manual testing

1. Create a run:
```bash
agency run --title "s4 test"
agency ls  # note the run_id
```

2. Detach from tmux (Ctrl+B, D)

3. Test attach:
```bash
agency attach <run_id>
```

4. Kill session and test resume:
```bash
agency kill <run_id>
agency attach <run_id>   # should fail with E_SESSION_NOT_FOUND
agency resume <run_id>   # should recreate session
```

5. Test restart confirmation:
```bash
agency resume <run_id> --restart   # should prompt
agency resume <run_id> --restart --yes  # should skip prompt
```

## Branch and Commit

**Branch name:** `pr4/s4-pr04c-resume-command`

**Commit message:**

```
feat(s4): implement agency resume command with session management

Add the `agency resume` command to complete slice 4 lifecycle control.
Resume ensures a tmux session exists for a run, creating one if needed,
with support for detached mode and forced restart.

Key changes:
- Add resume command with --detached, --restart, --yes flags
- Add E_CONFIRMATION_REQUIRED error code for non-interactive restart
- Add internal/tty package for TTY detection helpers
- Add internal/config/runner.go for shared runner resolution
- Refactor runservice to use shared runner resolution
- Add resume events (resume_attach, resume_create, resume_restart, resume_failed)
- Add comprehensive test suite (11 tests, no tmux required in CI)
- Update README with resume command documentation

Behavior:
- Resume attaches to existing session if present
- Resume creates new session (and starts runner) if missing
- --detached ensures session exists without attaching
- --restart kills and recreates session with confirmation
- --yes skips confirmation in non-interactive mode
- Validates worktree existence before tmux operations
- Uses double-check locking (lock only for create/restart)
- Appends exactly one event per successful resume

Error handling:
- E_WORKTREE_MISSING with reason (archived vs missing)
- E_CONFIRMATION_REQUIRED for non-interactive restart without --yes
- E_RUN_NOT_FOUND, E_REPO_LOCKED, E_TMUX_FAILED as appropriate

Tests:
- All paths covered with fake tmux client
- No tmux required in CI
- TTY behavior tested via isInteractive override

Closes slice 4 PR-04c. Ready for slice 5 (merge + archive).
```
