# agency l4: pr-04c spec — resume (create/attach/restart/detached) + worktree missing + locking

> slice: 04 lifecycle control  
> pr: 04c  
> repo: agency (go)  
> assumes pr-04a (tmux client interface) and pr-04b (attach/stop/kill) are merged.

## 0) goal

implement `agency resume <id>` with minimal, deterministic semantics:

- attach to an existing tmux session if present
- create a tmux session (and start the runner) only when missing
- support `--detached`, `--restart`, `--yes`
- enforce safe restart confirmation in interactive terminals
- never run scripts, never touch git, never mutate meta (events only)
- acquire repo lock **only** when creating or restarting a session (double-check under lock)

## 1) strict scope

in-scope:
- CLI: add `resume` subcommand and flags: `--detached`, `--restart`, `--yes`
- implement resume behavior per the contract below
- add tty helpers to support confirmation gating (no build tags required in v1)
- refactor runner resolution into a shared helper and use it in both run + resume
- add new error codes:
  - `E_WORKTREE_MISSING`
  - `E_CONFIRMATION_REQUIRED`
- append resume events:
  - `resume_attach`
  - `resume_create`
  - `resume_restart`
  - `resume_failed`
- unit tests for resume using fake tmux client + temp fs + fake lock hook

out-of-scope:
- any PID/process-tree inspection
- transcript replay / context injection
- any setup/verify/archive script execution
- any git mutations
- any runner argv-array config support (string-only runner command in v1)
- any status derivation changes (other than events/flags already defined)
- any meta.json mutations (avoid unknown-field loss)

## 2) public surface area

### command added

agency resume  [–detached] [–restart] [–yes]

### flags

- `--detached`: do not attach; return after ensuring session exists (and runner started if created)
- `--restart`: if session exists, optionally confirm; then kill and recreate session
- `--yes`: skip confirmation prompt for `--restart` (only meaningful when session exists)

### output rules

- **default (attach path)**: prints nothing to stdout (tmux takes over); any warnings/prompts go to stderr
- **`--detached` success**: print a single line to stdout:
  - `ok: session <session_name> ready` (use `tmux.SessionName(run_id)`)
- all errors print via existing CLI behavior (stderr); command returns an error
- output is produced by the resume command implementation; the CLI dispatcher should not print extra lines for resume.

## 3) required behavior

### 3.1 run lookup + meta read

- resolve `<id>` via `internal/ids` resolution rules (exact or unique prefix)
- if not found: `E_RUN_NOT_FOUND`
- read run meta via store (`store.ReadMeta`)

### 3.2 worktree existence gate (before tmux actions)

- validate `meta.worktree_path` exists and is a directory
- if missing:
  - if `meta.archive.archived_at != ""` (non-empty / non-null timestamp) => treat as archived
  - else => treat as corrupted/missing
- in both missing cases:
  - append event `resume_failed` with `data.reason`:
    - `"archived"` or `"missing"`
  - return `E_WORKTREE_MISSING` with message:
    - archived: `run is archived; cannot resume`
    - missing: `worktree missing; run is corrupted`

**note:** do not introduce a boolean `flags.archived`. archived predicate is timestamp-only.

### 3.3 session existence check (no lock)

- compute session name: `internal/tmux.SessionName(meta.run_id)`
- check existence via `TmuxClient.HasSession(sessionName)`
- if `--restart` is NOT set:
  - if session exists:
    - append event `resume_attach` with `data.detached` (bool)
    - if `--detached`: print success line and exit 0
    - else: `TmuxClient.Attach(sessionName)` and return its result
  - if session missing:
    - proceed to session creation path (requires lock)

### 3.4 restart path

if `--restart` is set:

1) if session exists:
   - **confirmation gate**:
     - if `--yes` is provided: skip prompt
     - else:
       - prompt only if **both** stdin and stderr are TTYs (see tty helper)
       - prompt text (to stderr):
         - `restart session? in-tool history will be lost (git state unchanged) [y/N]: `
       - read from stdin; accept `y` or `yes` (case-insensitive) as yes
       - otherwise: treat as user cancel:
         - print `canceled` to stderr and exit 0 without changes
       - if not a tty and `--yes` missing:
         - return `E_CONFIRMATION_REQUIRED` with message:
           - `refusing to restart without confirmation in non-interactive mode; pass --yes`

