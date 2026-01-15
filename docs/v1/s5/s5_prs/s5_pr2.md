agency s5 pr 5.2 spec — meta + flags + events integration (+ archived workspace error)

goal

wire the s5 verify runner (pr 5.1) into run metadata and events, with deterministic flag semantics and a clear failure when the workspace is missing/archived.

this pr must not introduce the user-facing agency verify command yet (that’s pr 5.3).

⸻

dependencies
	•	pr 5.1 merged: internal verify runner exists and can execute scripts.verify, write verify_record.json, and write/overwrite logs/verify.log.

⸻

scope

in-scope
	•	add error code E_WORKSPACE_ARCHIVED
	•	add flags.needs_attention_reason to meta.json schema (additive) + store struct
	•	fix repo lock stale detection: stale iff pid not alive (age alone never steals lock)
	•	implement an internal “verify pipeline” entrypoint that:
	•	resolves <run_id> globally (cwd-independent)
	•	acquires repo lock for the run’s repo_id
	•	fails fast if worktree missing/archived
	•	emits verify_started and verify_finished events (best-effort)
	•	runs verify via the pr 5.1 runner (context-aware)
	•	updates meta.json atomically (last_verify_at, needs_attention semantics)
	•	if best-effort event append fails, record the message into verify_record.json.error (without polluting summary) by rewriting verify_record atomically

out-of-scope
	•	no new cli command / flags (agency verify is pr 5.3)
	•	no changes to tmux
	•	no changes to agency push / agency merge
	•	no github checks parsing
	•	no new storage backend
	•	no changes to tmux behavior

⸻

public surface area

new error code
	•	E_WORKSPACE_ARCHIVED: “run exists but worktree missing or archived; cannot verify”.

notes:
	•	keep existing E_WORKTREE_MISSING unchanged (used by resume today). E_WORKSPACE_ARCHIVED is used by the verify pipeline only.

schema additions (meta.json)

additive fields only:
	•	last_verify_at (string, rfc3339)
	•	flags.needs_attention_reason (string, omitempty)

allowed values (v1 enum):
	•	absent or "" (no attention reason)
	•	verify_failed
	•	stop_requested (reserved)
	•	user_marked (reserved)
	•	pr_not_mergeable (reserved)
	•	setup_failed (reserved)
	•	unknown (reserved)

v1 in this pr:
	•	only sets/clears verify_failed and "".

⸻

implementation plan (tight)

1) run resolution (cwd-independent)

