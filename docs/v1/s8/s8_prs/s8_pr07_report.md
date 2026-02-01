# PR-07 Report: Headless Stream Parsing, Normalized Events, and Semantic Status

## Summary of Changes

This PR implements daemon-side parsing of headless runner output (Claude and Codex), producing:

1. **Normalized event stream** (`stream.jsonl`) - A stable, runner-agnostic event format
2. **Persisted semantic status** (`semantic_status`) in `InvocationMeta` - Derived status like `working` and `ready_for_review`
3. **Line-framed reader** - Proper handling of JSONL streams including EOF partial lines

### Files Added

- `internal/daemon/stream/events.go` - Normalized event schema
- `internal/daemon/stream/adapter.go` - Adapter interface
- `internal/daemon/stream/claude.go` - Claude adapter (parses `stream-json` output)
- `internal/daemon/stream/codex.go` - Codex adapter (parses JSON output)
- `internal/daemon/stream/parser.go` - Main parser orchestrating line reading, parsing, and status tracking
- `internal/daemon/stream/parser_test.go` - Comprehensive tests
- `internal/daemon/stream/testdata/*.jsonl` - Real runner output fixtures

### Files Modified

- `internal/store/store.go` - Added `SandboxStreamLogPath()` for `stream.jsonl` path
- `internal/store/invocation.go` - Added `SemanticStatus` and `SemanticStatusUpdatedAt` fields to `InvocationMeta`
- `internal/daemon/types.go` - Added `Parser`, `StreamLogFile`, and `Runner` fields to `SupervisedProcess`
- `internal/daemon/handlers.go` - Updated headless start handlers to create parser and stream file
- `internal/daemon/server.go` - Added `streamAndParseOutput`, `runSemanticStatusFlushLoop`, and `flushSemanticStatus`
- `README.md` - Added documentation for stream parsing and semantic status features

## Problems Encountered

### 1. Line Framing vs Chunk-Based Reading

**Problem**: The original `streamOutput` used 4KB chunk-based reading which doesn't respect JSON line boundaries. This would cause lines to be split across writes, making parsing impossible.

**Solution**: Implemented a line-framed reader using `bufio.Reader.ReadBytes('\n')` that:
- Preserves exact bytes when writing to `raw.jsonl`
- Handles partial final lines (EOF without trailing newline)
- Enforces an 8MB safety valve for oversized lines

### 2. EOF Partial Line Handling

**Problem**: The final runner event (often `result:success`) may not have a trailing newline, which could cause it to be silently dropped.

**Solution**: The parser checks for remaining data after EOF and processes it as the final line:
```go
if err == io.EOF {
    if len(line) > 0 {
        return line, io.EOF  // Return partial line before EOF
    }
    return nil, io.EOF
}
```

### 3. Parse Error Handling Without Breaking Streams

**Problem**: Malformed JSON in the middle of a stream should not crash the daemon or break the invocation.

**Solution**: Parse errors are handled gracefully:
- Write raw bytes to `raw.jsonl` regardless of parse success
- Emit `kind=parse_error` events with throttling (at most once per 10 errors or 5 seconds)
- Continue parsing subsequent lines normally
- Preserve the last valid semantic status

### 4. Throttled Meta Writes

**Problem**: Writing semantic status to meta.json on every event would be expensive (atomic writes involve temp file + rename).

**Solution**: Implemented throttled writes:
- Update semantic status in memory immediately
- Flush to disk at most once per 500ms
- Only write when status actually changed (not just on every tick)
- Always flush on invocation exit

## Decisions Made

### 1. Separate Adapter Pattern

Chose to implement runner-specific adapters rather than a single parser with conditionals:
- Cleaner separation of concerns
- Easier to add new runners in the future
- Each adapter fully owns its event mapping

### 2. `stream.jsonl` is Daemon-Written Only

The CLI never parses `raw.jsonl` or writes to `stream.jsonl`. This maintains the single-writer principle:
- Daemon writes all log files
- CLI only reads log files (for `agent logs`)
- Avoids race conditions between CLI and daemon

### 3. Semantic Status Uses Existing `runnerstatus.Status` Type

Rather than creating new status types, reused the existing `runnerstatus.Status` constants:
- `working`, `needs_input`, `blocked`, `ready_for_review`
- JSON compatibility with existing runner_status.json contracts
- CLI can display semantic status without new code

### 4. No `blocked` Inference for Codex

Per the spec, Codex does not emit reliable structured signals for "cannot proceed". Attempting heuristics would be wrong more often than right. Lifecycle failure (`status=failed`) handles actual errors.

### 5. Normalized Event Data is Full-Text

Chose to store full text content in normalized events rather than truncating:
- It's local storage (not over network)
- `raw.jsonl` has it anyway
- Enables future watch/display features without re-parsing

## Deviations from Spec

### Minor: Additional Data Fields