2) acquire repo lock (see 3.6), then:
   - re-check `HasSession` under lock
   - if exists: `KillSession`
   - create new session: `NewSession(sessionName, meta.worktree_path, runnerArgv)`
   - append event `resume_restart` with:
     - `data.detached` (bool)
     - `data.session_name`
     - `data.runner` (meta.runner)
     - `data.restart=true`
   - if `--detached`: print success line and exit 0
   - else attach

### 3.5 create-missing-session path

if session missing and not restart:

- acquire repo lock, then:
  - re-check `HasSession` under lock
  - if still missing:
    - create via `NewSession(sessionName, meta.worktree_path, runnerArgv)`
    - append event `resume_create` with:
      - `data.detached`
      - `data.session_name`
      - `data.runner`
      - `data.restart=false`
  - else (race): treat as attach path:
    - append `resume_attach` and attach or detached behavior

event invariant:
- on any successful resume command (attach/create/restart), append exactly one event:
  - `resume_attach`, `resume_create`, or `resume_restart` respectively (even in race attach path)

### 3.6 lock rules

- resume takes the repo lock **only** when it will create or restart a session:
  - create path (session missing)
  - restart path (always)
- resume does **not** take lock if session exists and `--restart` is not set

double-check pattern:
- check session existence without lock
- if mutation needed, acquire lock, re-check, then mutate

lock API:
- use `internal/lock` repo-level lock keyed by repo_id
- if lock acquisition fails: return `E_REPO_LOCKED` (existing behavior)

### 3.7 runner command resolution (string-only)

runner argv for tmux `NewSession`:

- do not add argv-array support in this PR.
- resolved command is `[]string{cmd}` where:
  - if `agency.json.runners.<runner>` exists: it must be a string command; use it
  - else if runner is `claude` or `codex`: use that string (PATH)
  - else: `E_RUNNER_NOT_CONFIGURED`

**implementation constraint:** resume must reuse the same runner resolution logic used by `agency run` (do not duplicate semantics).
**required change:** introduce `internal/config/runner.go` with:
  - `ResolveRunnerCmd(cfg *AgencyConfig, runnerName string) (string, error)`
  - refactor `agency run` to call this helper; resume must call the same helper.

### 3.8 tmux invocation invariants

`NewSession` must create:
- detached session (`-d`)
- name `-s <session_name>` (use `tmux.SessionName(run_id)`)
- cwd via tmux `-c <worktree>`
- exec-style args with `-- <cmd> <args...>` (no shell quoting)

attach:
- `tmux attach -t <name>`

## 4) persistence

### events

append-only to `${AGENCY_DATA_DIR}/repos/<repo_id>/runs/<run_id>/events.jsonl`:

- `resume_attach`
- `resume_create`
- `resume_restart`
- `resume_failed`

event data minimum keys:
- `session_name`
- `runner`
- `detached` (bool)
- `restart` (bool)

for `resume_failed`, include:
- `reason`: `"archived"` | `"missing"`

events path note:
- event writes must use the run directory under `${AGENCY_DATA_DIR}` even if the worktree is missing.
- do not attempt to write under `<worktree>/.agency/`.

### meta.json

**no mutations in this PR.**
(do not set flags; do not write timestamps; avoid unknown-field loss.)

## 5) error codes

add to `internal/errors`:

- `E_WORKTREE_MISSING`
- `E_CONFIRMATION_REQUIRED`

use existing:
- `E_RUN_NOT_FOUND`
- `E_REPO_LOCKED`
- `E_TMUX_NOT_INSTALLED` (if tmux missing, via tmux client)
- `E_RUNNER_NOT_CONFIGURED`

## 6) files to modify / add

### add
- `internal/commands/resume.go` — resume command implementation
- `internal/tty/tty.go` — tty helpers:
  - `IsTTY(f *os.File) bool`
  - `IsInteractive() bool` (stdin+stderr are ttys)

