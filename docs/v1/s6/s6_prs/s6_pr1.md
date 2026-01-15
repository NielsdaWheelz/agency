# agency l4: pr-06a spec — archive pipeline + `agency clean` (v1 mvp)

## goal

ship **safe, best-effort archival** + `agency clean <run_id>`:

- run `scripts.archive` (timeout 5m) and capture logs
- kill tmux session for the run (missing session is ok)
- delete the run worktree safely (prefer `git worktree remove`; fallback to guarded `rm -rf`)
- retain `${AGENCY_DATA_DIR}/repos/<repo_id>/runs/<run_id>/` (meta, events, logs)
- update `meta.json` + append events deterministically

## non-goals

- no `gh` / pr logic
- no merge logic
- no verify logic
- no worktree creation changes
- no daemon / pid inspection
- no deleting git branches (local or remote)
- no changes to tmux session naming (keep current `agency_<run_id>`)

## constraints

- destructive command requires explicit typed confirmation: **must type `clean`**
- acquire repo lock; on acquisition print exactly:
  - `lock: acquired repo lock (held during clean/archive)`
- best-effort archive: attempt all steps even if earlier steps fail
- **never `rm -rf` outside** `${AGENCY_DATA_DIR}/repos/<repo_id>/worktrees/` (after Clean + EvalSymlinks)
- `agency clean` is **interactive-only**; scripts are always non-interactive

---

## public surface area

### new command

- `agency clean <run_id>`

### new errors (add if missing)

- `E_ARCHIVE_FAILED`
- `E_WORKTREE_MISSING` (prefer this over inventing `E_WORKSPACE_ARCHIVED` in v1)
- `E_ABORTED` (recommended): user declined / wrong confirmation token
- `E_NOT_INTERACTIVE`: clean requires interactive tty (stdin/stderr not TTY)

---

## behavior spec

### `agency clean`: happy path

given:
- run exists
- `meta.worktree_path` exists on disk

when:
- `agency clean <run_id>`

then:
1) acquire repo lock for `<repo_id>` and print lock line
2) prompt on stderr:
   - `confirm: type 'clean' to proceed: `
   - accept only `clean` after `strings.TrimSpace`
   - otherwise: return `E_ABORTED` (no archive attempted)
3) run archive pipeline (below)
4) if archive pipeline returns success:
   - set `flags.abandoned=true`
   - set `archive.archived_at=<UTC RFC3339>`
   - persist meta atomically
   - append events (below)
   - exit 0

### archive pipeline: steps + failure semantics

archive is 3 sub-steps; always attempt all 3:

1) **archive script**
   - run repo-configured `scripts.archive` with:
     - timeout: 5m
     - stdin: `/dev/null`
     - env injection (see “env”)
     - cwd: `meta.worktree_path`
   - capture combined stdout+stderr to:
     - `${AGENCY_DATA_DIR}/repos/<repo_id>/runs/<run_id>/logs/archive.log` (overwrite)
     - file perms: `0644`
   - non-zero exit or timeout counts as failure

2) **tmux session kill**
   - session name: `agency_<run_id>` (via existing `tmux.SessionName(run_id)` / prefix logic)
   - call existing `tmux.Client.KillSession(name)`
   - if session does not exist: treat as success (not a failure)
   - define “no session” as:
     - exit code 1 with stderr containing any of:
       - `no server running`
       - `can't find session`
       - `no sessions`
   - other errors count as failure

3) **worktree deletion**
   - prefer:
     - `git -C <repo_root> worktree remove --force <worktree_path>`
   - if `<repo_root>` is missing/unresolvable: skip git removal and log reason
   - if git removal fails, fallback:
     - `rm -rf <worktree_path>`
     - **only if** `worktree_path` is within allowed prefix:
       - `${AGENCY_DATA_DIR}/repos/<repo_id>/worktrees/`
    - safety check requirements:
      - compare absolute, cleaned paths
      - resolve symlinks for both prefix + target (`filepath.EvalSymlinks`)
      - require `IsSubpath(resolvedTarget, resolvedPrefix)` (true subpath, not equal)
   - if fallback disallowed: deletion fails

