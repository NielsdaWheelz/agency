# agency l4: pr-06c spec — gh merge + full merge flow + idempotency (v1 mvp)

## goal

complete `agency merge` end-to-end:

- run deterministic prechecks + verification evidence (from pr-06b)
- require explicit typed confirmation (`merge`)
- execute `gh pr merge` with explicit strategy flag
- confirm PR reaches `MERGED` state
- then archive via the pr-06a archive pipeline
- handle “already merged” idempotently (skip verify + remote-head checks, but still require typed confirmation before archive)

---

## scope

### in-scope

- `agency merge <run_id> [--squash|--merge|--rebase] [--force]` completes:
  - prechecks + PR resolution + mergeability checks (from pr-06b)
  - verify runner execution and verify evidence recording (from pr-06b)
  - typed confirmation prompt (`merge`)
  - `gh pr merge` invocation
  - post-merge state confirmation (`MERGED` required)
  - archive pipeline call (from pr-06a)
  - metadata + events updates

- idempotency for already merged PR:
  - if PR state is `MERGED` at precheck time:
    - **skip**: verify + remote head checks + mergeability checks
    - **still require** typed confirmation `merge` (because archive is destructive)
    - set `archive.merged_at` if missing (use `clock.NowUTC()`)
    - run archive pipeline
    - return 0 on successful archive; otherwise `E_ARCHIVE_FAILED` (merge already done)

### out-of-scope

- implicit `agency push` (merge must not push/create PRs)
- deleting remote branches (`--delete-branch` not used)
- deleting local branches
- PR checks parsing/enforcement
- auto-rebase/conflict resolution
- interactive TUI
- enterprise github hosts / non-github.com origins

---

## dependencies / assumptions

This PR assumes pr-06a and pr-06b have landed.

### assumed outputs from pr-06a (must exist)

- archive pipeline:
  - file: `internal/archive/pipeline.go`
  - function: `archive.Archive(...)` returning a `Result` struct
- worktree removal helper:
  - file: `internal/worktree/remove.go`
  - function: `worktree.RemoveWorktree(repoRoot, worktreePath) error`
- safe rm-rf guard:
  - file: `internal/fs/safe_remove.go`
  - function: `fs.RemoveAllIfUnderPrefix(target, prefix string) error`
- tmux helper:
  - file: `internal/tmux/client_exec.go`
  - function: `tmux.IsNoSessionErr(err) bool`
- error codes added:
  - `E_ARCHIVE_FAILED`, `E_WORKTREE_MISSING`, `E_ABORTED`, `E_NOT_INTERACTIVE`

### assumed outputs from pr-06b (must exist)

- merge prechecks + PR resolution + verify runner are implemented somewhere (file paths not fixed by 06b).
- required interfaces exist (names fixed by 06b):
  - `Exec`, `Clock`, `Store`, `GH adapter`
- required events exist (names fixed by 06b):
  - `merge_started`, `merge_prechecks_passed`
  - `verify_started`, `verify_finished`
  - `verify_continue_prompted`, `verify_continue_accepted`, `verify_continue_rejected`
- error codes introduced by 06b exist:
  - `E_GIT_FETCH_FAILED`, `E_GH_PR_VIEW_FAILED`, `E_PR_MISMATCH`, `E_GH_REPO_PARSE_FAILED`, `E_PR_MERGEABILITY_UNKNOWN`
- merge command exists but stops before calling gh merge and before archive (ends with `E_NOT_IMPLEMENTED`).

> if 06b did not fix file paths, pr-06c **must not** refactor or relocate 06b’s code; it should add the minimum glue needed in place.
> pr-06c may introduce a small merge glue file/package (e.g., `internal/commands/merge.go` or `internal/merge/glue.go`) that wraps whatever 06b produced without moving existing code.
> if 06b did not add a Store interface, pr-06c must use existing primitives: `store.ReadMeta/UpdateMeta` + `events.AppendEvent(store.EventsPath(...), ...)`.

---

## public surface area

### command behavior change

- `agency merge ...` is now fully implemented (no longer exits `E_NOT_IMPLEMENTED` after prechecks)

### flags (no change)