Added some additional normalized data fields beyond the spec:
- `role` field in message events (helps distinguish assistant vs user)
- `thread_id` in Codex session_start (for debugging/correlation)

These are additive and don't affect the core contract.

### None: Spec Compliance

The implementation follows the spec closely:
- Line-framed reading with 8MB safety valve ✓
- EOF partial line handling ✓
- Normalized event schema with `seq`, `timestamp`, `invocation_id`, `runner`, `kind`, `data` ✓
- Claude and Codex adapters with specified mappings ✓
- Semantic status inference rules as specified ✓
- Throttled meta writes (500ms, only on change) ✓
- Parse error throttling (10 errors or 5 seconds) ✓

## How to Test

### Run Stream Package Tests

```bash
cd /path/to/agency
go test ./internal/daemon/stream/... -v
```

Expected output: All 8 test cases pass including:
- Claude adapter parsing
- Codex adapter parsing
- Full fixture streaming (Claude and Codex)
- Malformed line handling
- EOF partial line handling
- Seq monotonicity

### Run Full Daemon Tests

```bash
go test ./internal/daemon/... -v -count=1
```

Expected output: All daemon tests pass (28+ tests) including the new stream parsing integration.

### Manual Testing (Headless Invocation)

1. Start daemon:
   ```bash
   agency daemon start
   ```

2. Create a worktree:
   ```bash
   agency worktree create --name test-pr07
   ```

3. Start a headless invocation:
   ```bash
   agency agent start --worktree test-pr07 --headless --prompt "List the files in the current directory"
   ```

4. Check log files:
   ```bash
   # Find sandbox directory
   SANDBOX=$(agency agent show <invocation-id> --json | jq -r '.sandbox_path')
   
   # View raw.jsonl (verbatim)
   cat "$SANDBOX/../logs/raw.jsonl"
   
   # View stream.jsonl (normalized)
   cat "$SANDBOX/../logs/stream.jsonl"
   
   # Check semantic status in meta
   agency agent show <invocation-id> --json | jq '.semantic_status'
   ```

5. Expected observations:
   - `raw.jsonl` contains verbatim runner JSONL output
   - `stream.jsonl` contains normalized events with `seq`, `kind`, `data`
   - `semantic_status` shows `working` during execution, `ready_for_review` on success

## Commit Message

```
feat(daemon): implement PR-07 stream parsing, normalized events, semantic status

Add daemon-side parsing of headless runner output for Claude and Codex.
Introduces normalized event stream (stream.jsonl) and persisted semantic
status in InvocationMeta.

Key changes:
- internal/daemon/stream/: New package with adapter pattern for runner parsing
  - events.go: Normalized event schema (schema_version, seq, timestamp, kind, data)
  - adapter.go: Adapter interface for pluggable runner parsers
  - claude.go: Claude adapter (system:init, assistant, user, result events)
  - codex.go: Codex adapter (thread.started, item.*, turn.completed events)
  - parser.go: Line-framed reader, semantic status tracking, throttled writes
  - parser_test.go: Comprehensive tests with real runner output fixtures

- internal/store/invocation.go: Add SemanticStatus and SemanticStatusUpdatedAt
  fields to InvocationMeta for persisting derived status

- internal/store/store.go: Add SandboxStreamLogPath() for stream.jsonl path

- internal/daemon/types.go: Add Parser, StreamLogFile, Runner to SupervisedProcess

- internal/daemon/handlers.go: Update start_headless handlers to:
  - Create stream.jsonl alongside raw.jsonl
  - Instantiate parser for stdout streaming
  - Start semantic status flush loop
  - Handle final semantic status on exit

- internal/daemon/server.go: Add streamAndParseOutput(), runSemanticStatusFlushLoop(),
  flushSemanticStatus() for orchestrating parsing and throttled persistence

- README.md: Document stream parsing, semantic status, and log file structure

Line-framing handles:
- Proper JSONL line boundaries (not chunk-based)
- EOF partial lines (no trailing newline)
- 8MB safety valve for oversized lines
- Graceful parse error handling with throttled emission

Semantic status inference:
- working: Any assistant/command activity
- ready_for_review: Successful result/completion
- Cleared on invocation failure

Throttled persistence:
- In-memory updates on every event
- Disk writes at most once per 500ms
- Only on actual status change
- Final flush guaranteed on exit

Acceptance criteria met:
- [x] raw.jsonl remains verbatim
- [x] EOF partial lines captured and parsed
- [x] Lines >8MB written but not parsed
- [x] stream.jsonl with monotonic seq
- [x] Normalized data matches per-adapter contracts
- [x] semantic_status appears in InvocationMeta
- [x] No codex blocked inference (deferred)
- [x] Lifecycle behavior unchanged
- [x] No CLI behavior changes required
- [x] Parsing failures do not crash daemon
- [x] Meta writes only on change
- [x] Tests use real runner output fixtures
```