**archive success criteria (v1):**
- success iff:
  - archive script succeeded (exit 0) **AND**
  - worktree deletion succeeded
- tmux kill missing-session is ok
- if any failure occurs:
  - return `E_ARCHIVE_FAILED`
  - do **not** set `archive.archived_at`
  - meta remains (so user can retry)

### missing worktree

if `meta.worktree_path` does not exist:
- return `E_WORKTREE_MISSING`
- do not attempt script/tmux/delete
- do not mutate meta or events (no writes on failure)

### idempotency (clean on already archived)

if meta indicates archived (`archive.archived_at` present):
- treat `agency clean` as idempotent:
  - print a single-line message to stdout: `already archived`
  - exit 0
- do not rewrite anything

(this avoids “re-clean”, reduces footguns)

### run lookup (clean)

- **clean requires being inside the repo** (cwd-based only in v1)
- resolve repo_id from cwd (same as other repo-scoped commands)
- load run by exact run_id under that repo only
- if not inside a repo: `E_NO_REPO` with hint “cd into repo root and retry”

### interactivity

- `agency clean` is interactive-only
- if stdin/stderr are not TTYs: return `E_NOT_INTERACTIVE`; no writes, no lock

---

## env contract for `scripts.archive` (pr-06a)

mirror existing setup/verify env shape; add only what’s needed.

required injected env:
- `AGENCY_RUN_ID`
- `AGENCY_TITLE`
- `AGENCY_REPO_ROOT` (best-effort: from repo record `repo_root_last_seen`; empty if unknown)
- `AGENCY_WORKSPACE_ROOT` (same as worktree root)
- `AGENCY_WORKTREE_ROOT` (alias; set equal to workspace root for clarity)
- `AGENCY_BRANCH`
- `AGENCY_PARENT_BRANCH`
- `AGENCY_ORIGIN_NAME`
- `AGENCY_ORIGIN_URL`
- `AGENCY_RUNNER`
- `AGENCY_PR_URL` (empty string if unknown)
- `AGENCY_PR_NUMBER` (empty string if unknown)
- `AGENCY_DOTAGENCY_DIR` (`<worktree>/.agency/`)
- `AGENCY_OUTPUT_DIR` (`<worktree>/.agency/out/`)
- `AGENCY_LOG_DIR` (`${AGENCY_DATA_DIR}/repos/<repo_id>/runs/<run_id>/logs/`)
- `AGENCY_NONINTERACTIVE=1`
- `CI=1`
optional (allowed but not required in v1):
- `AGENCY_REPO_ID`
- `AGENCY_DATA_DIR`

notes:
- do **not** invent new “secrets” behavior
- do **not** pass args to scripts; just execute the configured path
- follow existing setup/verify runner style (`sh -lc <script>`), for consistency in v1
- if `<repo_root>` is missing and git removal is skipped, write a line into archive.log explaining why

---

## persistence

### files written/updated

- overwrite:
  - `${AGENCY_DATA_DIR}/repos/<repo_id>/runs/<run_id>/logs/archive.log`
- update (atomic):
  - `${AGENCY_DATA_DIR}/repos/<repo_id>/runs/<run_id>/meta.json`
- append:
  - `${AGENCY_DATA_DIR}/repos/<repo_id>/runs/<run_id>/events.jsonl`

### events

append (minimum stable strings):

for `clean`:
- `clean_started`
- `archive_started`
- `archive_finished` with `{ "ok": true }` only on full success
- `archive_failed` with:
  - `{ "script_ok": bool, "tmux_ok": bool, "delete_ok": bool }`
  - plus `reason` strings for failures where available (bounded length, max 512 bytes)
- `clean_finished` with `{ "ok": bool }`

event envelope uses existing `events.AppendEvent(...)` format.

event sequencing rules:
- do not write events or meta before lock acquisition + confirmation
- after confirmation, always write `clean_started`, then `archive_started`
- on failure, write `archive_failed` (with bounded `reason` strings) then `clean_finished`