- `--squash | --merge | --rebase` (at most one; default `--squash`)
- `--force` bypasses only the “verify failed, continue?” prompt
  - `--force` does **not** bypass:
    - missing PR (`E_NO_PR`)
    - PR not open / closed (`E_PR_NOT_OPEN`)
    - draft PR (`E_PR_DRAFT`)
    - unknown mergeability (`E_PR_MERGEABILITY_UNKNOWN`)
    - non-mergeable PR (`E_PR_NOT_MERGEABLE`)
    - unsupported origin host (`E_UNSUPPORTED_ORIGIN_HOST`)
    - remote out of date (`E_REMOTE_OUT_OF_DATE`)
    - gh not authenticated (`E_GH_NOT_AUTHENTICATED`)
    - PR mismatch (`E_PR_MISMATCH`)

---

## files created/modified

### modified

- merge command implementation file (existing from 06b; path depends on current codebase)
  - if not already present, use: `internal/commands/merge.go` and wire into dispatcher
- any gh adapter file from 06b (to add merge + state-check methods)
- tests for merge flow
- `internal/cli/dispatch.go` (add `merge` subcommand wiring with stdlib `flag`)

### storage paths

Uses the existing/defined paths:

- `${AGENCY_DATA_DIR}/repos/<repo_id>/runs/<run_id>/meta.json` (update)
- `${AGENCY_DATA_DIR}/repos/<repo_id>/runs/<run_id>/events.jsonl` (append)
- `${AGENCY_DATA_DIR}/repos/<repo_id>/runs/<run_id>/logs/verify.log` (overwrite; already handled by 06b verify)
- `${AGENCY_DATA_DIR}/repos/<repo_id>/runs/<run_id>/verify_record.json` (overwrite; already handled by 06b verify)
- `${AGENCY_DATA_DIR}/repos/<repo_id>/runs/<run_id>/logs/archive.log` (overwrite; already handled by 06a archive)
- `${AGENCY_DATA_DIR}/repos/<repo_id>/runs/<run_id>/logs/merge.log` (overwrite; new in 06c, captures gh merge stdout/stderr)

---

## new error codes

- `E_GH_PR_MERGE_FAILED` — gh merge failed or merge state could not be confirmed as `MERGED`
- merge prompt errors (from 06a): `E_ABORTED`, `E_NOT_INTERACTIVE`

> do not add new error codes beyond this without updating the constitution. reuse existing codes for other failures.

---

## gh merge contract

### command invocation

All gh operations must be pinned to the repo:

- use `-R <owner>/<repo>` on **every** gh call in merge flow
- `<owner>/<repo>` is derived from origin URL parsing already required in 06b
  - parse failures remain `E_GH_REPO_PARSE_FAILED`

### merge call

- run:
  - `gh pr merge <pr_number> -R <owner>/<repo> <strategy_flag>`
- where `<strategy_flag>` is exactly one of:
  - `--squash` (default if none specified)
  - `--merge`
  - `--rebase`

### forbidden gh flags (v1)

- must not pass:
  - `--auto`
  - `--delete-branch`
  - `--admin`
  - `--body` / `--subject` (no body editing here)

### success criteria

- `gh pr merge` exit code == 0 is **necessary but not sufficient**
- after exit 0, agency must confirm PR state becomes `MERGED`:

Post-merge confirmation:
- call `gh pr view <pr_number> -R <owner>/<repo> --json state`
- retry up to 3 times with backoff (250ms, 750ms, 1500ms)
- require `state == "MERGED"`
- if unable to confirm `MERGED` after retries:
  - return `E_GH_PR_MERGE_FAILED`
  - **do not archive**
  - include hint: “re-run `agency merge <id>`; it may have merged but confirmation failed”
- if gh view fails during confirmation retries:
  - return `E_GH_PR_MERGE_FAILED`
  - include stderr in error message (truncate)

If `gh pr merge` exits non-zero:
- return `E_GH_PR_MERGE_FAILED`
- include gh stderr in the printed error (truncate to a reasonable limit)
- write gh stdout/stderr to `logs/merge.log` (overwrite)

merge log:
- always capture gh merge stdout/stderr to `logs/merge.log` (overwrite), regardless of success or failure

---

## behaviors (given/when/then)

### lock + logging requirement

- acquire repo lock at command start
- print to stdout immediately on lock acquisition:
  - `lock: acquired repo lock (held during verify/merge/archive)`
- hold lock through verify + merge + archive attempts
- release on termination
- if stdin/stderr are not TTYs, return `E_NOT_INTERACTIVE` after lock acquisition and run existence checks; do not run prechecks or verify

