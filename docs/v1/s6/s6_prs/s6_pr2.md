# agency l4: pr-06b spec — verify runner + merge prechecks (no gh merge yet) (v1 mvp)

## goal

implement deterministic **verify evidence** + **merge prechecks** for `agency merge`, with a testable internal abstraction layer (`Exec`, `Clock`, `Store`) and strict error mapping. pr-06b **must not** call `gh pr merge` and **must not** archive.

deliverable: `agency merge <run_id>` runs prechecks + runs `scripts.verify` + (optionally) prompts on verify failure, then exits `E_NOT_IMPLEMENTED` after recording all evidence/events needed for pr-06c.

---

## non-goals

- no `gh pr merge`
- no archive/clean behavior
- no implicit `agency push`
- no pid-based runner detection
- no PR checks parsing/enforcement
- no branch deletion (local or remote)
- no interactive TUI work

---

## public surface area

### command

- `agency merge <run_id> [--squash|--merge|--rebase] [--force]`

### flags

- `--squash|--merge|--rebase`
  - parsed and validated in this PR
  - at most one allowed
  - if none set: default strategy = `squash`
  - if >1 set: `E_USAGE`
  - record resolved strategy in `merge_started` (meta field optional in v1)
- `--force`
  - bypasses only the verify-failed `[y/N]` prompt (still runs verify; still records failure)

---

## files created/modified

### global store (written)

- `${AGENCY_DATA_DIR}/repos/<repo_id>/runs/<run_id>/logs/verify.log` (overwrite)
- `${AGENCY_DATA_DIR}/repos/<repo_id>/runs/<run_id>/verify_record.json` (overwrite)

### global store (modified)

- `${AGENCY_DATA_DIR}/repos/<repo_id>/runs/<run_id>/meta.json` (update flags + timestamps + pr fields if discovered)
- `${AGENCY_DATA_DIR}/repos/<repo_id>/runs/<run_id>/events.jsonl` (append)

---

## internal architecture changes (required)

pr-06b MUST introduce an abstraction layer if not already present. do not scatter `exec.Command` across commands.

### Exec

```go
type Exec interface {
  Run(ctx context.Context, cwd string, name string, args ...string) (stdout, stderr string, exit int, err error)
}

requirements:
	•	cwd is applied (empty means inherit)
	•	returns:
	•	stdout, stderr as captured strings
	•	exit (process exit code; if unknown, set to 1)
	•	err (Go execution error; must still attempt to set exit)
	•	production implementation uses exec.CommandContext + buffers
	•	include a helper to open /dev/null for stdin when required (mac/linux only)
	•	include helper(s) to classify errors:
		- IsTimeout(err) (context deadline exceeded)
		- IsNotFound(err) (exec.ErrNotFound)

tests:
	•	provide FakeExec that matches by (cwd,name,args...) and returns pre-canned results in order

Clock

type Clock interface { NowUTC() time.Time }

	•	production uses time.Now().UTC()
	•	tests use fixed time

Store (persistence)

must centralize:
	•	load + validate meta.json
	•	atomic json writes (temp + rename)
	•	append jsonl events (create file if missing)
	•	ensure directories exist (runs/<id>/logs/)

recommended minimal surface:

type Store interface {
  LoadRunMeta(repoID, runID string) (*RunMeta, error)
  LoadRepoRecord(repoID string) (*RepoRecord, bool, error)
  SaveRunMeta(repoID, runID string, meta *RunMeta) error
  AppendEvent(repoID, runID string, ev Event) error
  RunLogPath(repoID, runID, name string) string // e.g. "verify.log"
  RunPath(repoID, runID string) string          // runs/<id> dir
}

constraints:
	•	writes are atomic where applicable (meta.json, verify_record.json)
	•	jsonl append is single-line per event; if append fails, command fails with E_PERSIST_FAILED
	•	all store writes must be under ${AGENCY_DATA_DIR} only
	•	store ensures parent directories exist before writes

GH adapter (required)

```go
type GH interface {
  AuthStatus(ctx context.Context, repo string) error
  PRViewByNumber(ctx context.Context, repo string, number int) (PRView, error)
  PRViewByHead(ctx context.Context, repo string, head string) (PRView, error)
}

