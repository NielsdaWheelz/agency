# PR-06c Report: gh merge + full merge flow + idempotency

## summary

completed the `agency merge` command end-to-end:

- implemented `gh pr merge` invocation with explicit strategy flags (`--squash`, `--merge`, `--rebase`)
- added typed confirmation prompt (user must type `merge` exactly)
- added post-merge state confirmation with retries (250ms, 750ms, 1500ms backoff)
- integrated archive pipeline from PR-06a after successful merge
- implemented already-merged idempotent path (skips verify/mergeability checks but still requires confirmation)
- added merge.log capture for all gh merge operations
- added all required events to events.jsonl
- updated README.md with v1 MVP complete status and merge command documentation

## files changed

### modified

- `internal/commands/merge.go` — complete merge flow implementation
- `internal/commands/merge_test.go` — new tests for PR-06c functionality
- `internal/events/append.go` — new event data helpers for merge events
- `README.md` — updated status to v1 MVP complete, enhanced merge documentation

### unchanged

- `.gitignore` — already had `.agency/` entry

## problems encountered

1. **return signature change for validatePRState**: needed to return a result struct instead of just error to communicate "already merged" state for idempotent path handling. this required updating the test file as well.

2. **git.RepoRoot type check**: the `git.GetRepoRoot` function returns a struct value, not a pointer, so couldn't check for `!= nil`. fixed by checking the error return instead.

3. **sleeper interface reuse**: PR-06b already had a `Sleeper` interface and `realSleeper` type that needed to be reused for the post-merge confirmation retries.

## solutions implemented

1. **prStateResult struct**: created a new struct `prStateResult` with an `AlreadyMerged` bool field. `validatePRState` now returns `(*prStateResult, error)` allowing the caller to detect the idempotent path.

2. **handleAlreadyMergedPR function**: extracted the idempotent path into a separate function that:
   - skips verify, mergeability, and remote head checks
   - still requires typed confirmation (archive is destructive)
   - sets `archive.merged_at` if missing
   - runs archive pipeline

3. **confirmPRMerged function**: implemented post-merge state confirmation with retries:
   - queries `gh pr view --json state` up to 3 times
   - uses 250ms, 750ms, 1500ms backoff
   - returns true only when state is `MERGED`
   - returns false after all retries (no error, just unconfirmed)

4. **executeGHMerge function**: centralized gh pr merge execution:
   - always captures stdout/stderr to `logs/merge.log`
   - handles exit code and error wrapping

5. **runArchivePipeline function**: unified archive invocation for both normal and idempotent paths:
   - calls the PR-06a archive.Archive pipeline
   - emits appropriate events
   - handles partial success (merge ok but archive failed)

## decisions made

1. **confirmation required for already-merged path**: per spec, the typed confirmation is still required even for already-merged PRs because archive is destructive (deletes worktree).

2. **no verify for already-merged path**: when PR is already merged, we skip verify, mergeability checks, and remote head checks. the user is simply confirming they want to archive.

3. **merge.log always written**: regardless of success or failure, the gh merge output is captured to merge.log for debugging.

4. **post-merge confirmation is best-effort**: if gh pr merge succeeds (exit 0) but state confirmation fails, we return E_GH_PR_MERGE_FAILED with a hint to re-run. the user can then check manually or re-run.

5. **archive failure after merge success**: if merge succeeds but archive fails, we return E_ARCHIVE_FAILED with a message indicating "merge succeeded; archive failed". the `archive.merged_at` is set but `archive.archived_at` is not set unless deletion succeeded.

## deviations from spec

1. **none significant**: the implementation closely follows the PR-06c spec. minor implementation details:
   - `truncateString` helper added for error message formatting (not in spec but reasonable)
   - error message wording matches constitution patterns

2. **event data structure**: used existing `events.MergeFinishedData` helper pattern for consistency rather than inline map construction in some places.

## how to run

### build

```bash
go build -o agency ./cmd/agency
```

### test

```bash
# run all tests
go test ./...

# run merge-specific tests
go test ./internal/commands/ -run "TestValidatePRState|TestMergeStrategyFlags|TestConfirmPRMerged|TestTruncateString|TestCheckMergeability" -v
```

### manual testing

```bash
# prerequisites: have a run with a PR already created via agency push

# normal merge flow (squash is default)
agency merge <run_id>

# with specific strategy
agency merge <run_id> --merge
agency merge <run_id> --rebase

# force through verify failures
agency merge <run_id> --force

# if PR is already merged (idempotent path)
# manually merge PR in GitHub UI first, then:
agency merge <run_id>
# should print "note: PR #X is already merged; proceeding to archive"
# then prompt for confirmation
```

### verifying functionality

1. **normal merge flow**:
   - create run, make commits, `agency push <id>`
   - `agency merge <id> --squash`
   - should see verify run, confirmation prompt, PR merge, archive
   - check: PR merged in GitHub, worktree deleted, tmux session killed, meta.json has `archive.merged_at` and `archive.archived_at`

2. **already merged path**:
   - manually merge PR in GitHub UI
   - `agency merge <id>`
   - should skip verify, prompt for confirmation, archive
   - check: worktree deleted, `archive.merged_at` set

3. **verify failure with force**:
   - set verify script to `exit 1`
   - `agency merge <id> --force`
   - should skip verify-fail prompt, still require merge confirmation

4. **events verification**:
   - after merge, check `${AGENCY_DATA_DIR}/repos/<repo_id>/runs/<run_id>/events.jsonl`
   - should contain: `merge_started`, `merge_prechecks_passed`, `verify_started`, `verify_finished`, `merge_confirm_prompted`, `merge_confirmed`, `gh_merge_started`, `gh_merge_finished`, `archive_started`, `archive_finished`, `merge_finished`

5. **merge.log verification**:
   - check `${AGENCY_DATA_DIR}/repos/<repo_id>/runs/<run_id>/logs/merge.log`
   - should contain gh merge command output

## branch name and commit message

**branch**: `pr6/s6-pr3-gh-merge-full-flow`

**commit message**:

```
feat(merge): complete agency merge with gh pr merge + archive + idempotency

Implement PR-06c: the final piece of the agency merge command.

Changes:
- Add gh pr merge execution with strategy flags (--squash/--merge/--rebase)
- Add typed confirmation prompt (must type 'merge' exactly)
- Add post-merge state confirmation with retries (250ms, 750ms, 1500ms backoff)
- Add already-merged idempotent path (skip verify, still require confirmation)
- Integrate archive pipeline from PR-06a after successful merge
- Add merge.log capture to logs directory
- Add all required events: merge_confirm_prompted, merge_confirmed,
  gh_merge_started, gh_merge_finished, merge_already_merged, merge_finished
- Add new event data helpers in events/append.go
- Add tests for strategy flags, post-merge confirmation, truncateString helper
- Update validatePRState to return prStateResult for idempotent path detection
- Update README.md to mark v1 MVP complete and enhance merge documentation

The merge command now follows the full flow specified in s6_spec.md:
1. Run prechecks (worktree, origin, gh auth, PR state, mergeability, remote head)
2. Run verify script with --force bypass for verify-fail prompt
3. Prompt for typed confirmation
4. Execute gh pr merge with explicit strategy flag
5. Confirm PR reached MERGED state with retries
6. Run archive pipeline (script, tmux kill, worktree delete)

Idempotent behavior: if PR is already merged, skips verify/mergeability checks
but still requires confirmation before archive (archive is destructive).

This completes slice 6 and marks v1 MVP as feature-complete.

Refs: s6_spec.md, s6_prs.md, s6_pr3.md
```
