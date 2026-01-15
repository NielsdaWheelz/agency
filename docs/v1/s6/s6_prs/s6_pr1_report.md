# s6 pr-01 report: archive pipeline + `agency clean`

## summary of changes

implemented the archive pipeline and `agency clean` command for slice 6 (merge + archive):

1. **new `internal/archive/` package** — reusable archive pipeline with 3 steps:
   - run `scripts.archive` with timeout, capture logs
   - kill tmux session (missing session treated as ok)
   - delete worktree (git worktree remove with safe rm-rf fallback)

2. **new `agency clean <run_id>` command** — archives a run without merging:
   - requires interactive TTY for confirmation
   - prompts user to type `clean` to confirm
   - runs archive pipeline
   - marks run as abandoned on success
   - retains metadata and logs

3. **new error codes** — added S6 error codes to errors.go:
   - `E_ARCHIVE_FAILED` — archive step failed
   - `E_ABORTED` — user declined confirmation
   - `E_NOT_INTERACTIVE` — command requires interactive TTY
   - plus future S6 codes: `E_GIT_FETCH_FAILED`, `E_REMOTE_OUT_OF_DATE`, `E_PR_DRAFT`, `E_PR_MISMATCH`, etc.

4. **new `internal/fs/safe_remove.go`** — safe rm-rf with allowed-prefix guard:
   - uses `filepath.Clean` + `filepath.EvalSymlinks` to prevent path trickery
   - only deletes if target is a true subpath of allowed prefix
   - returns `ErrNotUnderPrefix` if guard fails

5. **new `internal/worktree/remove.go`** — git worktree remove wrapper:
   - runs `git -C <repo_root> worktree remove --force <path>`
   - returns structured result for archive pipeline

6. **tmux helpers** — added `IsNoSessionErr()` to detect "no session" errors:
   - treats missing session as ok during archive (not a failure)

7. **event helpers** — added clean/archive event data helpers:
   - `CleanStartedData`, `CleanFinishedData`
   - `ArchiveStartedData`, `ArchiveFinishedData`, `ArchiveFailedData`

## problems encountered

1. **exec package type name** — tests initially used `exec.RunResult` which doesn't exist; the correct type is `exec.CmdResult`. fixed by reading the actual source.

2. **test nil pointer** — initial test tried to call real tmux client with nil runner, causing panic. fixed by setting killErr directly on fake client.

3. **unused import** — clean.go had unused `path/filepath` import. fixed by removing it.

## solutions implemented

1. **archive pipeline is best-effort** — per spec, all 3 steps are attempted regardless of earlier failures:
   - script failure doesn't block tmux kill
   - tmux failure doesn't block worktree deletion
   - result tracks success/failure per step

2. **safe rm-rf fallback** — worktree deletion tries git first, then falls back to rm-rf only if:
   - path is under `${AGENCY_DATA_DIR}/repos/<repo_id>/worktrees/`
   - symlinks resolved to prevent `../` attacks

3. **archive success criteria** — success requires:
   - script exit 0 AND worktree deleted
   - tmux kill failure alone doesn't fail archive (missing session is ok)

4. **idempotent clean** — if run is already archived (`archive.archived_at` set):
   - prints `already archived` and exits 0
   - no re-archiving, no errors

## decisions made

1. **stdin piped to /dev/null for archive script** — consistent with setup/verify scripts per L0 contract.

2. **sh -lc for script execution** — matches existing setup/verify runner pattern for consistency.

3. **lock message format** — `lock: acquired repo lock (held during clean/archive)` per s6_pr1 spec.

4. **confirmation token** — must type exactly `clean` (after trim), not `y` or `yes`.

5. **osEnv reuse** — used existing `osEnv` from doctor.go package; no need to redefine.

6. **event sequencing** — events written in order:
   - `clean_started` → `archive_started` → `archive_finished|archive_failed` → `clean_finished`

## deviations from spec

1. **added more S6 error codes** — added error codes for future PR-02/PR-03 (merge prechecks) in this PR for completeness. they're defined but not yet used.

