# agency l2: slice 06 — merge + archive (v1 mvp)

## goal

finish the core loop: merge an **already-existing** github pr for a run (after verification + explicit human confirmation), then archive the workspace (scripts + tmux + worktree cleanup) while retaining metadata/logs.

## scope

### in-scope

- `agency merge <run_id>`:
  - requires a pre-existing pr on GitHub for the run; pr discovery only resolves an existing pr
  - prechecks: run exists, worktree present, origin host `github.com`, gh authed, pr open and not draft, mergeability known+ok, fetch origin branch succeeds, remote head up-to-date
  - always runs `scripts.verify` and records evidence (even if recently verified)
  - if verify fails: prompt to continue; `--force` bypasses this prompt only
  - requires explicit human confirmation (type `merge`)
  - merges via `gh pr merge` with explicit strategy flag (`--squash|--merge|--rebase`)
  - on success: archives (runs `scripts.archive`, kills tmux session, deletes worktree)
  - writes events + updates `meta.json`

- `agency clean <run_id>`:
  - archives without merging
  - marks run abandoned after successful archive
  - runs `scripts.archive`, kills tmux, deletes worktree
  - retains metadata/logs

- idempotency:
  - if pr is already merged: `agency merge` should become “archive only” (mark merged + archive), not an error.

### out-of-scope

- auto-push / implicit `agency push` during merge
- auto-rebase, auto-conflict resolution
- pr checks parsing / enforcement
- headless runs / background execution
- pid-based runner detection
- garbage collection of archived metadata (`agency gc`)
- enterprise github hosts / custom gh hosts

---

## public surface area added/changed

### new/changed commands

- `agency merge <run_id> [--squash|--merge|--rebase] [--force]`
- `agency clean <run_id>` (if not already implemented)

### new flags (merge)

- `--squash` (merge strategy)
- `--merge` (merge strategy)
- `--rebase` (merge strategy)
- `--force`:
  - bypasses only the “verify failed, continue?” prompt
  - **does not** bypass: missing pr, non-mergeable pr, unknown mergeability, unsupported origin host, remote out-of-date, pr not open, gh auth failure

merge strategy rules (v1):
- at most one of `--squash|--merge|--rebase` may be set
- if none provided: default to `--squash`
- if more than one is provided: fail `E_USAGE`

---

## files created/modified

### modified (global store)

- `${AGENCY_DATA_DIR}/repos/<repo_id>/runs/<run_id>/meta.json` (updated)
- `${AGENCY_DATA_DIR}/repos/<repo_id>/runs/<run_id>/events.jsonl` (append; create if missing)

### written/overwritten (global store)

- `${AGENCY_DATA_DIR}/repos/<repo_id>/runs/<run_id>/logs/verify.log` (overwrite)
- `${AGENCY_DATA_DIR}/repos/<repo_id>/runs/<run_id>/logs/archive.log` (overwrite)
- `${AGENCY_DATA_DIR}/repos/<repo_id>/runs/<run_id>/logs/merge.log` (overwrite)
- `${AGENCY_DATA_DIR}/repos/<repo_id>/runs/<run_id>/verify_record.json` (overwrite)

notes:
- `events.jsonl` is canonical at `${AGENCY_DATA_DIR}/repos/<repo_id>/runs/<run_id>/events.jsonl`; if not created in earlier slices, s6 must create it on first append

### deleted (on successful archive)

- `${AGENCY_DATA_DIR}/repos/<repo_id>/worktrees/<run_id>/` (entire directory)
- tmux session `agency_<run_id>` (killed if exists)

---

## new error codes (if not already defined)

- `E_PR_MERGEABILITY_UNKNOWN` — gh reports mergeable as `UNKNOWN` after retries
- `E_REMOTE_OUT_OF_DATE` — local head sha != origin/<branch> sha (requires `agency push`)
- `E_ARCHIVE_FAILED` — archive step failed (script failure and/or deletion failure)
- `E_WORKSPACE_ARCHIVED` — merge/clean called after workspace already archived (optional; otherwise reuse `E_WORKTREE_MISSING`)
- `E_GIT_FETCH_FAILED` — failed to fetch remote branch before head check
- `E_PR_DRAFT` — pr is a draft
- `E_GH_PR_VIEW_FAILED` — `gh pr view --json ...` failed or returned unexpected fields/values
- `E_PR_MISMATCH` — resolved PR does not match expected branch
- `E_GH_REPO_PARSE_FAILED` — failed to parse owner/repo from origin
- `E_GH_PR_MERGE_FAILED` — gh merge failed or merge state could not be confirmed
- `E_ABORTED` — user declined confirmation / wrong token
- `E_NOT_INTERACTIVE` — command requires an interactive TTY

notes:
- keep existing codes: `E_NO_PR`, `E_PR_NOT_OPEN`, `E_PR_NOT_MERGEABLE`, `E_UNSUPPORTED_ORIGIN_HOST`, `E_GH_NOT_AUTHENTICATED`, etc.

---

## behaviors (given/when/then)

### lock scope

