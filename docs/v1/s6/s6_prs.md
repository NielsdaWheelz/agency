# agency l3: s06 pr roadmap — merge + archive (v1 mvp)

goal: implement `agency merge` + `agency clean` with deterministic prechecks, verification evidence, explicit human confirmation, `gh pr merge`, and best-effort archive cleanup (tmux + worktree) while retaining metadata/logs.

constraints:
- 3 prs total
- merge requires an existing pr (no implicit `push`)
- `--force` bypasses only “verify failed, continue?”
- never delete remote branches in v1
- never delete local branches in v1
- destructive commands require explicit typed confirmation
- print on lock acquisition per command:
  - merge: `lock: acquired repo lock (held during verify/merge/archive)`
  - clean: `lock: acquired repo lock (held during clean/archive)`

---

## pr-06a — archive pipeline + clean

goal
- ship safe, best-effort archive behavior + `agency clean <run_id>`.

scope
- add a reusable archive pipeline:
  - run `scripts.archive` (timeout 5m), write `logs/archive.log`, record success/failure
  - kill tmux session `agency_<run_id>` (missing session is not failure)
  - delete worktree:
    - first try `git -C <repo_root> worktree remove --force <worktree_path>`
    - if that fails, fallback to `rm -rf <worktree_path>` **only if** `<worktree_path>` is under `${AGENCY_DATA_DIR}/repos/<repo_id>/worktrees/`
    - allowed-prefix check uses `filepath.Clean` + `filepath.EvalSymlinks` to prevent `../` trickery
    - if `<repo_root>` is missing, skip `git worktree remove` and go straight to safe `rm -rf` (allowed-prefix only), logging the reason
  - archive failure is best-effort: attempt all steps regardless of earlier failures
  - if any sub-step fails, return `E_ARCHIVE_FAILED` and do not set `archive.archived_at`
- implement `agency clean <run_id>`:
  - acquire repo lock
  - prechecks: run exists, worktree present
  - prompt: `confirm: type 'clean' to proceed: `
  - on confirmation, run archive pipeline
  - on full success: set `flags.abandoned=true` and `archive.archived_at`
  - append events (`clean_started`, `archive_started`, `archive_finished|archive_failed`, `clean_finished`)

public surface area
- new command:
  - `agency clean <run_id>`
- new errors (if not already present):
  - `E_ARCHIVE_FAILED`
  - `E_WORKTREE_MISSING` (or `E_WORKSPACE_ARCHIVED` if you prefer)

files changed
- `${AGENCY_DATA_DIR}/repos/<repo_id>/runs/<run_id>/meta.json`
- `${AGENCY_DATA_DIR}/repos/<repo_id>/runs/<run_id>/events.jsonl`
- `${AGENCY_DATA_DIR}/repos/<repo_id>/runs/<run_id>/logs/archive.log`

tests
- unit:
  - archive safety: rm-rf fallback rejected if path not under allowed prefix
  - tmux kill: missing session treated as ok
- integration (no real github):
  - create temp repo + worktree; write fake meta with worktree_path under data dir; run clean; assert worktree deleted + meta updated
- manual:
  - create run in earlier slices, then `agency clean <id>` and ensure tmux/worktree removed

guardrails
- do not touch gh / pr logic
- do not delete any git branches (local or remote)
- never rm-rf outside allowed prefix

---

## pr-06b — verify runner + merge prechecks (no gh merge yet)

goal
- implement merge prechecks + deterministic verify evidence + prompts, but stop before calling `gh pr merge`.

scope
- add reusable verify runner:
  - run `scripts.verify` (timeout 30m) with injected env
  - stdin=/dev/null
  - overwrite `logs/verify.log`
  - write/overwrite `verify_record.json` (schema from s6 slice spec)
  - append events `verify_started` / `verify_finished`
  - set `flags.needs_attention=true` when verify fails
- implement merge prechecks:
  - precheck order:
    1) run exists + worktree present
    2) origin exists (prefer repo.json; else git remote)
    3) origin host == github.com (`E_UNSUPPORTED_ORIGIN_HOST`)
    4) gh auth status ok (`E_GH_NOT_AUTHENTICATED`)
    5) PR resolution via gh
  - require existing PR resolution:
    1) if `meta.pr_number` present: `gh pr view <num> --json number,url,state,isDraft,mergeable,headRefName`
    2) else: `gh pr view --head <owner>:<branch> --json number,url,state,isDraft,mergeable,headRefName`
    3) else fail `E_NO_PR` with hint `agency push <id>`
  - run all gh commands with `-R <owner>/<repo>` (owner/repo parsed from origin url; `E_GH_REPO_PARSE_FAILED` on parse failure)
  - if gh fails, returns non-json, or any required field is missing/unexpected: `E_GH_PR_VIEW_FAILED` with stderr captured
  - PR must be:
    - `state == OPEN`
    - `isDraft == false` else `E_PR_DRAFT`
  - PR mismatch validation:
    - require `headRefName == <branch>`
    - if mismatch: `E_PR_MISMATCH` with hint to rerun `agency push` or repair meta
  - mergeability:
    - read `mergeable` from gh json
    - retry 3x if `UNKNOWN` with backoff (1s,2s,2s)
    - if still `UNKNOWN`: `E_PR_MERGEABILITY_UNKNOWN`
    - if `CONFLICTING`: `E_PR_NOT_MERGEABLE`
  - remote head up-to-date:
    - `git -C <worktree> fetch origin refs/heads/<branch>:refs/remotes/origin/<branch>`; on failure `E_GIT_FETCH_FAILED`
    - compare `git -C <worktree> rev-parse HEAD` vs `git -C <worktree> rev-parse origin/<branch>`
    - mismatch -> `E_REMOTE_OUT_OF_DATE` with hint `agency push <id>`
