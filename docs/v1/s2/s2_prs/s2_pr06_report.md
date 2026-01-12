# s2 pr-06 report: transcript capture + ansi stripping + events.jsonl

## summary of changes

this PR implements deterministic tmux transcript capture and per-run event logging for `agency show <id> --capture`, completing slice 2 (observability).

### new packages created:
- `internal/tmux/` — tmux session detection, scrollback capture, and ANSI escape code stripping
- `internal/events/` — per-run append-only event logging (events.jsonl)

### modified packages:
- `internal/commands/show.go` — added `--capture` flag with lock acquisition, event emission, and transcript capture
- `internal/cli/dispatch.go` — wired `--capture` flag to show command
- `internal/errors/errors.go` — added `E_REPO_LOCKED` error code
- `internal/render/json.go` — added `CaptureJSON` struct for JSON output

### files created:
```
internal/tmux/ansi.go           # StripANSI pure function
internal/tmux/ansi_test.go      # table-driven tests (28 cases + panic safety tests)
internal/tmux/capture.go        # Executor interface, HasSession, CaptureScrollback
internal/tmux/capture_test.go   # mock executor tests
internal/events/append.go       # Event struct, AppendEvent function
internal/events/append_test.go  # append + rotation tests
```

### files modified:
```
README.md                       # updated status, added --capture docs, updated project structure
internal/cli/dispatch.go        # added --capture flag
internal/commands/show.go       # implemented capture flow
internal/errors/errors.go       # added E_REPO_LOCKED
internal/render/json.go         # added CaptureJSON struct
```

## problems encountered

### 1. ANSI regex incomplete for trailing escapes
**problem:** the initial regex didn't handle a lone ESC byte at the end of input, causing a test failure.

**solution:** added `|\x1b\[?$` to the regex to catch trailing ESC and partial CSI sequences.

### 2. mock executor design
**problem:** needed a clean way to test tmux interactions without real tmux.

**solution:** created an `Executor` interface that abstracts command execution, with `RealExecutor` for production and `MockExecutor` for tests. the mock records calls and returns configurable responses.

## solutions implemented

### ANSI stripping
- used a comprehensive regex that handles:
  - CSI sequences (colors, cursor movement, clear)
  - OSC sequences (title set, hyperlinks)
  - single-char escapes
  - DCS/PM/APC sequences
  - trailing/malformed escapes
- function is pure, never panics, and always returns valid output

### transcript capture flow
1. acquire repo lock (fails with `E_REPO_LOCKED` if held)
2. emit `cmd_start` event (best-effort)
3. check session existence via `tmux has-session`
4. if exists: capture via `tmux capture-pane -p -S -`
5. strip ANSI codes
6. rotate transcript.txt → transcript.prev.txt
7. write new transcript atomically (temp file + rename)
8. emit `cmd_end` event with capture result
9. release lock
10. show normal output

### failure handling
- capture failures never block show output
- all tmux failures emit warnings to stderr
- events append failures are silently ignored
- rotation failures don't prevent new transcript write

## decisions made

### 1. lock acquisition before event emission
events are only emitted when lock is successfully acquired. this ensures events reflect actual command execution state.

### 2. single backup rotation
chose `transcript.prev.txt` as the only backup rather than timestamped archives. simpler and matches spec exactly.

### 3. atomic transcript write
using temp file + rename pattern to prevent partial writes. failure to write temp still allows show to complete.

### 4. args recording in events
recording full args slice (`opts.Args`) in events for debugging, including the run_id and all flags.

### 5. capture result in JSON output
when `--json` + `--capture` are combined, the JSON output includes a `capture` field with `capture_ok`, `capture_stage`, and `capture_error`.

## deviations from spec

### none significant
implementation follows the spec closely:
- lock + events emission as specified
- best-effort capture with warnings on failure
- no new top-level commands
- no gh calls
- no meta.json or repo_index.json mutations

### minor clarifications
- `Args` field added to `ShowOpts` for event logging (not explicitly in spec but needed for `cmd_start` data)
- `CaptureJSON` type added to render package for JSON output (clean separation)

## how to run new/changed commands

### basic show (unchanged)
```bash
agency show <run_id>
agency show <run_id> --json
agency show <run_id> --path
```

### show with capture (new)
```bash
# capture transcript from active tmux session
agency show <run_id> --capture

# capture + JSON output
agency show <run_id> --capture --json

# capture + path output
agency show <run_id> --capture --path
```

### viewing transcript files
```bash
# after capture, view transcript
cat ${AGENCY_DATA_DIR}/repos/<repo_id>/runs/<run_id>/transcript.txt

# view previous transcript
cat ${AGENCY_DATA_DIR}/repos/<repo_id>/runs/<run_id>/transcript.prev.txt

# view events
cat ${AGENCY_DATA_DIR}/repos/<repo_id>/runs/<run_id>/events.jsonl
```

## how to test functionality

### run tests
```bash
go test ./...
go test ./internal/tmux/...
go test ./internal/events/...
```

### manual testing

1. **with active tmux session:**
```bash
agency run --title "test capture"
# do some work in the session, then detach
agency show <run_id> --capture
cat ${AGENCY_DATA_DIR}/repos/<repo_id>/runs/<run_id>/transcript.txt
```

2. **without tmux session:**
```bash
# kill the session first
tmux kill-session -t agency_<run_id>
agency show <run_id> --capture
# should warn "no tmux session; transcript not captured"
```

3. **lock contention:**
```bash
# in one terminal, hold the lock
# (requires instrumenting code or using a long-running command)
# in another terminal
agency show <run_id> --capture
# should fail with E_REPO_LOCKED
```

4. **events file:**
```bash
agency show <run_id> --capture
tail -n 2 ${AGENCY_DATA_DIR}/repos/<repo_id>/runs/<run_id>/events.jsonl
# should show cmd_start and cmd_end events
```

## branch name and commit message

**branch:** `pr06/transcript-capture-events`

**commit message:**
```
feat(s2): implement transcript capture + events.jsonl for show --capture

Add deterministic tmux transcript capture and per-run event logging.

New packages:
- internal/tmux: Executor interface for testable tmux interaction
  - StripANSI: pure function to remove ANSI escape codes
  - HasSession: check if tmux session exists
  - CaptureScrollback: capture full pane scrollback
- internal/events: append-only event logging
  - Event struct with schema_version, timestamp, repo_id, run_id, event, data
  - AppendEvent: lazily creates events.jsonl and appends entries

New functionality:
- agency show <id> --capture:
  - takes repo lock (mutating mode)
  - emits cmd_start/cmd_end events to events.jsonl
  - captures tmux scrollback when session exists
  - strips ANSI escape codes from captured text
  - rotates transcript.txt -> transcript.prev.txt (single backup)
  - writes new transcript.txt atomically
  - capture failures never block show output

Error handling:
- E_REPO_LOCKED returned if lock cannot be acquired
- all other capture failures emit warnings and continue

Tests:
- 28+ table-driven tests for ANSI stripping
- mock executor tests for tmux commands
- events append/rotation tests

Completes slice 2 (observability) per s2_spec.md and s2_pr06.md.
```
