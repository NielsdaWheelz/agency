# s6 pr-02 report: verify runner + merge prechecks (no gh merge yet)

## summary of changes

implemented deterministic **verify evidence** + **merge prechecks** for `agency merge`, stopping before actual `gh pr merge` call:

1. **new `agency merge <run_id>` command** — runs prechecks + verify, exits `E_NOT_IMPLEMENTED`:
   - parses and validates strategy flags (`--squash|--merge|--rebase`)
   - parses `--force` flag to bypass verify-fail prompt
   - defaults strategy to `squash` if none specified
   - at most one strategy flag allowed (`E_USAGE` otherwise)

2. **precheck pipeline** (deterministic order per spec):
   1. run exists + worktree present (`E_RUN_NOT_FOUND`, `E_WORKTREE_MISSING`)
   2. origin exists — prefers `repo.json`, fallback to `git remote get-url` (`E_NO_ORIGIN`)
   3. origin host == `github.com` (`E_UNSUPPORTED_ORIGIN_HOST`)
   4. gh authenticated (`E_GH_NOT_AUTHENTICATED`)
   5. resolve owner/repo from origin URL (`E_GH_REPO_PARSE_FAILED`)
   6. PR resolution — by stored `pr_number` or by `--head` branch lookup (`E_NO_PR`, `E_GH_PR_VIEW_FAILED`)
   7. PR state + mismatch checks (`E_PR_NOT_OPEN`, `E_PR_DRAFT`, `E_PR_MISMATCH`)
   8. mergeability with retry for UNKNOWN (`E_PR_NOT_MERGEABLE`, `E_PR_MERGEABILITY_UNKNOWN`)
   9. remote head up-to-date (`E_GIT_FETCH_FAILED`, `E_REMOTE_OUT_OF_DATE`)

3. **verify runner integration** — runs `scripts.verify` after prechecks pass:
   - writes `logs/verify.log` and `verify_record.json`
   - sets `flags.needs_attention=true` on verify failure
   - sets `last_verify_at` in `meta.json`

4. **verify-fail prompting** — when verify fails:
   - without `--force`: prompts `verify failed. continue anyway? [y/N]`
   - user must type `y` or `Y` to continue; anything else aborts
   - with `--force`: skips prompt, continues to termination

5. **events** — append-only to `events.jsonl`:
   - `merge_started` (with strategy + force)
   - `merge_prechecks_passed` (with pr_number, pr_url, branch)
   - `verify_started` (with timeout_ms)
   - `verify_finished` (with ok, exit_code, duration_ms)
   - `verify_continue_prompted` (if verify failed and no --force)
   - `verify_continue_accepted` / `verify_continue_rejected`

6. **termination** — after prechecks + verify:
   - prints to stderr: `note: merge step not implemented in pr-06b; re-run after pr-06c lands`
   - exits with `E_NOT_IMPLEMENTED`

## problems encountered

1. **existing fakeCommandRunner conflict** — test file had a `fakeCommandRunner` type that conflicted with existing test files in the package. renamed to `mergeTestCommandRunner`.

2. **store.RepoRecord type access** — needed to read `repo.json` for origin URL. used `json.Unmarshal` directly since the store doesn't expose a `LoadRepoRecord` method returning the full struct with all fields.

3. **gh pr view field validation** — needed to validate all required fields from gh JSON response and handle unexpected field values (e.g., invalid `state` or `mergeable` enums).

## solutions implemented

1. **pr resolution order** — follows spec exactly:
   - if `meta.pr_number` exists, try `gh pr view <num> -R <owner>/<repo>`
   - else try `gh pr view --head <owner>:<branch> -R <owner>/<repo>`
   - if both fail with "not found" pattern: `E_NO_PR` with hint
   - if gh fails with schema/parse error: `E_GH_PR_VIEW_FAILED`

2. **mergeability retry** — per spec:
   - retry 3 times with backoff (0, 1s, 2s, 2s) if `UNKNOWN`
   - after retries exhausted: `E_PR_MERGEABILITY_UNKNOWN`

3. **remote head check** — per spec:
   - fetch specific branch ref: `git fetch origin refs/heads/<branch>:refs/remotes/origin/<branch>`
   - compare `git rev-parse HEAD` vs `git rev-parse refs/remotes/origin/<branch>`
   - if remote ref missing: `E_REMOTE_OUT_OF_DATE` with hint "remote branch missing"
   - if sha differs: `E_REMOTE_OUT_OF_DATE` with hint "local head differs"

4. **event data payloads** — used inline `map[string]any` as per spec recommended data payloads.

## decisions made

1. **parseOriginHost helper** — created separate helper function that handles:
   - scp-like URLs: `git@github.com:owner/repo.git`
   - https URLs: `https://github.com/owner/repo.git`
   - returns empty string for unknown formats

2. **ghPRViewFull struct** — separate from existing `ghPRView` to include all required merge fields:
   - `number`, `url`, `state`, `isDraft`, `mergeable`, `headRefName`

3. **schema error detection** — errors from `parseGHPRViewFull` include "(schema_error)" marker to distinguish from "not found" errors.