---

## implementation plan (exact file changes)

### 1) cli wiring

- `internal/cli/dispatch.go`
  - add `clean` to `usageText`
  - add `case "clean": return runClean(...)`
  - use the same flag parsing style as other commands (stdlib `flag`)
  - args: exactly one positional `<run_id>` else `E_USAGE`

### 2) new command implementation

- new: `internal/commands/clean.go`
  - signature should match existing command patterns (look at `stop.go` / `kill.go` / `show.go`)
  - must:
    - require cwd to be inside a repo; resolve repo_id from cwd and read run by **run_id** only
      - if not inside a repo: fail `E_NO_REPO` with hint “cd into repo root and retry”
    - acquire repo lock (`lock.NewRepoLock(...).Lock(repoID, "clean")`)
    - print lock acquisition line
    - if non-interactive: fail `E_NOT_INTERACTIVE` (no writes)
    - prompt typed confirmation only if `tty.IsInteractive(stdin)` is true
    - call archive pipeline
    - update meta on success: `flags.abandoned=true`, `archive.archived_at=...`

### 3) archive pipeline + helpers

- new: `internal/archive/pipeline.go`
  - `type Result struct { ScriptOK, TmuxOK, DeleteOK bool; Err error }`
  - `func Archive(ctx, deps..., meta, repoRecord) (Result, errorCode)`
  - takes:
    - store (paths)
    - exec runner (use existing exec abstraction if present; otherwise introduce minimal interface for test fakes)
    - tmux client
    - repo_root (from repo record; may be empty/missing)
    - allowedPrefix computed from store paths + repo_id

- new: `internal/archive/run_archive_script.go` (or inline)
  - implement script execution mirroring verify/setup:
    - `sh -lc <scriptPath>`
    - cwd=worktree
    - stdin=/dev/null
    - env injection
    - timeout 5m
    - combined output -> archive.log

- new: `internal/worktree/remove.go`
  - `RemoveWorktree(repoRoot, worktreePath) error`
  - run `git -C <repoRoot> worktree remove --force <worktreePath>`

- new: `internal/fs/safe_remove.go`
  - `RemoveAllIfUnderPrefix(target, prefix string) error`
  - implement the Clean + EvalSymlinks + IsSubpath guard
  - if guard fails: return an error that becomes `E_ARCHIVE_FAILED`

- update: `internal/tmux/client_exec.go` (or new helper)
  - add `IsNoSessionErr(err) bool` (string match on stderr is fine in v1)
  - archive pipeline treats no-session as ok

### 4) meta updates

- update meta using existing `store.UpdateMeta(...)`
  - set `flags.abandoned=true` only on full archive success
  - set `archive.archived_at` only on full archive success
  - do not clear other flags in v1

---

## tests

### unit

- `internal/fs/safe_remove_test.go`
  - rejects rm-rf when target is outside prefix
  - rejects when EvalSymlinks fails (fail closed)
  - accepts valid subpath

- `internal/archive/pipeline_test.go`
  - tmux kill “missing session” treated as ok
  - script fail + delete ok => overall failure (E_ARCHIVE_FAILED)
  - delete fail => overall failure

(use dependency injection / fakes: fake exec, fake tmux)

### integration (no github)

- create temp dir as `${AGENCY_DATA_DIR}`
- create fake repo record + fake run meta with worktree path under `${AGENCY_DATA_DIR}/repos/<repo_id>/worktrees/<run_id>`
- create a fake worktree dir on disk
- run `agency clean <run_id>` with stdin providing `clean\n`
- assert:
  - worktree directory gone
  - meta updated (`abandoned=true`, `archived_at` set)
  - archive.log exists
  - events appended

---

## guardrails

- do not touch gh/pr codepaths
- do not delete any git branches (local or remote)
- do not change tmux session naming (`agency_<run_id>`)
- never run archive script in the runner tmux pane
- never `rm -rf` outside allowed prefix
- on `E_ARCHIVE_FAILED`, keep run metadata/logs intact so user can retry
