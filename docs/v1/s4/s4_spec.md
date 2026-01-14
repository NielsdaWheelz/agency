# Agency L2: Slice 04 — lifecycle control (stop/kill/resume + flags)

## goal
add minimal, reliable lifecycle controls for runner tmux sessions: interrupt (`stop`), destroy session (`kill`), and ensure a live session (`resume`), without touching PR/merge/archive logic.

## scope
in-scope:
- `agency stop <id>`: best-effort interrupt of the runner via tmux key injection; sets `needs_attention` flag when it actually targets an existing session.
- `agency kill <id>`: kill the tmux session; workspace remains intact.
- `agency resume <id> [--detached] [--restart]`: ensure a tmux session exists for the run and starts the configured runner command if needed; attach unless detached.
- `agency attach <id>` behavior refinement: if tmux session missing, error `E_SESSION_NOT_FOUND` and suggest `resume`.
- persistence updates: `meta.json` flag mutations and event logging for stop/kill/resume actions.
- add a tmux abstraction layer in Go to make this slice unit-testable without requiring tmux in CI.

out-of-scope:
- pid/process-tree inspection for “runner alive” (v1 uses session existence only).
- transcript replay / context rehydration into the runner on resume.
- launching setup/verify/archive scripts from resume (resume never runs scripts).
- changing status derivation logic beyond what’s needed for flags/events consistency.
- planner/council/headless automation.
- deleting worktrees (archive/clean remain in later slices).

---

## public surface area

### commands added/changed
added:
- `agency stop <id>`
- `agency kill <id>`
- `agency resume <id> [--detached] [--restart]`

changed:
- `agency attach <id>` now errors with `E_SESSION_NOT_FOUND` when tmux session is missing (instead of a generic error).

### flags / behavior
- `stop` sets `meta.flags.needs_attention = true` **only** if the tmux session existed and Agency attempted to send keys.
- `kill` does not set flags by default.
- `resume --restart` kills the session (if present) and recreates it; no confirmation prompt in v1 (prints a loud warning).

---

## commands + flags

### `agency stop <id>`
behavior:
- if run not found: `E_RUN_NOT_FOUND`
- if tmux session missing: print `no session for <id>` and exit 0 (no-op)
- if session exists:
  - send `C-c` then `Enter` to the primary pane
  - set `flags.needs_attention=true`
  - append event `stop` to `events.jsonl`

notes:
- best-effort only. does not guarantee the runner stops, exits, or cancels a tool action.

### `agency kill <id>`
behavior:
- if run not found: `E_RUN_NOT_FOUND`
- if tmux session missing: print `no session for <id>` and exit 0 (no-op)
- if session exists:
  - `tmux kill-session -t agency:<run_id>`
  - append event `kill_session` to `events.jsonl`

### `agency resume <id> [--detached] [--restart]`
flags:
- `--detached`: do not attach; return after ensuring session exists (and runner started when creating session).
- `--restart`: kill existing session (if any), then recreate session and start runner.

behavior:
- if run not found: `E_RUN_NOT_FOUND`
- validate worktree path exists; if missing, error `E_WORKTREE_MISSING` (new code) with message: run is archived or corrupted; cannot resume.
- if `--restart`:
  1) if session exists, kill it
  2) create new session with cwd=`meta.worktree_path` and command=`resolved runner cmd`
  3) append event `resume_restart`
  4) attach unless `--detached`
- else (default):
  - if session exists: append event `resume_attach` and attach unless detached
  - if session missing:
    1) create new session with cwd=`meta.worktree_path` and command=`resolved runner cmd`
    2) append event `resume_create`
    3) attach unless detached

warning output for `--restart`:
- print: `restarting session; in-tool history will be lost (git state unchanged)`

### `agency attach <id>`
behavior change:
- if run not found: `E_RUN_NOT_FOUND`
- if tmux session missing: `E_SESSION_NOT_FOUND` with suggestion `agency resume <id>`
- else: attach to session

---

## files created / modified

### modified
- Go CLI entrypoint / command router: add stop/kill/resume; refine attach error handling.
- tmux integration module:
  - introduce `TmuxClient` interface (see below)
  - default implementation executes `tmux` via subprocess
- metadata writer:
  - set `meta.flags.needs_attention` on stop (session existed)
- events writer:
  - append events for stop/kill/resume flows

### new (recommended)
- `internal/tmux/tmux.go` (or similar)
- `internal/tmux/interface.go` defining `TmuxClient`
- tests under `internal/tmux/..._test.go` using fake client
- if needed: `internal/runtime/session.go` helper to compute session name `agency:<run_id>`

no changes to:
- `agency.json` schema
- setup/verify/archive scripts
- PR creation, merge, archive, clean

---

## error codes

### new
- `E_SESSION_NOT_FOUND` — `attach` when tmux session is missing; suggests `resume`.
- `E_WORKTREE_MISSING` — run exists but `meta.worktree_path` does not exist on disk (archived or corrupted); resume cannot proceed.

