# Agency PR-2 Spec — Slice 03 (push): preflight + git fetch/ahead + git push

path: `docs/slices/slice-03/pr-3b_spec.md`

## goal
implement `agency push <run_id> [--force]` up through a successful **git push**:
- deterministic, non-interactive, idempotent where possible
- strict preflight (origin/github.com, report gating, gh auth)
- git fetch + parent-ref resolution + ahead check + `git push -u`
- persist `last_push_at` + append push events

this PR does **not** create/update GitHub PRs (gh pr create/edit/view deferred to PR-3C).

---

## scope

### user-visible behavior
`agency push <run_id> [--force]`:

1) loads run metadata and validates workspace presence
2) acquires repo lock (mutating command)
3) runs preflight checks (no network side effects before passing):
   - origin exists and host is exactly `github.com`
   - report gating (missing/empty requires `--force`)
   - warns if worktree is dirty
   - `gh auth status` succeeds
4) runs git operations (network side effects begin only here):
   - `git fetch origin`
   - resolve parent ref (`<parent_branch>` local preferred, else `origin/<parent_branch>`)
   - compute ahead commits vs parent; refuse if ahead==0
   - `git push -u origin <workspace_branch>`
5) persists meta + emits events:
   - on success: `meta.last_push_at = now`
   - append events: `push_started`, `git_fetch_finished`, `git_push_finished`, `push_finished`
   - on failure: append `push_failed` with error_code

### files / modules in play (expected)
- command wiring:
  - `cmd/agency/main.go` (or cobra/urfave equivalent)
  - `internal/commands/push.go` (new)
- storage:
  - `internal/store/meta.go` (read/write meta.json)
  - `internal/store/events.go` (append events.jsonl) (from PR-3A)
- exec:
  - `internal/exec/cmdrunner.go` (CmdRunner interface + real impl) (from PR-3A)
- git helpers:
  - `internal/git/origin.go` (get origin url, parse host)
  - `internal/git/refs.go` (ref existence checks)
  - `internal/git/ahead.go` (ahead computation)
- locking:
  - `internal/lock/repo_lock.go` (from earlier slice or PR-3A integration point)

(names can differ; intent matters.)

---

## non-scope
- gh PR creation/update (`gh pr create`, `gh pr edit`, `gh pr view`)
- any PR metadata persistence (`pr_number`, `pr_url`)
- `last_report_sync_at`, `last_report_hash` (deferred to PR-3C)
- merge, verify, PR checks parsing
- force-push, rebase, reset, merge
- interactive prompts of any kind (including gh/git prompts)
- draft PRs, forks/upstream-base PRs, GitHub Enterprise support

---

## public surface area

### commands
- `agency push <run_id> [--allow-dirty] [--force]`

### flags
- `--allow-dirty`
  - allows proceeding when worktree has uncommitted changes
  - without this flag: `E_DIRTY_WORKTREE`
- `--force`
  - allows proceeding when `.agency/report.md` is missing or effectively empty
  - **does not** bypass `E_EMPTY_DIFF`
  - **does not** enable force-push
  - when used with missing/empty report: push proceeds, but no PR functionality exists in this PR anyway

### stdout/stderr contract (v1 for this PR)
- on success: prints a single line to stdout:
  - `pushed <branch> to origin`
- warnings to stderr:
  - dirty worktree warning (only when `--allow-dirty` is used)
  - report missing/empty warning when `--force`
- errors:
  - print error code + one-line explanation to stderr
  - include captured stderr for `git fetch`/`git push` failures

---

## error codes

### introduced (public contract)
- `E_REPORT_INVALID` — report missing/empty without `--force`
- `E_NO_ORIGIN` — origin remote missing
- `E_UNSUPPORTED_ORIGIN_HOST` — origin host not exactly `github.com`
- `E_PARENT_NOT_FOUND` — neither `<parent_branch>` nor `origin/<parent_branch>` exists after fetch
- `E_GIT_PUSH_FAILED` — `git push` non-zero exit
- `E_WORKTREE_MISSING` — run exists but `meta.worktree_path` missing on disk
- `E_DIRTY_WORKTREE` — worktree has uncommitted changes without `--allow-dirty`

### referenced existing
- `E_RUN_NOT_FOUND`
- `E_GH_NOT_AUTHENTICATED`
- `E_GH_NOT_INSTALLED`
- `E_EMPTY_DIFF`
- `E_REPO_LOCKED`

---

## behavior spec (given/when/then)

### preflight ordering (no side effects before passing)
**given** a valid run id  
**when** `agency push <id>` runs  
**then** it MUST perform checks in this order (and abort immediately on failure):

1. load `meta.json` for run
2. ensure worktree exists on disk (`meta.worktree_path` directory present)
   - else `E_WORKTREE_MISSING`
3. acquire repo lock
   - else `E_REPO_LOCKED`
4. ensure `origin` exists
   - implement via `git remote get-url origin`
   - else `E_NO_ORIGIN`
5. ensure origin host is exactly `github.com`
   - parse ssh/https formats; if hostname != `github.com` => `E_UNSUPPORTED_ORIGIN_HOST`
6. report gating:
   - report path: `<worktree>/.agency/report.md`
   - “effectively empty” = missing OR trimmed length < 20 chars
   - if empty AND `--force` not set => `E_REPORT_INVALID`
   - if empty AND `--force` set => warn to stderr and continue
7. dirty worktree gate:
   - run `git status --porcelain --untracked-files=all`
   - if non-empty AND `--allow-dirty` not set => `E_DIRTY_WORKTREE`
   - if `--allow-dirty` set => warn to stderr and continue
