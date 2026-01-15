# PR-04b Report: attach + stop + kill commands

## Summary of Changes

This PR implements the `stop` and `kill` commands for tmux session lifecycle control, and refines the `attach` command to return a specific `E_SESSION_NOT_FOUND` error with a helpful suggestion when the tmux session is missing.

### Key Changes

1. **New `agency stop <run_id>` command**
   - Sends C-c to the tmux session (best-effort interrupt)
   - Sets `needs_attention` flag on the run if session exists
   - Appends `stop` event to events.jsonl with keys list
   - No-op if session missing (exits 0, stderr message, no event)

2. **New `agency kill <run_id>` command**
   - Kills the tmux session
   - Workspace remains intact
   - Appends `kill_session` event to events.jsonl
   - No-op if session missing (exits 0, stderr message, no event)

3. **Updated `agency attach <run_id>` command**
   - Now uses TmuxClient interface for testability
   - Returns `E_SESSION_NOT_FOUND` (instead of `E_TMUX_SESSION_MISSING`) when session is missing
   - Error includes suggestion: `try: agency resume <id>`

4. **New error code**
   - `E_SESSION_NOT_FOUND` - attach when tmux session is missing; suggests resume

5. **New event helper functions**
   - `StopData(sessionName string, keys []string)` - for stop events
   - `KillSessionData(sessionName string)` - for kill_session events

## Problems Encountered

1. **Test environment setup complexity**: The tests required computing the correct `repo_id` to match what the real code computes from git origin. Initially used a hardcoded value which didn't match the sha256 hash of `github:test/repo`.

2. **Git command key mismatch**: The fake command runner's response keys didn't match the actual git commands used by the code. `GetOriginInfo` uses `git config --get remote.origin.url` not `git remote get-url origin`.

3. **Unused variable in attach.go**: After refactoring to use session name from `tmux.SessionName(runID)`, the `meta` variable was no longer used, causing a compilation error.

## Solutions Implemented

1. **Computed repo_id dynamically**: Changed tests to use `identity.DeriveRepoIdentity(repoDir, originURL)` to compute the exact same repo_id that the production code uses.

2. **Fixed git command keys**: Updated fake command runner responses to use `git config --get remote.origin.url` to match the actual code path.

3. **Removed unused variable**: Changed `meta, err := st.ReadMeta(...)` to `_, err = st.ReadMeta(...)` since we only need to verify the run exists.

## Decisions Made

1. **Session name source of truth**: Used `tmux.SessionName(runID)` consistently as the source of truth for session names, rather than reading from `meta.TmuxSessionName`. This ensures consistency and correctness even if meta is stale.

2. **Interactive attach bypasses TmuxClient**: The actual `tmux attach` call uses direct `os/exec` for proper terminal handling, while session existence checks use the `TmuxClient` interface. This allows testing the session check logic while preserving interactive attach behavior.

3. **Error handling order**: For stop/kill, SendKeys/KillSession errors are returned immediately before meta/events are written. This ensures we don't record an event for an action that failed.

4. **Event append failures**: If event append fails after a successful tmux operation, the command returns `E_PERSIST_FAILED`. For stop, the `needs_attention` flag is already set in meta before the event is appended.

## Deviations from Spec

None. The implementation follows the PR-04b spec exactly:
- `attach` returns `E_SESSION_NOT_FOUND` with suggestion
- `stop` sends C-c, sets flag, appends event (no-op if missing)
- `kill` kills session, appends event (no-op if missing)
- All commands use exact run_id (no prefix resolution)
- Stop/kill bypass repo lock

## How to Run Commands

### Build
```bash
go build ./cmd/agency
```

### Test
```bash
go test ./...
go test ./internal/commands/... -v -run "Test(Stop|Kill|Attach)"
```

### Usage

**Stop a run's tmux session:**
```bash
agency stop 20260110120000-a3f2
```

**Kill a run's tmux session:**
```bash
agency kill 20260110120000-a3f2
```

**Attach to a run (shows E_SESSION_NOT_FOUND if session missing):**
```bash
agency attach 20260110120000-a3f2
```

### Verify functionality

1. Create a run:
   ```bash
   agency run --title "test" --runner claude
   agency ls  # note the run_id
   ```

2. Test stop:
   ```bash
   agency stop <run_id>
   agency show <run_id>  # should show needs_attention: true
   ```

3. Test kill:
   ```bash
   agency kill <run_id>
   agency attach <run_id>  # should fail with E_SESSION_NOT_FOUND
   ```

4. Test attach on missing session:
   ```bash
   agency attach <run_id>  # should suggest "agency resume <id>"
   ```

## Branch Name and Commit Message

**Branch:** `pr4/s4-pr04b-attach-stop-kill`

**Commit Message:**
```
feat(s4): implement attach + stop + kill lifecycle commands (PR-04b)

Add tmux session lifecycle control commands for slice 4:

- agency stop <run_id>: send C-c to runner (best-effort interrupt)
  - Sets needs_attention flag when session exists
  - Appends 'stop' event with keys list
  - No-op if session missing (exit 0, stderr message)

- agency kill <run_id>: kill tmux session (workspace remains)
  - Appends 'kill_session' event
  - No-op if session missing (exit 0, stderr message)

- agency attach <run_id>: refined error handling
  - Returns E_SESSION_NOT_FOUND when session missing
  - Includes suggestion: "try: agency resume <id>"
  - Uses TmuxClient interface for testability

Implementation details:
- Add E_SESSION_NOT_FOUND error code to errors.go
- Add StopData/KillSessionData event helpers to events/append.go
- Create stop.go and kill.go command implementations
- Add StopWithTmux/KillWithTmux variants for testing with fake client
- Update AttachWithTmux to use TmuxClient.HasSession
- Register stop and kill commands in dispatch.go
- Unit tests for all commands using fake tmux client

Error codes used:
- E_RUN_NOT_FOUND: run metadata not found
- E_SESSION_NOT_FOUND: tmux session missing (attach only)
- E_TMUX_NOT_INSTALLED: tmux binary not found
- E_TMUX_FAILED: tmux command failed
- E_PERSIST_FAILED: event append failed

Per spec:
- Stop/kill bypass repo lock (best-effort, non-mutating of git)
- Attach does not create sessions or mutate meta/events
- Session name derived from tmux.SessionName(runID)

Tests pass without tmux installed (fake client).
```