### merge: normal happy path

given:
- run exists + worktree present
- origin exists + host `github.com`
- gh authenticated
- PR exists, `state == OPEN`, `isDraft == false`
- mergeable `MERGEABLE` (not UNKNOWN, not CONFLICTING)
- remote head is up to date with local head
- verify succeeds (exit 0)

when:
- user runs: `agency merge <id> --squash`

then:
- run prechecks (06b) → append `merge_started`, `merge_prechecks_passed`
- run verify (06b) → append `verify_started`, `verify_finished`
- prompt: `confirm: type 'merge' to proceed: `
  - accept only `strings.TrimSpace(input) == "merge"`
  - anything else: abort with `E_ABORTED`
- append events:
  - `merge_confirm_prompted`
  - `merge_confirmed`
- run gh merge:
  - append `gh_merge_started`
  - invoke `gh pr merge ...`
  - append `gh_merge_finished` with `{ok, pr_number, pr_url}`
- confirm PR state becomes `MERGED` (retry policy above)
- set `archive.merged_at = clock.NowUTC()` in meta.json
- run archive pipeline (06a):
  - append `archive_started`
  - append `archive_finished` or `archive_failed` with details
- if archive fully succeeds:
  - append `merge_finished` with `{ok:true}`
  - set `archive.archived_at = clock.NowUTC()` in meta.json
  - exit 0
- if archive has any failure:
  - return `E_ARCHIVE_FAILED` (even though merge succeeded)
  - ensure error message explicitly says: “merge succeeded; archive failed”
  - `archive.archived_at` is set **iff** worktree deletion succeeded (06a rule)
  - append `merge_finished` with `{ok:false, error_code:"E_ARCHIVE_FAILED"}`

### merge: verify fails, user rejects

given:
- verify exits non-zero or times out

when:
- user runs `agency merge <id>` without `--force`
- prompt appears: `verify failed. continue anyway? [y/N]`
- user enters empty or `n`

then:
- append events:
  - `verify_continue_prompted`
  - `verify_continue_rejected`
- set `flags.needs_attention=true`
- exit non-zero with verify’s mapped error (`E_SCRIPT_FAILED` or `E_SCRIPT_TIMEOUT`)
- append `merge_finished` with `{ok:false, error_code:"E_SCRIPT_FAILED|E_SCRIPT_TIMEOUT"}`
- do not prompt for merge confirmation
- do not call gh merge
- do not archive

### merge: verify fails, user forces

given:
- verify fails

when:
- user runs `agency merge <id> --force`

then:
- do not prompt “verify failed, continue?”
- still require typed confirmation `merge`
- proceed with gh merge + archive

### merge: PR already merged (idempotent)

given:
- PR exists, and `state == MERGED`

when:
- user runs `agency merge <id>`

then:
- treat as idempotent archive path:
  - append `merge_started`
  - append `merge_already_merged` with `{pr_number, pr_url}`
  - **skip** verify, mergeability, and remote head checks
  - prompt: `confirm: type 'merge' to proceed: `
    - accept only `strings.TrimSpace(input) == "merge"`
    - else `E_ABORTED`
  - set `archive.merged_at` if missing (use `clock.NowUTC()`)
  - run archive pipeline (06a)
- if archive succeeds: append `merge_finished` with `{ok:true}`, exit 0
- if archive fails: append `merge_finished` with `{ok:false, error_code:"E_ARCHIVE_FAILED"}`, return `E_ARCHIVE_FAILED`

### merge: PR closed (unmerged)

given:
- PR exists, `state == CLOSED`

when:
- user runs merge

then:
- return `E_PR_NOT_OPEN`
- do not archive

### merge: gh merge command fails

given:
- `gh pr merge` exit code != 0

then:
- append `gh_merge_started`
- append `gh_merge_finished` with `{ok:false, pr_number, pr_url}`
- append `merge_finished` with `{ok:false, error_code:"E_GH_PR_MERGE_FAILED"}`
- return `E_GH_PR_MERGE_FAILED`
- do not archive
- meta must **not** set `archive.merged_at`

### merge: gh merge ok but state not confirmed

given:
- `gh pr merge` exit code == 0
- but `gh pr view ... --json state` does not return `MERGED` after retries
  - or gh view fails during confirmation retries