- acquire repo lock at command start
- hold lock through verify/merge/archive attempts (if any)
- release lock on command termination (success or failure)

### merge: happy path

given:
- run exists, worktree present
- pr exists, is open, and is not a draft
- origin host is `github.com`
- gh authenticated
- remote branch is up to date with local head
- mergeable is `MERGEABLE`

when:
- user runs `agency merge <id> --squash`

then:
- agency acquires repo lock
- agency runs `scripts.verify` (timeout 30m), writes verify evidence
- agency prompts: `confirm: type 'merge' to proceed: `
- accept only `strings.TrimSpace(input) == "merge"`; anything else aborts
- on confirmation, agency runs `gh pr merge` with the chosen strategy
- on successful merge, agency runs `scripts.archive` (timeout 5m)
- agency kills tmux session if exists
- agency deletes worktree via `git worktree remove --force <path>` (if that fails, fallback to `rm -rf` only when `<path>` is under `${AGENCY_DATA_DIR}/repos/<repo_id>/worktrees/`)
- updates meta:
  - `archive.merged_at` set (UTC RFC3339) after gh confirms merged
  - `archive.archived_at` set (UTC RFC3339) after worktree deletion succeeds
  - clears any transient flags only if you already have that mechanic (optional)
- appends events (see “events” section)
- exits 0

### merge: verify fails, user aborts

given verify exits non-zero

when:
- `agency merge <id>` is run without `--force`

then:
- agency prompts: `verify failed. continue anyway? [y/N]`
- if user answers no/empty:
  - do not merge
  - do not archive
  - set `flags.needs_attention=true`
  - exit non-zero with `E_SCRIPT_FAILED` (or `E_VERIFY_FAILED` if you add it later)

### merge: verify fails, user forces proceed

given verify fails

when:
- user runs `agency merge <id> --force`

then:
- no “continue anyway?” prompt
- still requires the final `merge` confirmation prompt
- merge proceeds; archive proceeds
- events record verify failure + forced continuation

### interactivity

- `agency merge` and `agency clean` require an interactive TTY for confirmation prompts
- if stdin/stderr are not TTYs: return `E_NOT_INTERACTIVE` and perform no writes

### merge: pr missing

given pr cannot be resolved by (in order):
1. `meta.pr_number` → `gh pr view <number> -R <owner>/<repo>`
2. `gh pr view --head <owner>:<branch> -R <owner>/<repo>` (branch is the run's head branch)
3. otherwise: no pr exists for this run
note: if the remote branch no longer exists, treat this as no pr found
note: a failed or schema-mismatched `gh pr view --json number,url,state,isDraft,mergeable` is `E_GH_PR_VIEW_FAILED`, not `E_NO_PR`

when:
- `agency merge <id>` is run

then:
- fail with `E_NO_PR`
- print hint: `run: agency push <id>`
- no verify, no merge, no archive
- do not create a pr here

### merge: pr not open

given pr exists but state is `MERGED` or `CLOSED`, or `isDraft` is true

when/then:
- if `MERGED`:
  - treat as idempotent: skip gh merge, set `archive.merged_at` (if missing), proceed to archive
  - exit 0 if archive succeeds
- if `CLOSED` (unmerged):
  - fail `E_PR_NOT_OPEN`
  - no archive (user decision)
- if `DRAFT`:
  - fail `E_PR_DRAFT`
  - no archive (user decision)

### merge: mergeability unknown

given:
- `gh pr view -R <owner>/<repo> --json number,url,state,isDraft,mergeable,headRefName` returns `UNKNOWN` for `mergeable` (and state/isDraft checks already passed)

when:
- `agency merge <id>` is run

then:
- retry mergeability check 3 times with short backoff (e.g. 1s, 2s, 2s)
- if still `UNKNOWN`: fail `E_PR_MERGEABILITY_UNKNOWN`
- no verify, no merge, no archive

### merge: gh pr view schema mismatch

given:
- `gh pr view --json number,url,state,isDraft,mergeable` fails, returns non-json, omits any required field, or returns an unexpected value for `state`/`mergeable` (expected: `state` in {`OPEN`,`CLOSED`,`MERGED`}, `mergeable` in {`MERGEABLE`,`CONFLICTING`,`UNKNOWN`})

when:
- `agency merge <id>` is run

then:
- fail `E_GH_PR_VIEW_FAILED`
- capture stderr in error message/log output
- no verify, no merge, no archive

### merge: remote out of date

given:
- `git fetch origin refs/heads/<branch>:refs/remotes/origin/<branch>` fails

when:
- `agency merge <id>` is run

then:
- fail `E_GIT_FETCH_FAILED`
- no verify, no merge, no archive

### merge: remote out of date

given:
- `git fetch origin refs/heads/<branch>:refs/remotes/origin/<branch>` succeeded
- `git rev-parse HEAD` != `git rev-parse origin/<branch>`

when:
- `agency merge <id>` is run

then:
- fail `E_REMOTE_OUT_OF_DATE`
- hint: `run: agency push <id>`
- no verify, no merge, no archive

### clean: happy path

given:
- run exists
- worktree present

when:
- `agency clean <id>`

then:
- acquire repo lock
- set `flags.abandon_requested=true` (optional)
- run `scripts.archive` (timeout 5m)
- kill tmux session if exists
- delete worktree via `git worktree remove --force <path>` (if that fails, fallback to `rm -rf` only when `<path>` is under `${AGENCY_DATA_DIR}/repos/<repo_id>/worktrees/`)
- on successful archive:
  - set `flags.abandoned=true`
  - set `archive.archived_at`
- append events
- exit 0

### archive failure semantics (merge or clean)

archive has three independent failure points:
1) archive script non-zero
2) tmux kill fails (session missing is not failure)
3) worktree deletion fails