- `agency merge <run_id> [--squash|--merge|--rebase] [--force]`:
  - parse strategy flags but do not call gh merge yet
  - acquire repo lock
  - run all prechecks
  - always run verify; if fail and no `--force`: prompt `verify failed. continue anyway? [y/N]`
    - if no: exit `E_SCRIPT_FAILED` and do not archive
  - for this PR: do not prompt for merge confirmation; after verify/prechecks, print `merge step not implemented; run updated; ready to merge once pr-06c lands.` and exit `E_NOT_IMPLEMENTED`
  - write events through `verify_finished` only
  - verify failure exit mapping:
    - non-zero exit -> `E_SCRIPT_FAILED` and set `flags.needs_attention=true`
    - timeout -> `E_SCRIPT_TIMEOUT` and set `flags.needs_attention=true`

public surface area
- command:
  - `agency merge ...` implemented up to the merge call
- new errors (if not already present):
  - `E_GIT_FETCH_FAILED`
  - `E_GH_PR_VIEW_FAILED`
  - `E_PR_MISMATCH`
  - `E_GH_REPO_PARSE_FAILED`

files changed
- `${AGENCY_DATA_DIR}/repos/<repo_id>/runs/<run_id>/meta.json`
- `${AGENCY_DATA_DIR}/repos/<repo_id>/runs/<run_id>/events.jsonl`
- `${AGENCY_DATA_DIR}/repos/<repo_id>/runs/<run_id>/logs/verify.log`
- `${AGENCY_DATA_DIR}/repos/<repo_id>/runs/<run_id>/verify_record.json`

tests
- unit (with FakeExec fixtures):
  - mergeability UNKNOWN 3x -> `E_PR_MERGEABILITY_UNKNOWN`
  - mergeability CONFLICTING -> `E_PR_NOT_MERGEABLE`
  - remote sha mismatch -> `E_REMOTE_OUT_OF_DATE`
  - pr draft -> `E_PR_DRAFT`
  - pr missing -> `E_NO_PR`
  - gh pr view missing fields -> `E_GH_PR_VIEW_FAILED`
  - pr head mismatch -> `E_PR_MISMATCH`
  - verify fail prompt path (simulate stdin responses)
  - verify timeout -> `E_SCRIPT_TIMEOUT` and `flags.needs_attention=true`
- integration (optional behind env):
  - no real gh; just ensure verify runner writes records/logs

guardrails
- do not call `gh pr merge` yet
- do not archive from merge in this PR

---

## pr-06c — gh merge + full merge flow + idempotency

goal
- complete `agency merge`: call `gh pr merge`, then archive (using pr-06a pipeline), handle “already merged” idempotently.

scope
- implement actual merge:
  - run all gh commands with `-R <owner>/<repo>` (owner/repo parsed from origin url)
  - merge strategy rules:
    - at most one of `--squash|--merge|--rebase`
    - default `--squash` if none
    - >1 -> `E_USAGE`
  - capture gh merge stdout/stderr to `${AGENCY_DATA_DIR}/repos/<repo_id>/runs/<run_id>/logs/merge.log` (overwrite)
  - add typed merge confirmation + events:
    - prompt: `confirm: type 'merge' to proceed: `
    - append `merge_confirm_prompted` and `merge_confirmed`
  - after prechecks + verify + confirmation:
    - run `gh pr merge <pr_number> <strategy_flag>`
    - record events `gh_merge_started` / `gh_merge_finished`
  - post-merge confirmation:
    - if `gh pr merge` exits non-zero: `E_GH_PR_MERGE_FAILED` with stderr
    - if exit 0, re-query `gh pr view <num> --json state` with a short retry (2-3 tries) and require `MERGED`
    - if still not merged: `E_GH_PR_MERGE_FAILED` with stderr
  - set `archive.merged_at` (utc) after merge success
  - call archive pipeline from pr-06a:
    - on success: set `archive.archived_at`, exit 0
    - on archive failure: return `E_ARCHIVE_FAILED` but note merge succeeded
- idempotency:
  - if PR state is already `MERGED` at precheck time:
    - skip gh merge
    - skip verify + mergeability + remote head check
    - set `archive.merged_at` if missing
    - archive as normal
- PR state `CLOSED` (unmerged):
  - `E_PR_NOT_OPEN`, no archive

public surface area
- `agency merge` fully implemented end-to-end

new errors (if needed)
- `E_GH_PR_MERGE_FAILED` (or reuse `E_GH_PR_EDIT_FAILED` etc. but better new one)
- `E_PR_MISMATCH`
- `E_GH_REPO_PARSE_FAILED`

tests
- unit:
  - args produced for each strategy flag
  - already merged path skips gh merge but archives
  - archive failure after merge returns `E_ARCHIVE_FAILED` and still sets `merged_at`
- integration (mock gh via FakeExec):
  - simulate gh merge ok -> state becomes MERGED -> archive called

guardrails
- never invoke `agency push`
- never delete branches
- always require explicit confirmation prompts

---

## cross-pr acceptance criteria (after pr-06c)

manual end-to-end:
1) create run + commit + `agency push <id>` to create PR
2) `agency merge <id> --squash`
3) verify evidence exists, pr merged, tmux killed, worktree deleted, meta marked merged+archived, logs retained
