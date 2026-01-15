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
  "started_at": "RFC3339 string",
  "finished_at": "RFC3339 string",
  "duration_ms": 12345,
  "timeout_ms": 1800000,
  "timed_out": false,
  "cancelled": false,
  "exit_code": 0,
  "ok": true,
  "verify_json_path": "string|null",
  "log_path": "string",
  "summary": "string"
}

Notes:
	•	exit_code is -1 if the process did not produce an exit code (e.g., killed before start completes).
	•	verify_json_path is null if absent.
	•	summary is derived for human display:
	•	prefer verify.json.summary if present/valid
	•	else a generic message (e.g., "verify succeeded" / "verify failed (exit 1)" / "verify timed out")

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

given scripts.verify exits non-zero, and verify.json exists valid with "ok": true
when agency verify <id> runs
then
	•	verify_record.json.ok = true (verify.json wins)
	•	do not set needs_attention due to verify
	•	record the non-zero exit code in verify_record.json.exit_code for debugging

4) invalid verify.json falls back to exit code

given verify.json exists but is invalid JSON or missing required keys
when agency verify <id> runs
then
	•	treat verify.json as absent for ok derivation
	•	set verify_json_path to the file path for transparency, but mark summary accordingly (e.g., "verify.json invalid; used exit code")

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
	•	treat lock as stale and proceed (same stale detection rules as prior slices)

⸻

persistence

writes
	•	verify_record.json written atomically (write temp + rename)
	•	meta.json updated atomically
	•	events.jsonl appended (best-effort; failure to append should not lose verify_record/meta updates)

log file policy
	•	${...}/logs/verify.log:
	•	v1 recommendation: overwrite per verify run (simpler, avoids unbounded growth)
	•	include a timestamp header line at the top to preserve provenance
	•	(alternatively append; if you append, ensure separators)

⸻

tests

automated (minimum)

unit tests (table-driven)
	1.	ok derivation precedence:

	•	(timed_out=true) => ok=false regardless of verify.json/exit_code
	•	(cancelled=true) => ok=false regardless
	•	(verify.json valid) => ok=verify.json.ok regardless of exit_code
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

open questions (must be answered in implementation)
	•	grace period for SIGINT before SIGKILL (suggest: 5s)
	•	whether verify.log overwrites or appends (spec prefers overwrite in v1)
	•	exact error messaging formatting (must include actionable next step)