2. **archive.log path** — stored in `result.LogPath` and printed on success. spec didn't explicitly require this but it's useful for debugging.

3. **warning on meta update failure** — if meta.json update fails after successful archive, prints warning instead of failing. archive is done; meta update is best-effort.

## how to run new/changed commands

### `agency clean`

```bash
# archive a run without merging
agency clean <run_id>

# example
agency clean 20260115-a3f2
```

**confirmation prompt:**
```
lock: acquired repo lock (held during clean/archive)
confirm: type 'clean' to proceed: clean
cleaned: 20260115-a3f2
log: /path/to/logs/archive.log
```

**requires:**
- cwd inside a git repo with agency.json
- interactive terminal (stdin/stderr are TTYs)
- run exists and worktree is present

### how to test

```bash
# build
go build ./...

# run all tests
go test ./...

# run specific tests
go test ./internal/fs/... -v          # safe_remove tests
go test ./internal/archive/... -v     # archive pipeline tests

# manual test
cd /path/to/agency-enabled-repo
agency run --title "test clean"       # create a run
agency clean <run_id>                 # type 'clean' to confirm
agency ls                             # should show run as "abandoned (archived)"
```

### verify archive cleanup worked

```bash
# check worktree deleted
ls ${AGENCY_DATA_DIR}/repos/<repo_id>/worktrees/<run_id>/  # should not exist

# check tmux session killed
tmux has-session -t agency_<run_id>  # should return exit 1

# check metadata retained
cat ${AGENCY_DATA_DIR}/repos/<repo_id>/runs/<run_id>/meta.json
# should have:
#   "flags": { "abandoned": true }
#   "archive": { "archived_at": "..." }

# check logs retained
cat ${AGENCY_DATA_DIR}/repos/<repo_id>/runs/<run_id>/logs/archive.log
```

## branch name and commit message

**branch:** `pr6/s6-pr1-archive-pipeline-clean`

**commit message:**
```
feat(s6): add archive pipeline + agency clean command

implement slice 6 PR-01: safe, best-effort archival + agency clean

archive pipeline (internal/archive/):
- run scripts.archive with 5m timeout, capture logs
- kill tmux session (missing session is ok)
- delete worktree via git worktree remove, fallback to safe rm-rf
- best-effort: all steps attempted regardless of earlier failures
- success = script ok AND delete ok (tmux failure alone is ok)

agency clean command:
- requires interactive TTY for confirmation (must type 'clean')
- acquires repo lock during clean/archive
- runs archive pipeline
- on success: sets flags.abandoned=true, archive.archived_at
- retains metadata and logs in AGENCY_DATA_DIR
- idempotent: already-archived runs print message and exit 0

new error codes:
- E_ARCHIVE_FAILED: archive step failed
- E_ABORTED: user declined confirmation
- E_NOT_INTERACTIVE: command requires interactive TTY
- E_GIT_FETCH_FAILED, E_REMOTE_OUT_OF_DATE, E_PR_DRAFT,
  E_PR_MISMATCH, E_GH_REPO_PARSE_FAILED, E_PR_MERGEABILITY_UNKNOWN,
  E_GH_PR_MERGE_FAILED, E_PR_NOT_MERGEABLE, E_NO_PR (future S6)

safety guards:
- rm-rf fallback only under allowed prefix
- filepath.Clean + EvalSymlinks to prevent path trickery
- IsSubpath check (true subpath, not equal)

new helpers:
- internal/fs/safe_remove.go: SafeRemoveAll with prefix guard
- internal/worktree/remove.go: git worktree remove wrapper
- internal/tmux/client_exec.go: IsNoSessionErr helper
- internal/events/append.go: clean/archive event data helpers

tests:
- internal/fs/safe_remove_test.go: prefix guard tests
- internal/archive/pipeline_test.go: archive pipeline tests

docs updated:
- README.md: added agency clean docs, S6 progress, project structure
```
