# Agency S5: Verify Recording (`agency verify`)

## goal

add a standalone `agency verify <id>` command that runs the repo’s required `scripts.verify`, records a canonical verification record, and updates run metadata/status flags deterministically.

---

## scope

### in-scope

- new command: `agency verify <id> [--timeout <dur>]`
- run `scripts.verify` outside tmux with non-interactive constraints
- capture stdout/stderr to per-run logs
- write an agency-owned `verify_record.json` (canonical evidence)
- consume optional `.agency/out/verify.json` structured output (precedence rules)
- update `meta.json`:
  - `last_verify_at`
  - `flags.needs_attention` and `flags.needs_attention_reason`
- append events to `events.jsonl`
- acquire repo lock for the duration of verification
- cancellation behavior (SIGINT/SIGKILL) is defined and recorded

### non-scope

- no changes to `agency push` behavior (push does not run verify)
- no PR checks parsing or enforcement (GitHub checks are out of scope)
- no transcript replay / runner interaction
- no “auto verify” triggers
- no new scripts beyond required `setup/verify/archive`
- no new storage backends (continue using `${AGENCY_DATA_DIR}` JSON files)
- no changes to merge semantics in this slice (merge will still run verify later in S6)

---

## public surface area

### commands

#### `agency verify <id> [--timeout <dur>]`

Runs the repo’s `scripts.verify` for an existing run, records results, and updates flags.

- `<id>`: required run id
- `--timeout <dur>`: optional override. default `30m` (matches L0). accepts Go duration format (`30m`, `10m`, `90s`).

### flags and env

no new environment variables beyond the L0 script contract. this slice relies on existing env injection.

---

## files created / modified

### global storage (under `${AGENCY_DATA_DIR}`)

For run `<run_id>` in repo `<repo_id>`:

- `${AGENCY_DATA_DIR}/repos/<repo_id>/runs/<run_id>/verify_record.json` (NEW, canonical)
- `${AGENCY_DATA_DIR}/repos/<repo_id>/runs/<run_id>/logs/verify.log` (NEW/updated, append or overwrite per run; see behavior)
- `${AGENCY_DATA_DIR}/repos/<repo_id>/runs/<run_id>/meta.json` (UPDATED)
- `${AGENCY_DATA_DIR}/repos/<repo_id>/runs/<run_id>/events.jsonl` (UPDATED, append-only)

### workspace-local (`<worktree>/.agency/`)

Optional input file (already defined in L0; produced by verify script):
- `<worktree>/.agency/out/verify.json` (READ ONLY by agency)

No new workspace-local files are required beyond those in L0.

---

## schema changes

### `meta.json` (UPDATED)

Add the following fields if not already present (additive, v1):

- `last_verify_at` (RFC3339 timestamp string)
- `flags.needs_attention` (boolean)
- `flags.needs_attention_reason` (string enum)

#### `flags.needs_attention_reason` allowed values (v1)

- `verify_failed`
- `stop_requested`
- `user_marked`
- `pr_not_mergeable`
- `setup_failed`
- `unknown`

v1 usage in this slice:
- only `verify_failed` is set/cleared by `agency verify`.
- other reasons are reserved for other commands/slices.

### `verify_record.json` (NEW; public contract)

Path:
- `${AGENCY_DATA_DIR}/repos/<repo_id>/runs/<run_id>/verify_record.json`

Schema (v1):