existing used:
- `E_RUN_NOT_FOUND`
- `E_TMUX_NOT_INSTALLED` (if tmux executable missing)

---

## behaviors (given/when/then)

### stop: existing session
given:
- run `<id>` exists
- tmux session `agency:<id>` exists

when:
- `agency stop <id>`

then:
- agency executes tmux send-keys to session pane: `C-c` then `Enter`
- `meta.flags.needs_attention` becomes `true`
- an event line is appended:
  - `event="stop"`, includes `{ "keys": ["C-c", "Enter"] }`
- exit code 0

### stop: missing session
given:
- run `<id>` exists
- tmux session `agency:<id>` does not exist

when:
- `agency stop <id>`

then:
- no-op, prints `no session for <id>`
- does **not** mutate flags
- appends event `stop_no_session`
- exit code 0

### kill: existing session
given:
- run `<id>` exists
- tmux session exists

when:
- `agency kill <id>`

then:
- session is killed
- workspace remains
- event `kill_session`
- exit code 0

### kill: missing session
given:
- run exists
- session missing

when:
- `agency kill <id>`

then:
- no-op, prints `no session for <id>`
- event `kill_no_session`
- exit code 0

### resume: attach existing
given:
- run exists
- session exists

when:
- `agency resume <id>`

then:
- attaches to tmux session (unless `--detached`)
- event `resume_attach`
- exit code 0

### resume: create missing session
given:
- run exists
- session missing
- worktree path exists

when:
- `agency resume <id>`

then:
- creates tmux session with:
  - name `agency:<run_id>`
  - cwd = worktree path
  - command = resolved runner command (see runner resolution below)
- attaches unless detached
- event `resume_create`
- exit code 0

### resume --restart
given:
- run exists
- worktree exists
- session may or may not exist

when:
- `agency resume <id> --restart`

then:
- kills session if present
- recreates session and starts runner
- prints warning about losing in-tool history
- attaches unless detached
- event `resume_restart`
- exit code 0

### attach: missing session
given:
- run exists
- session missing

when:
- `agency attach <id>`

then:
- exits non-zero with `E_SESSION_NOT_FOUND`
- message includes: `try: agency resume <id>`

---

## runner command resolution (must match L0)
for a run, the resolved runner command is:
1) if `agency.json.runners.<runner>` exists and is non-empty: use it
2) else if runner is `claude` or `codex`: use the literal string (`claude` / `codex`) and rely on PATH
3) else: error `E_RUNNER_NOT_CONFIGURED`

execution in tmux:
- use tmux `-c <worktree>` for cwd
- command string executed via `sh -lc "<cmd>"` to preserve expected shell PATH behavior

---

## persistence

### meta.json mutations
- `stop`: set `flags.needs_attention=true` only when session existed and keys were sent.
- no other command mutates flags in this slice.

### events.jsonl additions
append-only, one event per command execution:
- `stop`, `stop_no_session`
- `kill_session`, `kill_no_session`
- `resume_attach`, `resume_create`, `resume_restart`
- optionally include:
  - `data.session_name`
  - `data.runner`
  - `data.detached`
  - `data.restart`

---

## tests

### automated (required)
unit tests (table-driven):
- session name derivation: run_id → `agency:<run_id>`
- stop behavior:
  - when fake tmux says session exists → sends keys `C-c`, `Enter`, sets needs_attention, writes correct events
  - when session missing → no keys, no flag mutation, correct event
- kill behavior:
  - session exists → kill called
  - session missing → no-op
- resume behavior:
  - session exists → attach called (unless detached)
  - session missing → new session called with correct cwd + runner cmd
  - restart → kill then new session
- attach behavior:
  - session missing → `E_SESSION_NOT_FOUND`

tmux abstraction:
- introduce `TmuxClient` interface and test the lifecycle logic with a fake implementation.
- do not require tmux installed to run tests.

### manual test plan (required)
1) create a run (slice 1) and detach from tmux
2) `agency attach <id>` and confirm it attaches
3) `agency stop <id>` while runner is doing something; confirm keys are delivered (best-effort)
4) `agency kill <id>`; confirm `agency attach <id>` now fails with `E_SESSION_NOT_FOUND`
5) `agency resume <id>`; confirm session recreates and runner starts in the worktree
6) `agency resume <id> --restart`; confirm session restarts and warning prints

---

## guardrails / constraints
- do not introduce pid/process inspection (session existence is the only runtime signal in v1).
- do not run setup/verify/archive scripts from stop/kill/resume.
- do not modify PR/merge/archive behavior in this slice.
- do not delete worktrees or mutate git state in stop/kill/resume/attach.
- keep tmux layout single session, single window, single pane.

---

## rollout notes
none (local tool). s4 is safe to ship incrementally; behavior is strictly additive except `attach` returning a more specific error.