rules:
- attempt all steps regardless of earlier failures
- if any failure occurs:
  - append `archive_failed` event with details
  - do **not** set `archive.archived_at` unless deletion succeeded
- return `E_ARCHIVE_FAILED` (even if merge succeeded; message should note merge ok but archive failed)
- do not `rm -rf` unless the worktree path is under `${AGENCY_DATA_DIR}/repos/<repo_id>/worktrees/`

---

## persistence

### meta.json updates

`agency merge`:
- before verify: update `last_verify_at` only after verify completes
- on verify completion: write/overwrite `verify_record.json`
- on successful merge:
  - set `archive.merged_at`
- on successful archive:
  - set `archive.archived_at`
- always update `updated_at` if you add it later (optional)

`agency clean`:
- set `flags.abandon_requested=true` (optional)
- on successful archive:
  - set `flags.abandoned=true`
  - set `archive.archived_at`

### verify_record.json (written by agency)

schema (v1):
```json
{
  "schema_version": "1.0",
  "run_id": "<run_id>",
  "started_at": "2025-01-09T12:34:56Z",
  "finished_at": "2025-01-09T12:35:56Z",
  "exit_code": 0,
  "ok": true,
  "duration_ms": 12345,
  "log_path": "<absolute path to logs/verify.log>",
  "script_path": "<absolute path to scripts/verify>",
  "timeout_ms": 1800000,
  "script_output_path": "<absolute path to .agency/out/verify.json or empty>"
}
```

### events.jsonl

required events appended (minimum set; keep stable strings):
- `merge_started`
- `merge_prechecks_passed`
- `verify_started`
- `verify_finished` (include {ok, exit_code, duration_ms})
- `verify_continue_prompted`
- `verify_continue_accepted` / `verify_continue_rejected`
- `merge_confirm_prompted`
- `merge_confirmed`
- `gh_merge_started`
- `gh_merge_finished` (include {ok, pr_number, pr_url})
- `merge_finished` (include {ok, error_code?})
- `archive_started`
- `archive_finished` (include {ok})
- `archive_failed` (if any archive sub-step fails; include reason)

for clean:
- `clean_started`
- `archive_started`
- `archive_finished` / `archive_failed`
- `clean_finished`

⸻

tests

manual test plan
	1.	happy merge

	•	create run, make a commit, push to create pr
	•	run agency merge <id> --squash
	•	confirm verify ran (verify.log updated), pr merged, worktree deleted, tmux session gone

	2.	verify fail prompt

	•	set verify script to exit 1
	•	run merge without --force and answer N
	•	ensure no merge happened; status shows needs attention

	3.	verify fail with force

	•	run merge with --force
	•	ensure verify ran, merge still required final merge confirmation, pr merged

	4.	pr already merged

	•	manually merge pr in github ui
	•	run agency merge <id>
	•	ensure it archives without error

	5.	remote out-of-date

	•	after push, make a new local commit but do not push
	•	agency merge <id> should fail E_REMOTE_OUT_OF_DATE

	6.	mergeability unknown (best-effort)

	•	hard to force; simulate by stubbing gh call in unit tests (see automated)

minimal automated tests (go)
	•	unit: mergeability handling
	•	mock gh output UNKNOWN 3 times -> E_PR_MERGEABILITY_UNKNOWN
	•	mock gh output CONFLICTING -> E_PR_NOT_MERGEABLE
	•	unit: remote head check
	•	given local sha != remote sha -> E_REMOTE_OUT_OF_DATE
	•	integration (optional, behind env flag):
	•	in temp git repo with fake scripts:
	•	verify success -> archive called -> meta updated
	•	gh interactions mocked via dependency injection (do not require real github)

⸻

guardrails
	•	never call agency push implicitly from merge
	•	never run any scripts in the tmux runner pane
	•	never merge without explicit confirmation (must type merge)
	•	never bypass missing-pr / non-mergeable / unknown-mergeability errors
	•	do not mutate parent working tree
	•	archive must be best-effort: attempt all cleanup steps and report what failed

⸻

rollout notes
	•	s6 is destructive; ship with conservative logs and clear “what was deleted” messages.
	•	ensure all write operations are atomic (temp + rename) for meta/records/events.
	•	keep user messaging plain and deterministic (no color, no spinners) in v1.
