# Agency S5 — PR 5.3 Spec: CLI command `agency verify`

## goal

ship the user-facing command:

```bash
agency verify <id> [--timeout <dur>]

wired to the existing S5 verify pipeline (PR 5.1 + 5.2), with stable UX output, correct exit behavior, and no changes to push/merge behavior.

⸻

scope

in-scope
	•	add CLI subcommand verify
	•	parse args/flags:
	•	required <id>
	•	optional --timeout <dur> (Go duration syntax), default 30m
	•	reject --timeout <= 0 as a usage error (exit 2)
	•	resolve run by <id> using global Agency storage (do not require cwd to be in repo)
	•	invoke the verify pipeline entrypoint from PR 5.2 with:
	•	run_id
	•	timeout
	•	context.Context for cancellation
	•	print minimal, predictable output (no log dumping)
	•	map pipeline results/errors to:
	•	stdout/stderr messages
	•	process exit code:
	•	0 for success
	•	1 for domain failures (with an E_* code printed to stderr)
	•	2 for usage errors (bad args/flags)
	•	ensure agency ls reflects flags.needs_attention after verify only if ls does not already surface it (display-only change, no schema changes)
	•	use standard data dir resolution (XDG + AGENCY_DATA_DIR override) for all run resolution

out-of-scope
	•	any changes to verify runner logic (process groups, precedence, parsing)
	•	any changes to repo lock semantics (must reuse existing lock helper)
	•	any changes to agency push / agency merge
	•	adding --json output
	•	reading/parsing GitHub checks
	•	tmux involvement
	•	new storage formats / sqlite / db

⸻

public surface area

new command

agency verify <id> [--timeout <dur>]

	•	<id>: required run id (string)
	•	--timeout <dur>:
	•	Go duration format (30m, 90s, etc.)
	•	default: 30m
	•	invalid/<=0: usage error (exit 2)

help text (required)
	•	verify help must clearly state:
	•	runs repo’s scripts.verify for the run
	•	does not require being in the repo directory
	•	writes verify_record.json and verify.log
	•	updates run flags (needs_attention)

⸻

UX output contract (v1)

on success (verify ok)
	•	stdout (single line, exact shape):

ok verify <id> record=<abs_path_to_verify_record.json> log=<abs_path_to_verify.log>

	•	stderr: empty (unless non-fatal warnings are already printed globally elsewhere; avoid adding new ones)
	•	exit code: 0

on failure (verify ok=false)
	•	stdout: empty
	•	stderr (single line, exact shape):

E_SCRIPT_FAILED: verify failed (<reason>) record=<abs_path> log=<abs_path>

Where <reason> is one of:
	•	exit <N> (when exit_code is non-zero)
	•	timed out
	•	cancelled
	•	exec failed (failed to start process)
	•	workspace archived (worktree missing)
	•	exit code: 1 (domain failure)
note:
	•	<reason> must be derived from the VerifyRecord fields (timed_out/cancelled/exit_code/error), not from stderr parsing.

on timeout
	•	stderr must use E_SCRIPT_TIMEOUT:

E_SCRIPT_TIMEOUT: verify timed out record=<abs_path> log=<abs_path>

	•	exit code: 1

on usage errors

Examples:
	•	missing <id>
	•	unparseable --timeout
	•	--timeout 0 / negative

Behavior:
	•	print command usage/help to stderr (framework default is fine)
	•	exit code: 2
	•	do not write any run state (no verify_record, meta, events, or logs)

on lock held
	•	stdout: empty
	•	stderr:

E_REPO_LOCKED: repo is locked (<details>) 

Include lock holder pid + age when available; if unknown, use pid=? age=?.
	•	exit code: 1

paths in output:
	•	print record= and log= paths deterministically even if files do not exist (e.g., archived workspace or log open failure).

⸻

behavior requirements

1) run resolution (must work from anywhere)

Given a valid run id <id>,
when agency verify <id> is invoked from any directory,
then it must locate:
	•	the run via the existing global resolver (store.ScanAllRuns + ids.ResolveRunRef)
	•	meta.worktree_path

If the run cannot be found:
	•	fail with E_RUN_NOT_FOUND (stderr includes the id)
	•	exit code 1

2) workspace archived/missing

If meta.worktree_path does not exist on disk:
	•	fail with E_WORKSPACE_ARCHIVED
	•	exit code 1
	•	do not attempt to run scripts
	•	still write verify_record.json with error="workspace archived" (exit_code=null)
	•	best-effort append verify_started/verify_finished events
	•	log path is still printed (even if no log file was created)

3) cancellation

If user hits Ctrl-C while verify is running:
	•	command must cancel the pipeline context
	•	pipeline will handle signalling and writing verify_record/meta/events per S5
	•	CLI must print failure form with reason cancelled
	•	exit code 1 (E_SCRIPT_FAILED)

4) lock integration

agency verify must:
	•	acquire repo lock for the duration of pipeline execution
	•	if lock held and pid is live → E_REPO_LOCKED, exit 1
	•	stale lock handling must reuse the existing lock helper (do not implement a second stale check in CLI)

5) no repo cwd requirements

agency verify must not call git rev-parse or require cwd inside repo.
All resolution is via run metadata + global storage.

6) pipeline return contract

CLI must assume:
	•	pipeline returns (record, nil) when a verify attempt produced a VerifyRecord (even if ok=false, timed_out, cancelled)
	•	pipeline returns error only for infrastructure failures (lock, missing run, persistence failure)
	•	on E_WORKSPACE_ARCHIVED, pipeline may still return a record (record != nil); CLI should use it for output if present
CLI chooses E_SCRIPT_TIMEOUT vs E_SCRIPT_FAILED based on record.timed_out/cancelled.

⸻

files to modify (tight)

adjust paths to match your repo’s layout; the important constraint is what changes, not the exact folders.

allowed
	•	command registration / wiring:
	•	cmd/agency/main.go (or equivalent root command)
	•	cmd/agency/verify.go (new) or internal/cli/verify.go
	•	pipeline call site (thin wrapper only):
	•	internal/app/verify_command.go (optional) — should contain no new logic, just adapt CLI args to pipeline API
	•	error mapping / exit code mapping:
	•	existing central error mapper file (e.g., internal/cli/errors.go)
	•	(optional) minimal UX integration in ls:
	•	only if agency ls currently does not surface flags.needs_attention
	•	changes must be limited to display derivation, not storage schema

not allowed
	•	modifying verify runner core (PR 5.1)
	•	modifying meta/events write semantics (PR 5.2)
	•	modifying push/merge code paths
	•	adding new flags other than --timeout

⸻

tests

required (minimum)
	•	CLI-level integration test (preferred if your project already tests commands)
	•	create temp ${AGENCY_DATA_DIR} fixture
	•	create a fake repo + worktree + meta.json matching an existing run (reuse integration harness from PR 5.2)
	•	run agency verify <id> --timeout 30s
	•	assert:
	•	exit code 0 on pass / 1 on fail / 2 on usage error
	•	stdout/stderr exact shape for pass/fail
	•	verify_record.json written on domain runs (pass/fail/timeout/cancel)
	•	verify_record.json written on workspace-archived failure
	•	usage error test (do not assert full usage text; assert exit code 2 and non-empty stderr)
	•	agency verify with no args exits 2
	•	agency verify <id> --timeout 0 exits 2

If your repo doesn’t have CLI integration tests:
	•	add a small internal test that calls the verify command handler function directly with injected stdout/stderr buffers and asserts outputs + returned exit code.

⸻

guardrails
	•	do not change verify semantics already locked in PR 5.1 + 5.2
	•	do not add new storage files beyond those already specified in S5
	•	do not add --json output or extra commands
	•	keep output minimal and stable (one line)

⸻

demo (manual)
	1.	create a run (S1) in some repo; ensure scripts.verify exists.
	2.	run:

agency verify <id>

	3.	confirm:

	•	verify_record.json exists under ${AGENCY_DATA_DIR}/repos/<repo_id>/runs/<id>/
	•	logs/verify.log exists and is overwritten each run
	•	agency ls reflects needs_attention on failures

	4.	force a failure (make verify script exit 1) and confirm:

	•	stderr shows E_SCRIPT_FAILED...
	•	exit code is 1
	•	needs_attention set with reason verify_failed