then:
- return `E_GH_PR_MERGE_FAILED`
- do not archive
- meta must **not** set `archive.merged_at` (because merge is unconfirmed)
- append `merge_finished` with `{ok:false, error_code:"E_GH_PR_MERGE_FAILED"}`

---

## persistence

### meta.json updates (minimum)

- on merge success (confirmed `MERGED`):
  - set `archive.merged_at` (UTC RFC3339)
- on archive success:
  - set `archive.archived_at` (UTC RFC3339)
- on already-merged idempotent path:
  - set `archive.merged_at` if missing (use now)
- verify-related fields remain owned by 06b’s verify pipeline:
  - do not change verify record schema in 06c

### events.jsonl

#### additional required events introduced by this PR

- `merge_confirm_prompted`
- `merge_confirmed`
- `gh_merge_started`
- `gh_merge_finished` (include `{ok, pr_number, pr_url}`)
- `merge_already_merged` (include `{pr_number, pr_url}`)
- `merge_finished` (include `{ok, error_code?}`)

> archive events are emitted by 06a pipeline: `archive_started`, `archive_finished`, `archive_failed`.

---

## tests

### unit tests (required)

All tests use the existing fake exec / gh adapter mechanisms from 06b. No real GitHub.
If 06b did not introduce these seams, 06c must add a minimal Exec/GH shim for merge only (do not migrate unrelated commands).

1) **strategy flag mapping**
- default → `--squash`
- `--merge` → `--merge`
- `--rebase` → `--rebase`
- >1 strategy flag → `E_USAGE`

2) **typed confirmation gates**
- wrong input (anything other than `merge`) → `E_ABORTED`
- ensure `gh pr merge` is not invoked when confirmation fails

3) **merge success path**
- simulate:
  - prechecks ok, verify ok
  - gh merge returns exit 0
  - gh view state returns `MERGED`
  - archive pipeline returns ok
- assert:
  - `archive.merged_at` and `archive.archived_at` set
  - `gh_merge_started`/`gh_merge_finished` events appended
  - archive invoked after merge confirmation

4) **archive failure after merge**
- simulate merge confirmed and gh merged, but archive returns failure
- assert:
  - returns `E_ARCHIVE_FAILED`
  - `archive.merged_at` set
  - `archive.archived_at` set iff deletion succeeded per 06a result model (use whatever 06a exposes)

5) **merge ok but state not confirmed**
- simulate gh merge exit 0, but gh pr view state is never `MERGED` (or view fails)
- assert:
  - returns `E_GH_PR_MERGE_FAILED`
  - archive not invoked
  - `archive.merged_at` not set

6) **already merged idempotent path**
- simulate PR `state=MERGED`
- assert:
  - verify not invoked
  - remote head check not invoked
  - confirmation still required
  - archive invoked

### manual tests (required)

1) end-to-end merge
- create run, commit, `agency push <id>` create PR
- `agency merge <id> --squash`
- observe:
  - verify evidence written
  - PR merged
  - tmux session killed
  - worktree removed
  - meta contains merged_at + archived_at
  - logs retained

2) already merged
- merge PR in GitHub UI
- rerun `agency merge <id>`
- should prompt and then archive (skip verify)

---

## guardrails

- never invoke `agency push` from merge
- never delete any branches (remote or local)
- never archive unless:
  - user typed `merge` exactly
  - AND either:
    - merge was confirmed `MERGED`, or
    - PR was already `MERGED`
- if merge cannot be confirmed as `MERGED`, do not archive
- no refactors or relocations of 06b code; add minimal glue only
- keep output plain (no color/spinners)

---

## implementation notes (non-normative)

- favor adding minimal methods to the existing GH adapter interface:
  - `PRMerge(ctx, repo, prNumber, strategyFlag) error`
  - `PRState(ctx, repo, prNumber) (state string, error)`
- if 06b did not introduce a GH adapter, add a minimal wrapper in a new file without moving existing code
- keep confirmation handling consistent with existing patterns:
  - use `tty.IsInteractive()` and `bufio.Scanner` + `strings.TrimSpace`
  - if non-interactive: `E_NOT_INTERACTIVE`
- ensure `E_GH_PR_MERGE_FAILED` prints:
  - `error_code: E_GH_PR_MERGE_FAILED`
  - `hint:` line with the recommended rerun / check guidance
- if 06b did not introduce an Exec seam, 06c may add a minimal Exec wrapper for merge only (do not migrate unrelated commands)