use the same global resolution pattern as show/push:
	•	store.ScanAllRuns() to build the universe of runs from ${AGENCY_DATA_DIR}/repos/*/runs/*
	•	ids.ResolveRunRef(...) to map the user-supplied id/ref → (repo_id, run_id, run_dir)

failure:
	•	if run not found → E_RUN_NOT_FOUND (existing)

2) workspace existence predicate

after reading meta:
	•	worktree_path := meta.WorktreePath
	•	if worktree_path == "" → E_STORE_CORRUPT (existing) (meta invalid)
	•	archived := (meta.Archive != nil && meta.Archive.ArchivedAt != "") OR (os.Stat(worktree_path) is not-exists)
	•	if archived → E_WORKSPACE_ARCHIVED

3) repo locking

acquire repo lock using existing internal/lock/repo_lock.go.

rules:
	•	lock must be held from immediately before verify_started until after meta update completes, including any verify_record rewrite for error augmentation.
	•	stale detection must be pid-only: a lock is stale only if pid is not alive.

on lock failure:
	•	E_REPO_LOCKED (existing)

4) verify pipeline entrypoint (internal)

add a single internal function that pr 5.3 will call.

suggested signature (adjust to match repo conventions):

// VerifyRun executes scripts.verify for an existing run and updates meta+events.
// It must be cwd-independent; it resolves run via global store scan.
func VerifyRun(ctx context.Context, runRef string, timeout time.Duration) (*verify.Record, error)

requirements:
	•	default timeout passed in by caller (pr 5.3 sets default 30m)
	•	uses the pr 5.1 runner, passing a ctx with timeout
	•	obtains paths from store helpers where available (run dir, meta path, events path, log path, verify_record path)
	•	returns record + nil error whenever verify ran and produced a record (even if ok=false)
	•	returns error only for infra failures (lock, missing workspace, persistence failure)

5) events emission (best-effort)

append two events to ${run_dir}/events.jsonl using events.AppendEvent(...):
	•	verify_started:
	•	timeout_ms
	•	log_path
	•	verify_json_path (if known pre-run; else omit)
	•	verify_finished:
	•	ok
	•	exit_code
	•	timed_out
	•	cancelled
	•	duration_ms
	•	verify_json_path (if present)
	•	log_path
	•	verify_record_path

ordering:
	•	emit verify_started after lock acquisition and after workspace existence check.
	•	emit verify_finished after verify_record has been written and meta updated (or meta update attempted). if meta update fails, still best-effort append verify_finished.
	•	if verify runner fails to start but still writes a record, emit verify_finished (it is a finished attempt).

if append fails:
	•	do not fail the verify pipeline for event failure alone.
	•	collect failures from both verify_started and verify_finished appends
	•	write/update verify_record.json.error with a concise combined message:
	•	events append failed: <err1>; <err2>
	•	preserve any existing error by concatenating with ; .

6) meta updates (atomic)

update via store.UpdateMeta(repoID, runID, fn).

rules:
	•	last_verify_at is set to verify_record.finished_at only if verify_record.started_at is non-empty and the verify runner actually started.
	•	if verify result ok == true:
	•	if meta.Flags.NeedsAttention == true AND meta.Flags.NeedsAttentionReason == "verify_failed":
	•	set NeedsAttention=false
	•	set NeedsAttentionReason="" (omitempty => field omitted)
	•	otherwise: do not change attention fields
	•	if verify result ok == false (includes timeout/cancel/failure):
	•	set NeedsAttention=true
	•	set NeedsAttentionReason="verify_failed"

if meta update fails:
	•	return E_META_WRITE_FAILED / E_PERSIST_FAILED / E_INTERNAL (whichever your existing error taxonomy uses for update failures)
	•	also update verify_record.json.error to include meta update failed: <err> (best-effort rewrite)

⸻

files to change (expected)
	•	internal/errors/errors.go
	•	add E_WORKSPACE_ARCHIVED
	•	internal/lock/repo_lock.go
	•	update stale detection (pid-only)
	•	internal/lock/repo_lock_test.go
	•	update tests to match pid-only staleness
	•	internal/store/run_meta.go
	•	add NeedsAttentionReason string \json:“needs_attention_reason,omitempty”``
	•	ensure read/write round-trips
	•	internal/commands/ no new command in this pr
	•	new internal pipeline package or location consistent with repo:
	•	e.g. internal/verifyservice/service.go (or similar)
	•	tests:
	•	unit tests near store/status or verifyservice
	•	integration tests in whichever package currently hosts “temp repo” tests

⸻

behavior (given/when/then)

1) workspace missing

given run exists in global store and meta.worktree_path does not exist on disk
when verify pipeline runs
then it returns E_WORKSPACE_ARCHIVED, does not invoke the verify runner, and:
	•	writes verify_record.json with error="workspace archived" (exit_code=null)
	•	best-effort appends verify_started/verify_finished events
	•	does not update meta.json
note:
	•	diagnostic verify_skipped events can be considered later; out of scope for v1.

2) verify failure sets attention

given verify runner completes and produces verify_record.ok=false
when verify pipeline completes
then:
	•	meta.flags.needs_attention=true
	•	meta.flags.needs_attention_reason=“verify_failed”
	•	meta.last_verify_at updated
	•	verify_started and verify_finished events best-effort appended

3) verify success clears attention only when reason==verify_failed

given meta.flags.needs_attention=true and reason==“verify_failed”
and verify_record.ok=true
then meta clears to needs_attention=false, reason="" (omitempty => field omitted).

given meta.flags.needs_attention=true and reason!=“verify_failed”
and verify_record.ok=true
then meta attention fields are unchanged.

4) events append failure is non-fatal but recorded

given events append fails (e.g., permission issue)
when verify pipeline runs
then verify pipeline still returns success/failure based on verify outcome, but verify_record.error is updated to include the append failure message.

⸻

tests

unit (table-driven)
	•	meta attention update rules:
	•	ok clears only when reason==“verify_failed”
	•	failure always sets reason==“verify_failed”
	•	workspace predicate:
	•	missing worktree path → E_WORKSPACE_ARCHIVED
	•	verify_record error augmentation (if you implement record rewrite helper):
	•	preserves existing error and appends new message

integration (no tmux)

setup:
	•	create temp git repo
	•	create a real git worktree directory for a fake run
	•	write meta.json pointing to that worktree
	•	create minimal .agency/out/ in worktree
	•	provide a test verify script referenced by agency.json

cases:
	•	verify exits 0 → meta last_verify_at updated, needs_attention cleared only when reason=verify_failed, events.jsonl has 2 lines
	•	verify exits 1 → needs_attention set + reason verify_failed, events written
	•	delete worktree dir → E_WORKSPACE_ARCHIVED

test entrypoint:
	•	call the internal VerifyRun(...) function directly.

command:
	•	go test ./...

⸻

guardrails
	•	do not add agency verify command here
	•	do not touch tmux packages
	•	do not change status derivation logic (it can ignore needs_attention_reason for now)
	•	do not change verify runner core logic from pr 5.1 except to expose a clean API needed by the pipeline
