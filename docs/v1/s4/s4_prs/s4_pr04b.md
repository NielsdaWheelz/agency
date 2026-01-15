# agency l4: pr-04b spec — attach + stop + kill (slice 04)

goal: implement deterministic, minimal lifecycle commands that **do not create sessions**: refine `attach` to emit a specific missing-session error, and add `stop` (best-effort interrupt) + `kill` (destroy tmux session) with correct meta/events behavior. all logic must be unit-testable with a fake tmux client (no tmux required in CI).

non-goals:
- no tmux client interface work beyond what is required to use the pr-04a interface (assumes pr-04a landed)
- no pid/process-tree inspection
- no transcript replay
- no setup/verify/archive scripts
- no git mutations (beyond reading/writing meta + appending events)
- no prefix-based run id resolution changes (attach/stop/kill remain **exact run_id only**)

---

## scope

in-scope:
- `agency attach <run_id>`:
  - check tmux session existence via `TmuxClient.HasSession`
  - if session missing, return `E_SESSION_NOT_FOUND` with suggestion `try: agency resume <id>`
  - if session exists, attach via `TmuxClient.Attach`
- new `agency stop <run_id>`:
  - if session exists: send `C-c` only via tmux key injection; set `meta.flags.needs_attention=true`; append a `stop` event with keys list
  - if session missing: no-op, exit 0, print `no session for <id>` to stderr; **no meta mutation; no event**
- new `agency kill <run_id>`:
  - if session exists: kill tmux session; append `kill_session` event
  - if session missing: no-op, exit 0, print `no session for <id>` to stderr; **no event**
- errors:
  - introduce stable public error code `E_SESSION_NOT_FOUND` (attach only)
- tests:
  - table-driven unit tests for attach/stop/kill using a fake tmux client and temp store/events files

out-of-scope:
- `resume` command (pr-04c)
- any changes to session naming conventions (whatever pr-04a defines is the source of truth)
- event schema changes; keep existing event envelope rules
- changing stop/kill to record noop events (explicitly not in v1 s4)

---

## public surface area

### new commands
- `agency stop <run_id>`
- `agency kill <run_id>`

### changed behavior
- `agency attach <run_id>`:
  - now returns `E_SESSION_NOT_FOUND` when the tmux session does not exist (and suggests `resume`)
  - still uses exact run_id (no prefix support)

### new error codes
- `E_SESSION_NOT_FOUND`

existing error codes used:
- `E_RUN_NOT_FOUND`
- `E_TMUX_NOT_INSTALLED`
- `E_TMUX_FAILED` / `E_TMUX_ATTACH_FAILED` (whichever is already the canonical mapping in this codebase)
- `E_PERSIST_FAILED` (events append failure)

---

## exact behaviors

### `agency attach <id>`

behavior:
1. resolve repo + repo_id as existing attach does today
2. read meta for exact `run_id`:
   - if missing: return `E_RUN_NOT_FOUND`
3. compute session name using `internal/tmux.SessionName(run_id)` (pr-04a source of truth)
   - do not call `ids.ResolveRunRef`; treat the provided run_id as exact
4. check session existence:
   - `exists, err := tmux.HasSession(sessionName)`
   - if err maps to `E_TMUX_NOT_INSTALLED`, return that
   - if err other: return existing tmux failure code (do not add new codes)
5. if `exists == false`:
   - return `E_SESSION_NOT_FOUND`
   - stderr message must include: `try: agency resume <id>`
   - stdout must be empty
6. else:
   - call `tmux.Attach(sessionName)`
   - on failure: return existing tmux attach failure code

notes:
- attach must not create sessions
- attach must not write events
- attach must not mutate meta

### `agency stop <id>`

behavior:
1. resolve repo + read meta for exact `run_id`:
   - if missing: `E_RUN_NOT_FOUND`
2. compute session name via `tmux.SessionName(run_id)`
3. check session existence:
   - if missing: print `no session for <id>` to stderr, exit 0
   - no meta mutation, no events
4. if session exists:
   - send keys: **C-c only**
     - `tmux.SendKeys(sessionName, []tmux.Key{tmux.KeyCtrlC})`
   - on send-keys error: return existing tmux failure code; do not mutate meta; do not append event
   - mutate meta:
     - `store.UpdateMeta(..., func(m *RunMeta) { m.Flags.NeedsAttention = true })`
   - append event:
     - event name: `stop`
     - data includes at least:
       - `session_name`: sessionName
       - `keys`: `["C-c"]`
   - exit 0