### modify
- `internal/cli/dispatch.go`
  - add `resumeUsageText`
  - add `runResume(args []string) error`
  - add switch case for `resume`
- `internal/errors/codes.go` (or equivalent) — register new codes
- `internal/events/` — ensure resume event names are used consistently (no new infra required)
- `internal/config/` or shared helper location — ensure runner resolution is reusable by resume
  - **constraint:** do not expand schema; no argv arrays
  - add `internal/config/runner.go` and refactor `agency run` to use it

## 7) tests

### automated (required)

create `internal/commands/resume_test.go`:

- use a fake tmux client (from pr-04a) with programmable behavior:
  - session exists vs missing
  - record calls: HasSession/NewSession/Attach/KillSession
- use temp directories to model:
  - existing worktree path (dir exists)
  - missing worktree path
- use a fake lock implementation or hook:
  - easiest: expose a `Lock` interface consumed by commands, or allow injecting a `lock.Acquirer` into Resume command constructor.
  - tests must assert lock is acquired only on create/restart paths

test cases (table-driven where possible):

1) session exists, no restart, not detached:
   - no lock
   - event `resume_attach`
   - attach called

2) session exists, no restart, detached:
   - no lock
   - event `resume_attach` (detached true)
   - attach NOT called
   - stdout contains `ok: session ... ready`

3) session missing, create path, not detached:
   - lock acquired
   - NewSession called with correct name/cwd/argv
   - event `resume_create`
   - attach called

4) session missing, create path, detached:
   - lock acquired
   - NewSession called
   - event `resume_create`
   - attach not called
   - stdout ok line

5) restart, session exists, `--yes`:
   - lock acquired
   - KillSession then NewSession
   - event `resume_restart`
   - attach called unless detached

6) restart, session exists, not tty, no `--yes`:
   - return `E_CONFIRMATION_REQUIRED`
   - no lock acquired
   - no tmux kill/new
   - no events written (since action not taken)
7) restart, session exists, tty, user answers "no":
   - exit 0
   - no lock acquired
   - no tmux kill/new
   - no events written

7) worktree missing + archived_at set:
   - return `E_WORKTREE_MISSING`
   - event `resume_failed` with reason `archived`

8) worktree missing + not archived:
   - return `E_WORKTREE_MISSING`
   - event `resume_failed` with reason `missing`

### tty tests

- unit test `IsTTY` behavior by injecting stubs is hard in pure Go without build tags.
- in v1, keep tty helper minimal and test it indirectly by **overriding a package-level function** in `internal/commands/resume.go`, e.g.:
  - `var isInteractive = tty.IsInteractive`
- tests override `isInteractive` temporarily to force interactive/non-interactive behavior.

**explicit requirement:** do not write flaky tests that depend on CI’s tty state.

## 8) prompt behavior (restart confirmation)

prompting is inside the `resume` command implementation (not the CLI dispatcher):

- prompt printed to stderr
- read a single line from stdin
- accept `y` / `yes` as confirmation
- any other response => print `canceled` to stderr and exit 0

if non-interactive (stdin or stderr not tty) and `--yes` not provided:
- `E_CONFIRMATION_REQUIRED` with message:
  - `refusing to restart without confirmation in non-interactive mode; pass --yes`

## 9) guardrails (do not do)

- do not mutate meta.json
- do not run scripts
- do not touch git
- do not add config schema support for argv arrays
- do not implement pid inspection
- do not add transcript replay
- do not change status derivation logic
- do not call os/exec directly for tmux; always use the tmux client

## 10) manual demo script (after merge)

```bash
# have an existing run id from slice 1
agency kill <id>
agency attach <id>   # should fail with E_SESSION_NOT_FOUND from pr-04b

# recreate session + attach
agency resume <id>

# detached mode
agency kill <id>
agency resume <id> --detached
agency attach <id>

# restart (interactive)
agency resume <id> --restart

11) definition of done
	•	agency resume wired in CLI and matches behavior contract
	•	new error codes exist and are stable
	•	events written on resume paths; resume_failed written on missing worktree paths
	•	repo lock acquired only for create/restart
	•	go test ./... passes; no tmux required in CI