```json
{
  "schema_version": "1.0",
  "repo_id": "string",
  "run_id": "string",
  "script_path": "string",
  "started_at": "RFC3339Nano string",
  "finished_at": "RFC3339Nano string",
  "duration_ms": 12345,
  "timeout_ms": 1800000,
  "timed_out": false,
  "cancelled": false,
  "exit_code": 0,
  "signal": "string|null",
  "error": "string|null",
  "ok": true,
  "verify_json_path": "string|null",
  "log_path": "string",
  "summary": "string"
}

Notes:
	•	exit_code is an integer or null.
	•	exit_code is null if no exit code is available (e.g., failed to start).
	•	signal is a string like "SIGKILL" when terminated by signal, otherwise null.
	•	error is a human-readable string for failures to start (e.g., "exec failed"), otherwise null.
	•	timed_out and cancelled are mutually exclusive; never both true.
	•	cancelled is true iff the user interrupted agency verify; timed_out is true iff the agency timeout fired.
	•	verify_json_path is null if absent.
	•	summary is derived for human display:
	•	prefer verify.json.summary if present/valid
	•	else a generic message (e.g., "verify succeeded" / "verify failed (exit 1)" / "verify timed out")
	•	if verify.json is invalid, record the parse/validation message in error (not summary)
	•	script_path is the resolved value from agency.json (exact string executed), not a realpath.

events.jsonl (UPDATED)

Add event(s) emitted by this slice:
	•	verify_started
	•	verify_finished

Event data fields (recommended):
	•	timeout_ms
	•	log_path
	•	verify_json_path (if any)
	•	ok
	•	exit_code
	•	timed_out
	•	cancelled
	•	duration_ms

⸻

behaviors (given/when/then)

0) ok derivation precedence

	•	if timed_out or cancelled => ok=false
	•	else if exit_code is null => ok=false
	•	else if exit_code != 0 => ok=false
	•	else if verify.json valid => ok=verify.json.ok
	•	else => ok=true

1) success via exit code

given a run exists and scripts.verify exits 0, and no <worktree>/.agency/out/verify.json exists
when agency verify <id> is executed
then
	•	verify.log is written
	•	verify_record.json.ok = true
	•	meta.json.last_verify_at is updated
	•	if meta.flags.needs_attention_reason == "verify_failed", clear needs_attention and the reason (or set reason to unknown); otherwise do not modify other attention reasons
	•	emit verify_started and verify_finished events

2) failure via exit code

given scripts.verify exits non-zero, and no verify.json exists
when agency verify <id> runs
then
	•	verify_record.json.ok = false
	•	set meta.flags.needs_attention = true
	•	set meta.flags.needs_attention_reason = "verify_failed"
	•	update last_verify_at
	•	logs + events written

3) verify.json wins over exit code (valid verify.json)

given scripts.verify exits 0, and <worktree>/.agency/out/verify.json exists and is valid with "ok": false
when agency verify <id> runs
then
	•	verify_record.json.ok = false (verify.json wins)
	•	set needs_attention + reason "verify_failed"

4) invalid verify.json falls back to exit code

given verify.json exists but is invalid JSON or missing required keys
when agency verify <id> runs
then
	•	treat verify.json as absent for ok derivation
	•	set verify_json_path to the file path for transparency
	•	record a parse/validation error message in verify_record.json.error

5) timeout overrides all

given scripts.verify does not complete before timeout
when agency verify <id> runs
then
	•	kill the process (SIGINT then SIGKILL; see cancellation)
	•	verify_record.json.timed_out = true
	•	verify_record.json.ok = false (timeout overrides verify.json)
	•	set needs_attention with reason "verify_failed"
	•	logs + events written

6) user cancellation (ctrl-c) overrides all

given user interrupts agency verify (SIGINT to agency) while verify is running
when agency verify <id> handles cancellation
then
	•	forward SIGINT to verify process group, wait a short grace period, then SIGKILL
	•	write verify_record.json.cancelled = true
	•	verify_record.json.ok = false
	•	set needs_attention reason "verify_failed"
	•	emit verify_finished event with cancelled=true

7) repo locking

given another mutating command holds ${AGENCY_DATA_DIR}/repos/<repo_id>/.lock
when agency verify <id> is invoked
then
	•	exit with E_REPO_LOCKED without modifying run state

given lock file exists but PID is stale
when agency verify <id> is invoked
then
	•	treat lock as stale and proceed (same stale detection rules as L0: lock file PID not alive)

8) workspace existence

given a run exists but its worktree is missing or archived
when agency verify <id> is invoked
then
	•	fail with E_WORKSPACE_ARCHIVED and a clear message (e.g., "cannot verify archived run")

⸻

persistence

writes
	•	verify_record.json written atomically (write temp + rename)
	•	meta.json updated atomically
	•	events.jsonl appended (best-effort; failure to append should not lose verify_record/meta updates)
	•	if events.jsonl append fails, emit a warning to stderr and include it in verify_record.json.error
	•	last_verify_at updates only if verify actually started

log file policy
	•	${...}/logs/verify.log:
	•	v1 recommendation: overwrite per verify run (simpler, avoids unbounded growth)
	•	record started_at in verify_record.json; logs are overwritten
	•	write a short header (timestamp + command + cwd) matching setup.log style

⸻

tests

automated (minimum)

unit tests (table-driven)
	1.	ok derivation precedence:

	•	(timed_out=true) => ok=false regardless of verify.json/exit_code
	•	(cancelled=true) => ok=false regardless
	•	(exit_code=null) => ok=false regardless of verify.json
	•	(exit_code!=0) => ok=false regardless of verify.json
	•	(exit_code==0 + verify.json valid) => ok=verify.json.ok
	•	(verify.json invalid/absent) => ok=(exit_code==0)

	2.	needs_attention update rules:

	•	verify ok clears needs_attention only if reason == verify_failed
	•	verify fail sets needs_attention and reason verify_failed
	•	verify ok does not clear if reason != verify_failed

	3.	verify.json parsing:

	•	valid envelope
	•	invalid json
	•	missing required fields

integration tests (temp repo + scripts)
Create a temp git repo with minimal agency.json and a run meta/worktree, and point scripts.verify to test scripts:
	•	exits 0
	•	exits 1
	•	writes verify.json ok=false but exits 0
	•	sleeps past timeout

Assert:
	•	verify_record.json exists and fields match expectations
	•	meta.json updated correctly (last_verify_at, flags)
	•	verify.log exists

manual
	•	create a run (S1), produce commits, then run:
	•	agency verify <id>
	•	agency show <id> and confirm verify details
	•	agency ls shows “needs attention” on failure

⸻

guardrails
	•	do not change agency push behavior (no implicit verify)
	•	do not introduce new storage formats (sqlite/db out of scope)
	•	do not attempt to parse GitHub checks
	•	do not run scripts inside tmux runner pane
	•	do not add new commands beyond agency verify (and internal helpers)

⸻