type PRView struct {
  Number int    `json:"number"`
  URL    string `json:"url"`
  State  string `json:"state"`
  IsDraft bool  `json:"isDraft"`
  Mergeable string `json:"mergeable"`
  HeadRefName string `json:"headRefName"`
}
```

required JSON fields: all fields in PRView above must be present; missing/invalid -> E_GH_PR_VIEW_FAILED.
helper:
	•	IsNotFound(stderr string) bool
		- matches known gh strings like:
			* "no pull requests found"
			* "no pull requests found for branch"
		- used only to map head-lookup non-zero exit to E_NO_PR

⸻

precheck pipeline (deterministic order)

agency merge must perform prechecks in this order and fail fast with the listed error codes:
	1.	run exists + worktree present

	•	load meta for <run_id>
	•	ensure worktree_path exists on disk
	•	errors:
	•	run missing: E_RUN_NOT_FOUND
	•	worktree missing: E_WORKTREE_MISSING

	2.	origin exists (source-of-truth)

	•	prefer repo.json (if present) for origin_url + origin_host
	•	no freshness heuristic in v1; if repo.json exists, use it
	•	else run git -C <worktree> remote get-url origin
	•	if both missing: E_NO_ORIGIN

	3.	origin host capability (github.com required)

	•	parse hostname from origin URL
	•	accept only github.com (exact match)
	•	else: E_UNSUPPORTED_ORIGIN_HOST

	4.	gh authenticated

	•	run gh auth status
	•	if non-zero: E_GH_NOT_AUTHENTICATED

	5.	resolve repo owner/name for gh -R

	•	determine <owner>/<repo>:
	•	prefer repo_key if available in repo.json and is github:<owner>/<repo>
	•	else parse origin URL
	•	supported origin URL formats (v1):
	•	git@github.com:<owner>/<repo>.git (optional .git)
	•	https://github.com/<owner>/<repo>.git (optional .git)
	•	anything else: E_GH_REPO_PARSE_FAILED

	6.	PR resolution (must already exist)
attempt in order:

	•	if meta.pr_number present:
	•	gh pr view <num> -R <owner>/<repo> --json number,url,state,isDraft,mergeable,headRefName
	•	else:
	•	gh pr view --head <owner>:<branch> -R <owner>/<repo> --json number,url,state,isDraft,mergeable,headRefName
if neither yields a PR:
	•	E_NO_PR with hint run: agency push <id>

gh failure handling:
	•	if gh exits non-zero on head lookup and stderr matches “no PR found” (see IsNotFound): E_NO_PR
	•	if gh exits non-zero otherwise, JSON invalid, or required fields missing/unexpected: E_GH_PR_VIEW_FAILED
	•	include stderr in error message (truncate reasonably)

required fields:
	•	number (int)
	•	url (string)
	•	state (string)
	•	isDraft (bool)
	•	mergeable (string)
	•	headRefName (string)
	•	do not require headRepository fields in v1

persist (on successful PR resolve):
	•	write pr_number and pr_url into meta (if missing or changed)

	7.	PR state and mismatch checks

	•	require state == "OPEN" else:
	•	if state == "MERGED": not handled in pr-06b (pr-06c will implement idempotent path). for pr-06b: return E_PR_NOT_OPEN.
	•	if state == "CLOSED": E_PR_NOT_OPEN
	•	require isDraft == false else E_PR_DRAFT
	•	mismatch validation (v1 strict):
	•	require headRefName == <branch>
	•	else: E_PR_MISMATCH with hint to repair PR/meta

	8.	mergeability

	•	interpret mergeable:
	•	if MERGEABLE: ok
	•	if CONFLICTING: E_PR_NOT_MERGEABLE
	•	if UNKNOWN: retry 3x with backoff (1s, 2s, 2s), re-running gh pr view <num> -R <owner>/<repo> --json mergeable
	•	if still UNKNOWN: E_PR_MERGEABILITY_UNKNOWN
	•	any other value: E_GH_PR_VIEW_FAILED (unexpected field value)

	9.	remote head up-to-date

	•	git -C <worktree> fetch origin refs/heads/<branch>:refs/remotes/origin/<branch>; on failure: E_GIT_FETCH_FAILED
	•	compare:
	•	local = git -C <worktree> rev-parse HEAD
	•	remote = git -C <worktree> rev-parse refs/remotes/origin/<branch>
	•	if remote rev-parse fails: E_REMOTE_OUT_OF_DATE with hint “remote branch missing; run: agency push <id>”
	•	if sha differs: E_REMOTE_OUT_OF_DATE with hint “local head differs from origin/<branch>; run: agency push <id>”
	•	optional event data on failure: { "local_sha": "...", "remote_sha": "...", "remote_present": true|false }

⸻

verify runner (deterministic evidence)

after prechecks pass, agency MUST run verify (even if recently verified).

execution:
	•	script path from agency.json scripts.verify resolved relative to repo root -> absolute path
	•	run with:
	•	cwd = <worktree>
	•	stdin = /dev/null
	•	environment injection as specified by L0 (include CI=1, AGENCY_NONINTERACTIVE=1, AGENCY_RUN_ID, etc.)
	•	timeout: 30 minutes (1800000 ms)
notes:
	•	verify script stdin is /dev/null
	•	agency process still reads stdin for prompts

outputs:
	•	overwrite logs/verify.log with:
	•	header line: timestamp + command line + cwd
	•	stdout then stderr (or interleaved; choose one and keep stable)
	•	write/overwrite verify_record.json (schema_version “1.0”):
	•	run_id
	•	started_at, finished_at (UTC RFC3339)
	•	duration_ms
	•	timeout_ms (1800000)
	•	exit_code
	•	ok (exit_code == 0)
	•	log_path (absolute)
	•	script_path (absolute)
	•	script_output_path (absolute to <worktree>/.agency/out/verify.json if file exists after run; else empty string)

meta effects:
	•	set last_verify_at on completion (success or failure)
	•	on verify failure (non-zero exit or timeout):
	•	set flags.needs_attention=true

events (append in order):
	•	verify_started (include timeout_ms)
	•	verify_finished (include ok, exit_code, duration_ms)

error mapping:
	•	if verify exits non-zero:
	•	return E_SCRIPT_FAILED (even if --force later allows continuing in pr-06c; here it only affects prompt)
	•	if verify times out:
	•	return E_SCRIPT_TIMEOUT

prompting:
	•	if verify fails AND --force is NOT set:
	•	prompt on stderr: verify failed. continue anyway? [y/N] 
	•	read one line from stdin
	•	treat EOF/empty as “no”
	•	if “yes” (accept y or Y only):
	•	proceed to the “not implemented” termination below
	•	else:
	•	exit immediately with E_SCRIPT_FAILED or E_SCRIPT_TIMEOUT (whichever occurred)
	•	if --force is set:
	•	do not prompt
	•	proceed to termination below

persistence failure rules:
	•	if any persistence write fails (meta.json, verify_record.json, events append): return E_PERSIST_FAILED
	•	if events append fails after verify ran: still return E_PERSIST_FAILED (verify already executed)
	•	if meta save fails after events append: return E_PERSIST_FAILED and print paths for manual inspection
	•	best-effort to include stderr context; do not attempt further writes after a persistence failure

⸻

termination behavior (no merge yet)

after:
	•	prechecks completed
	•	verify executed
	•	(if needed) user accepted verify-fail continue

then:
	•	print to stderr:
	•	note: merge step not implemented in pr-06b; re-run after pr-06c lands
	•	exit non-zero with E_NOT_IMPLEMENTED

do not:
	•	prompt for typed merge confirmation (reserved for pr-06c)
	•	call gh pr merge
	•	archive or modify tmux sessions

⸻

locking
	•	acquire repo lock at start of agency merge
	•	print on lock acquisition to stdout: lock: acquired repo lock (held during verify/merge/archive)
	•	note: wording is fixed even though this PR won’t merge/archive yet
	•	hold lock through prechecks + verify + prompt + persistence
	•	release lock on exit (success or failure)

⸻

error codes (must be used as specified)

pr-06b introduces (if not already present):
	•	E_GIT_FETCH_FAILED
	•	E_GH_PR_VIEW_FAILED
	•	E_PR_MISMATCH
	•	E_GH_REPO_PARSE_FAILED
	•	E_PR_MERGEABILITY_UNKNOWN

and uses existing:
	•	E_USAGE
	•	E_NOT_IMPLEMENTED
	•	E_RUN_NOT_FOUND
	•	E_WORKTREE_MISSING
	•	E_NO_ORIGIN
	•	E_UNSUPPORTED_ORIGIN_HOST
	•	E_GH_NOT_AUTHENTICATED
	•	E_NO_PR
	•	E_PR_DRAFT
	•	E_PR_NOT_OPEN
	•	E_PR_NOT_MERGEABLE
	•	E_REMOTE_OUT_OF_DATE
	•	E_SCRIPT_FAILED
	•	E_SCRIPT_TIMEOUT
	•	E_PERSIST_FAILED

error output format (v1):
	•	first stderr line: error_code: E_...
	•	next: human message
	•	optional: hint: ...

⸻

events

append-only ${...}/events.jsonl. each event is one JSON object per line.

minimum required events for pr-06b:
	•	merge_started
	•	merge_prechecks_passed
	•	verify_started
	•	verify_finished
	•	verify_continue_prompted
	•	verify_continue_accepted | verify_continue_rejected

recommended data payloads:
	•	merge_started: { "run_id": "...", "strategy": "squash|merge|rebase", "force": true|false }
	•	merge_prechecks_passed: { "pr_number": 123, "pr_url": "...", "branch": "..."}
	•	verify_started: { "timeout_ms": 1800000 }
	•	verify_finished: { "ok": true|false, "exit_code": 0, "duration_ms": 12345 }
	•	verify_continue_*: { "answer": "y|n|empty" }

note: do not write merge_confirm_* or gh_merge_* events in pr-06b.

event ordering (required):
	1) append merge_started (resolved strategy + force)
	2) run prechecks
	3) append merge_prechecks_passed (pr_number, pr_url)
	4) append verify_started
	5) run verify
	6) append verify_finished
	7) if verify failed and prompted: append verify_continue_prompted then verify_continue_accepted|rejected

⸻

tests

unit tests (required)

use FakeExec + fixed Clock. do not call real git/gh/tmux.

must cover:
	•	mergeability:
	•	UNKNOWN 3x -> E_PR_MERGEABILITY_UNKNOWN
	•	CONFLICTING -> E_PR_NOT_MERGEABLE
	•	PR missing:
	•	meta has no pr_number; head lookup returns not found -> E_NO_PR
	•	head lookup non-zero with non-matching stderr -> E_GH_PR_VIEW_FAILED
	•	PR draft:
	•	isDraft true -> E_PR_DRAFT
	•	PR not open:
	•	state CLOSED -> E_PR_NOT_OPEN
	•	PR mismatch:
	•	headRefName != branch -> E_PR_MISMATCH
	•	gh pr view missing fields / invalid json -> E_GH_PR_VIEW_FAILED
	•	remote head check:
	•	fetch fails -> E_GIT_FETCH_FAILED
	•	remote ref missing (rev-parse fails) -> E_REMOTE_OUT_OF_DATE (remote_present=false)
	•	sha mismatch -> E_REMOTE_OUT_OF_DATE
	•	verify runner:
	•	verify exit 0 writes verify_record.json with ok true
	•	verify exit 1 sets needs_attention and (without –force, with stdin “N”) aborts with E_SCRIPT_FAILED
	•	verify exit 1 and stdin “y” proceeds to E_NOT_IMPLEMENTED
	•	verify timeout -> E_SCRIPT_TIMEOUT and needs_attention true
	•	verify with –force skips prompt and proceeds to E_NOT_IMPLEMENTED

prompt simulation requirement:
	•	factor prompt IO behind a small interface (or pass io.Reader/io.Writer) so unit tests can feed stdin.

integration tests (optional; behind env flag)
	•	create temp repo + fake scripts; do not call gh
	•	run verify runner directly (not full merge) to assert log/record writing

⸻

guardrails
	•	do not implement gh pr merge
	•	do not call archive pipeline
	•	do not call agency push
	•	never delete branches (local or remote)
	•	do not mutate parent working tree
	•	all persistence writes are atomic where applicable
	•	all subprocess calls go through Exec (no direct exec.Command in commands)