8. ensure gh is installed + authenticated
   - run `gh auth status`
   - if missing gh binary => `E_GH_NOT_INSTALLED`
   - if non-zero => `E_GH_NOT_AUTHENTICATED`

only after (1–8) pass may the command run any network-affecting git operations.

### non-interactive enforcement (hard requirement)
all `git` and `gh` subprocesses executed by `agency push` MUST:
- have stdin connected to `/dev/null`
- run with env overlay:
  - `GIT_TERMINAL_PROMPT=0`
  - `GH_PROMPT_DISABLED=1`
  - `CI=1`
  - plus the standard `AGENCY_*` envs already defined in L0 for scripts/commands as applicable
  - optional: set `GIT_ASKPASS` to a `false` binary resolved from PATH if present

(note: scripts are not run in this PR; this is for cmdrunner calls.)

### events (observability)
on entry (after meta loaded, before lock acquisition), append:
- `push_started`

on failure at any step after `push_started`, append:
- `push_failed` with data:
  - `error_code`
  - `step` (string identifier; e.g. `origin_check`, `gh_auth`, `report_gate`, `git_fetch`, `ahead_check`, `git_push`)
  - optionally `stderr_tail` for git failures
  - include lock failures (e.g., `step=repo_lock`) so `push_started` is always paired with `push_failed` on abort

on success, append:
- `git_fetch_finished` (duration_ms)
- `git_push_finished` (duration_ms)
- `push_finished`

### fetch (non-destructive)
**given** preflight passes  
**when** push proceeds  
**then** it MUST run, in worktree cwd:
- `git fetch origin`

it MUST NOT run rebase/reset/merge or modify any branch pointers.

### parent ref resolution
**given** `meta.parent_branch = P` and fetch completed  
**then** resolve `parent_ref`:

1) if local ref exists: `refs/heads/P` => `parent_ref = P`  
2) else if remote ref exists: `refs/remotes/origin/P` => `parent_ref = origin/P`  
3) else => `E_PARENT_NOT_FOUND`

ref existence check implementation MUST be deterministic, e.g.:
- `git show-ref --verify --quiet refs/heads/<P>`
- `git show-ref --verify --quiet refs/remotes/origin/<P>`

### ahead check (hard gate)
compute:
- `ahead = git rev-list --count <parent_ref>..<workspace_branch>`

if `ahead == 0`:
- abort with `E_EMPTY_DIFF`
- `--force` MUST NOT bypass this

### push behavior
**when** ahead > 0  
**then** it MUST run:
- `git push -u origin <workspace_branch>`

it MUST NOT use `--force` or `--force-with-lease`.

on non-zero exit:
- `E_GIT_PUSH_FAILED`
- surface captured stderr (verbatim) in error output

### metadata persistence
on successful git push:
- update `meta.json`:
  - `last_push_at` = now (RFC3339)

do NOT set/modify:
- `pr_number`, `pr_url`
- `last_report_sync_at`, `last_report_hash` (deferred)

---

## persistence

writes in this PR:
- `${AGENCY_DATA_DIR}/repos/<repo_id>/runs/<run_id>/meta.json`
  - update `last_push_at` on success
- `${AGENCY_DATA_DIR}/repos/<repo_id>/runs/<run_id>/events.jsonl`
  - append events described above

no new files created in repo root. no new files created in worktree beyond what slice 01 created.

---

## tests

### unit tests (table-driven; mocked CmdRunner)
must cover:

1) origin gating
- no origin => `E_NO_ORIGIN`
- origin host not github.com => `E_UNSUPPORTED_ORIGIN_HOST`

2) gh auth gate
- gh missing => `E_GH_NOT_INSTALLED`
- gh auth status non-zero => `E_GH_NOT_AUTHENTICATED`

3) report gating
- report missing/empty without `--force` => `E_REPORT_INVALID`
- report missing/empty with `--force` => continues (assert warning emitted)
- report present and non-empty => continues

4) parent ref resolution
- local parent exists => uses `<parent_branch>`
- local missing but origin/<parent> exists => uses `origin/<parent>`
- neither exists => `E_PARENT_NOT_FOUND`

5) ahead check
- ahead==0 => `E_EMPTY_DIFF` even with `--force`

6) push invocation
- asserts `git push -u origin <branch>` called
- push failure => `E_GIT_PUSH_FAILED` and stderr surfaced

7) non-interactive env overlay
- every git/gh call includes:
  - `GIT_TERMINAL_PROMPT=0`
  - `GH_PROMPT_DISABLED=1`
  - `CI=1`
(verify via captured call args in mock)
- optional: if a `false` binary is present on PATH, `GIT_ASKPASS` is set to it

### integration test (local-only; no github)
- create temp repo with parent branch
- create bare repo as origin remote
- create an agency run worktree dir + minimal meta.json fixture (or use earlier slice helpers if available)
- commit ahead on workspace branch
- run `agency push <id>`
- assert bare origin has the branch ref (e.g. `git show-ref refs/heads/<branch>` in bare repo)

---

## guardrails
- do not call `gh pr create/edit/view`
- do not parse PR URLs/numbers
- do not run any scripts (setup/verify/archive)
- do not rebase/reset/merge
- do not force push
- do not update PR title/body
- do not write `last_report_sync_at` or `last_report_hash` in this PR
- enforce non-interactive subprocess env + stdin=/dev/null for all calls

---

## demo commands (manual)
```bash
# assumes an existing run with commits ahead
agency push <run_id>

# missing/empty report case:
agency push <run_id> --force
```