4. **lock message** — prints `lock: acquired repo lock (held during verify/merge/archive)` per spec (wording fixed even though this PR doesn't merge/archive).

5. **verify environment** — reused same env building pattern from verifyservice, adding all L0 contract env vars.

## deviations from spec

1. **no Exec/Clock/Store interfaces** — spec suggested abstracting these, but the codebase already has:
   - `exec.CommandRunner` interface
   - `store.Store` with `Now func() time.Time` injectable clock
   - decided to use existing patterns rather than add new abstractions

2. **no GH adapter interface** — spec suggested `GH interface` with `AuthStatus`, `PRViewByNumber`, `PRViewByHead`. instead used inline helper functions (`viewPRByNumberFull`, `viewPRByHeadFull`) that call `cr.Run` directly. this keeps the code simpler and matches existing patterns in push.go.

3. **pr-06b termination behavior** — spec said exit `E_NOT_IMPLEMENTED`. implemented exactly as specified, with message to stderr.

## how to run new/changed commands

### `agency merge` (pr-06b state)

```bash
# basic merge (will run prechecks + verify, then exit E_NOT_IMPLEMENTED)
agency merge <run_id>

# with explicit strategy
agency merge <run_id> --squash    # default
agency merge <run_id> --merge     # regular merge
agency merge <run_id> --rebase    # rebase merge

# with --force (skip verify-fail prompt)
agency merge <run_id> --force
```

**example output (happy path through verify):**
```
lock: acquired repo lock (held during verify/merge/archive)
note: merge step not implemented in pr-06b; re-run after pr-06c lands
error_code: E_NOT_IMPLEMENTED
merge step not implemented; re-run after pr-06c lands
```

**example output (verify fails, user rejects):**
```
lock: acquired repo lock (held during verify/merge/archive)
verify failed. continue anyway? [y/N] n
error_code: E_SCRIPT_FAILED
verify failed
```

### how to test prechecks

```bash
# test E_NO_PR (no PR exists)
agency merge <run_id_without_pr>
# expect: E_NO_PR with hint "run: agency push <id>"

# test E_PR_DRAFT (PR is draft)
# create a draft PR, then:
agency merge <run_id>
# expect: E_PR_DRAFT

# test E_REMOTE_OUT_OF_DATE
# make local commit after push, then:
agency merge <run_id>
# expect: E_REMOTE_OUT_OF_DATE with hint "run: agency push <id>"
```

### run tests

```bash
# all commands tests
go test ./internal/commands/... -v

# merge-specific tests
go test ./internal/commands/... -run "Test.*Merge\|TestParseOriginHost\|TestParseGHPRViewFull\|TestIsGHPRNotFound\|TestValidatePRState\|TestCheckMergeability\|TestCheckRemoteHeadUpToDate\|TestGetOriginURLForMerge" -v
```

### verify files written

```bash
# check verify_record.json
cat ${AGENCY_DATA_DIR}/repos/<repo_id>/runs/<run_id>/verify_record.json

# check verify.log
cat ${AGENCY_DATA_DIR}/repos/<repo_id>/runs/<run_id>/logs/verify.log

# check events.jsonl for merge events
cat ${AGENCY_DATA_DIR}/repos/<repo_id>/runs/<run_id>/events.jsonl | grep merge
```

## branch name and commit message

**branch:** `pr6/s6-pr2-merge-prechecks-verify`

**commit message:**
```
feat(s6): add merge prechecks + verify runner (no gh merge yet)

implement slice 6 PR-02: deterministic merge prechecks + verify evidence

merge command (internal/commands/merge.go):
- parse strategy flags (--squash/--merge/--rebase), default squash
- parse --force flag to bypass verify-fail prompt
- validate at most one strategy flag (E_USAGE)
- require interactive TTY for confirmation prompts

precheck pipeline (deterministic order):
1. run exists + worktree present
2. origin exists (prefer repo.json, fallback git remote)
3. origin host == github.com (E_UNSUPPORTED_ORIGIN_HOST)
4. gh authenticated (E_GH_NOT_AUTHENTICATED)
5. resolve owner/repo from origin URL (E_GH_REPO_PARSE_FAILED)
6. PR resolution by number or head branch (E_NO_PR, E_GH_PR_VIEW_FAILED)
7. PR state checks: OPEN, not draft, branch matches (E_PR_NOT_OPEN, E_PR_DRAFT, E_PR_MISMATCH)
8. mergeability with 3x retry for UNKNOWN (E_PR_NOT_MERGEABLE, E_PR_MERGEABILITY_UNKNOWN)
9. remote head up-to-date via fetch + sha compare (E_GIT_FETCH_FAILED, E_REMOTE_OUT_OF_DATE)

verify runner integration:
- always run verify after prechecks pass
- write logs/verify.log and verify_record.json
- set flags.needs_attention=true on verify failure
- set last_verify_at in meta.json

verify-fail prompting:
- without --force: prompt "verify failed. continue anyway? [y/N]"
- accept y/Y to continue, anything else aborts
- with --force: skip prompt, continue to termination

events appended to events.jsonl:
- merge_started (strategy, force)
- merge_prechecks_passed (pr_number, pr_url, branch)
- verify_started (timeout_ms)
- verify_finished (ok, exit_code, duration_ms)
- verify_continue_prompted / accepted / rejected

termination (pr-06b):
- after prechecks + verify: exit E_NOT_IMPLEMENTED
- message: "merge step not implemented in pr-06b; re-run after pr-06c lands"

new helpers:
- parseOriginHost: extract host from scp-like or https URLs
- ghPRViewFull: struct with all required merge fields
- parseGHPRViewFull: parse + validate gh pr view JSON
- isGHPRNotFound: detect "no PR found" patterns in error

tests:
- TestParseOriginHost: scp-like, https, enterprise, empty
- TestParseGHPRViewFull: valid, missing fields, invalid enums
- TestIsGHPRNotFound: nil, not found patterns, other errors
- TestValidatePRState: open, merged, closed, draft, mismatch
- TestCheckMergeability: MERGEABLE, CONFLICTING, UNKNOWN with retries
- TestCheckRemoteHeadUpToDate: up-to-date, fetch fails, sha mismatch

CLI updated:
- dispatch.go: add merge case + runMerge function
- mergeUsageText: document flags, behavior, examples

docs updated:
- README.md: merge command docs, S6 progress
```
