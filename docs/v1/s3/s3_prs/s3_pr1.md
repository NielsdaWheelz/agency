# Agency PR-1 Spec — slice 03 foundations (errors + meta fields + events + cmd runner + lock hook)

## goal
add the internal foundations required to implement `agency push` in later PRs: error codes, meta schema extensions, append-only events writer, a mockable command runner, and a repo-lock helper.

## scope
### in-scope
- add slice-03 error codes (public contract; no behavior uses them yet):
  - `E_UNSUPPORTED_ORIGIN_HOST`
  - `E_NO_ORIGIN`
  - `E_PARENT_NOT_FOUND`
  - `E_GIT_PUSH_FAILED`
  - `E_GH_PR_CREATE_FAILED`
  - `E_GH_PR_EDIT_FAILED`
  - `E_REPORT_INVALID`
- extend run `meta.json` schema (additive, optional fields):
  - `last_push_at` (RFC3339 string, omitted if unset)
  - `last_report_sync_at` (RFC3339 string, omitted if unset)
  - `last_report_hash` (lowercase hex string, omitted if unset)
- implement `events.jsonl` append-only writer with a stable envelope schema:
  - file: `${AGENCY_DATA_DIR}/repos/<repo_id>/runs/<run_id>/events.jsonl`
  - schema (one JSON per line):
    ```json
    {
      "schema_version": "1.0",
      "ts": "RFC3339",
      "event": "string",
      "repo_id": "string",
      "run_id": "string",
      "data": {}
    }
    ```
- introduce mockable command execution abstraction (`CmdRunner`) for `git`/`gh`/scripts:
  - supports: explicit cwd, argv (no shell strings), env overlay, stdin=/dev/null, stdout/stderr capture, exit code, duration, timeout
  - designed for unit testing via a fake runner
- add repo-lock helper (no behavioral integration required yet):
  - lock file: `${AGENCY_DATA_DIR}/repos/<repo_id>/.lock`
  - contents: `pid`, `started_at`, `command`
  - stale detection: if pid not alive → lock treated as stale and can be replaced
  - API: `WithRepoLock(repoID, commandName, fn)`

### explicitly out of scope
- no new CLI commands or flags
- no changes to `agency run/ls/show/...` behavior
- no git fetch/push logic
- no gh PR view/create/edit logic
- no changes to tmux behavior
- no changes to scripts execution semantics
- no changes to repo discovery / repo identity logic

## public surface area
none.

## files created/modified
### modified
- `.../meta.json` schema extension (struct + (de)serialization logic) to include optional:
  - `last_push_at`, `last_report_sync_at`, `last_report_hash`

### created (internal packages; names may vary but boundaries must match)
- `internal/errs/`:
  - error code constants (including slice-03 additions)
  - `AppError` type:
    - `Code string`
    - `Msg string`
    - `Cause error` (optional)
    - helpers: `IsCode(err, code)`, `Wrap(code,msg,cause)`
- `internal/execx/` (or equivalent):
  - `CmdRunner` interface
  - `OSCmdRunner` implementation using `exec.CommandContext`
  - `CmdSpec` / `CmdResult` types
- `internal/store/` (or equivalent):
  - atomic JSON file write helper (temp + rename)
  - meta load/save that preserves unknown fields (forward-compatible)
- `internal/events/` (or under store):
  - `EventWriter` with `Append(eventName string, data map[string]any) error`
- `internal/lock/`:
  - repo lock acquire/release, stale detection

## contracts / invariants
### cmd runner contract (v1)
- argv only; no shell-string execution
- `cwd` must be explicit for all agency uses (runner may allow empty but callers must not)
- env = process env + overlay map
- stdin must be `/dev/null` (non-interactive enforcement)
- timeouts:
  - runner must support `timeout` per command; on timeout return a typed timeout error and a result indicating timeout
- result semantics:
  - non-zero exit is NOT a go `error`; it is `CmdResult.ExitCode != 0`
  - `error` is reserved for spawn failures / timeout / internal runner failures

### meta.json IO contract
- all writes are atomic (write temp + rename)
- unknown fields in existing `meta.json` MUST be preserved on save (additive-only promise)
- explicit strategy (pick one and implement it in PR-3A):
  - decode into `map[string]json.RawMessage`, unmarshal known fields from that map, keep the raw map as `Unknown`; on save, re-emit the raw map with updated known fields to preserve unknowns byte-for-byte where possible
  - do not rely on `json.Unmarshal` into a struct alone (it drops unknowns)

### events.jsonl contract
- append-only; never rewrite or truncate
- each line must be valid JSON, newline-terminated
- writer failure is best-effort: emit a warning and continue the command; only meta writes are fatal

## tests
### unit tests (required)
1) **error codes**
- compile-time: slice-03 codes exist as constants
- runtime: `AppError{Code}` prints `CODE: msg` format (no panic)

2) **meta roundtrip + unknown preservation**
- given fixture `meta.json` containing an unknown field (e.g. `"future_field": {"x":1}`)
- when load → set one new optional push field → save
- then saved json still contains `future_field` unchanged

3) **atomic write**
- write meta; ensure no partial file on simulated interruption:
  - (testable by verifying writes go to `*.tmp` then rename; don’t try to simulate power loss)

4) **events writer**
- append two events; file contains exactly two json lines
- each line includes required keys (`schema_version`, `ts`, `event`, `repo_id`, `run_id`)

5) **cmd runner (OS implementation)**
- do not require `sh`; prefer direct binaries if present:
  - use `exec.LookPath("true")` / `exec.LookPath("false")` for exit code tests
  - use `exec.LookPath("sleep")` for timeout tests
  - if required binaries are missing, skip the test cleanly
- if you keep a shell-based test, gate it with `exec.LookPath("sh")` and skip if absent
- assert stdout/stderr/exit code
- timeout test: `sh -c 'sleep 2'` with 100ms timeout → timeout error

6) **cmd runner (fake)**
- provide a fake runner used by tests (or demonstrate interface allows mocking cleanly)

### how to run
- `go test ./...`

## guardrails
- do not add or modify any CLI commands/flags
- do not implement any git/gh behavior beyond the generic command runner
- do not change existing schemas except the additive optional fields listed
- do not add new persistence files beyond internal helpers and the lock/events primitives
- keep packages small and dependency-direction clean:
  - commands (later) depend on `errs/execx/store/events/lock`, not vice versa

## acceptance checklist (reviewer)
- new error codes exist, documented in code, and don’t change existing ones
- `meta.json` optional fields are additive and do not break older meta files
- meta save preserves unknown fields
- file writes are atomic (temp + rename)
- events.jsonl writer produces valid json lines
- events append failures warn and do not block mutating commands
- cmd runner captures stdout/stderr/exit code and enforces stdin=/dev/null
- tests pass: `go test ./...`

## notes
- do not introduce `agency push` command in this PR (that is PR-3B+)
- keep the implementation flexible enough for later non-interactive env injection (`GIT_TERMINAL_PROMPT=0`, `GH_PROMPT_DISABLED=1`) but do not hardcode those in pr-3a unless they’re needed for tests.