notes:
- stop bypasses repo lock (per L0)
- meta mutation must be atomic (use existing `store.UpdateMeta`)
- event append may be best-effort per current events module, but for this PR:
  - if event append fails, return `E_PERSIST_FAILED` (do not print-and-continue)
  - meta mutation is not rolled back if event append fails; command returns error but leaves `needs_attention=true`

### `agency kill <id>`

behavior:
1. resolve repo + read meta for exact `run_id`:
   - if missing: `E_RUN_NOT_FOUND`
2. compute session name via `tmux.SessionName(run_id)`
3. check session existence:
   - if missing: print `no session for <id>` to stderr, exit 0; no event
4. if session exists:
   - call `tmux.KillSession(sessionName)`
   - on error: return existing tmux failure code
   - append event:
     - event name: `kill_session`
     - data includes:
       - `session_name`: sessionName
   - exit 0

notes:
- kill does not mutate meta flags in v1
- kill bypasses repo lock (per L0)
- if event append fails, return `E_PERSIST_FAILED`

---

## persistence

### meta.json
- stop (session existed and keys sent successfully):
  - set `flags.needs_attention = true`
- no other meta changes in this PR

### events.jsonl
events appended only on session-exists paths:
- `stop` (session exists, keys sent ok)
- `kill_session` (session exists, kill succeeded)

no events appended for noop cases (missing session), and no events for attach.

event envelope:
- use existing events schema conventions in this repo (caller fills schema_version/timestamp)
- ensure each appended line is valid JSON and ends with `\n`

---

## files to modify / create

### add
- `internal/commands/stop.go`
- `internal/commands/kill.go`
- tests:
  - `internal/commands/attach_test.go` (extend existing if present)
  - `internal/commands/stop_test.go`
  - `internal/commands/kill_test.go`

### modify
- `internal/commands/attach.go`
  - adopt `TmuxClient.HasSession` pre-check and return `E_SESSION_NOT_FOUND`
- `internal/cli/dispatch.go`
  - register `stop` and `kill` commands and wire dependencies (tmux client from pr-04a)
- `internal/errors/codes.go` (or equivalent)
  - add `E_SESSION_NOT_FOUND`

---

## dependency wiring constraints

- commands are plain functions today; maintain that style.
- add `tmuxClient internal/tmux.Client` parameter to attach/stop/kill command functions (do not introduce an app-wide deps struct in this PR).
- in `internal/cli/dispatch.go`, construct the real tmux client (pr-04a) once and pass it through.

this keeps PR minimal and avoids a broader DI refactor.

---

## tests

### automated tests (required): `go test ./...`

use a fake tmux client from pr-04a in all tests. tests must pass without tmux installed.

#### attach tests
- session exists:
  - fake `HasSession` returns true
  - assert `Attach` called with session name from `tmux.SessionName(runID)`
- session missing:
  - fake `HasSession` returns false
  - assert error code `E_SESSION_NOT_FOUND`
  - stderr contains `try: agency resume <id>`
  - stdout empty
- run missing:
  - assert `E_RUN_NOT_FOUND`

#### stop tests
- session missing:
  - fake `HasSession` false
  - assert exit success (nil error)
  - stderr contains `no session for <id>`
  - meta not mutated (NeedsAttention remains false)
  - events file unchanged
- session exists:
  - fake `HasSession` true
  - fake `SendKeys` captures keys
  - assert keys == [`KeyCtrlC`]
  - assert meta flags set NeedsAttention=true
  - assert one event appended with name `stop` and keys `["C-c"]`

#### kill tests
- session missing:
  - fake `HasSession` false
  - nil error; stderr message
  - no event appended
- session exists:
  - fake `HasSession` true
  - assert `KillSession` called
  - assert event `kill_session` appended

session naming in tests:
- expected session name is always derived via `tmux.SessionName(runID)` (no hardcoded prefix)

test setup:
- create temp `${AGENCY_DATA_DIR}` and set env var
- create minimal repo store state required for reading meta (reuse patterns from existing command tests)
- write a meta.json for the run into the store before invoking commands
- capture stdout/stderr via bytes.Buffer and assert exact strings where applicable

---

## guardrails

- do not change status derivation logic
- do not change session naming conventions (use `tmux.SessionName` from pr-04a)
- do not add new config schema features
- do not add prefix-based id resolution to attach/stop/kill
- stop/kill must not mutate git state
- attach must not mutate meta or events

---

## manual verification (post-merge of pr-04b)

1. create a run (slice 1), detach
2. `agency attach <id>` attaches
3. `agency stop <id>` while runner is active; verify:
   - meta shows needs_attention
   - event `stop` appended
4. `agency kill <id>`; verify:
   - `agency attach <id>` now fails with `E_SESSION_NOT_FOUND`
5. run a stop/kill on a run with no session; verify no-op stderr message and exit 0
