# agency l3: slice 04 pr roadmap — lifecycle control (stop/kill/resume + flags)

goal: implement slice-04 as a sequence of small, reviewable PRs with tight scope, strong unit tests (no tmux required in CI), and minimal blast radius.

non-goals for this roadmap:
- no pid/process-tree inspection
- no transcript replay / runner context injection
- no setup/verify/archive execution from resume
- no git mutations (besides reading meta / writing meta/events)
- no new config schema features (runner argv arrays stay out of s4)

key decisions locked for s4 implementation:
- tmux session existence is the only runtime signal in v1 (`tmux has-session`)
- `stop` sends **C-c only**
- stop/kill missing session: **no-op, exit 0**, stderr message, **no event recorded**
- `attach` missing session: **E_SESSION_NOT_FOUND** with “try: agency resume <id>”
- `resume` acquires repo lock **only when it will create or restart a session** (double-check pattern)
- archived predicate for resume failure: `meta.archive.archived_at != null`
- `resume --restart` prompts for confirmation **only if session exists and stdin+stderr are ttys**, unless `--yes` provided
- tmux integration uses exec-style args with `tmux new-session -c <cwd> -- <cmd> <args...>` (no shell quoting)

---

## pr-04a: tmux client interface + concrete implementation + fakes

### goal
make tmux operations unit-testable: introduce a `TmuxClient` interface (in `internal/tmux`) and a concrete implementation that shells out to `tmux`, plus a fake for tests.

### scope
- `internal/tmux/`:
  - define `type Client interface` with:
    - `HasSession(name string) (bool, error)`
    - `NewSession(name, cwd string, argv []string) error`
    - `Attach(name string) error`
    - `KillSession(name string) error`
    - `SendKeys(name string, keys []Key) error`
  - define `type Key string` (or enum-ish constants) with at least `KeyCtrlC`
  - implement `client_exec.go` using `internal/exec.CommandRunner` (no direct os/exec usage)
  - keep existing tmux helpers (capture/ansi stripping) untouched unless they need to move behind the concrete impl

- `internal/tmux/`:
  - add a pure helper: `SessionName(runID string) string` => `agency_<run_id>`

### out-of-scope
- wiring commands to use the interface
- any behavior changes in CLI

### acceptance
- `go test ./...` passes without tmux installed
- table tests cover:
  - session name derivation
  - `HasSession` mapping of tmux exit codes (0 => true, 1 => false, else error)
- `NewSession` argv invariants:
    - includes `new-session`, `-d`, `-s <name>`, `-c <cwd>`
    - includes `--` separator
    - includes the command argv tail

### files
- add/modify:
  - `internal/tmux/client.go`
  - `internal/tmux/client_exec.go`
  - `internal/tmux/client_fake_test.go` (or `_test.go`)
  - `internal/tmux/session_name.go` (or similar)

---

## pr-04b: attach + stop + kill

### goal
make session-existence commands deterministic without creating sessions: attach errors cleanly; stop/kill are best-effort no-ops when missing.

### scope
- `internal/commands/attach.go` (or equivalent):
  - resolve run via `internal/ids` + `internal/store`
  - call `TmuxClient.HasSession(sessionName)`
  - if false: return `E_SESSION_NOT_FOUND` (stderr only), include `try: agency resume <id>`
  - else: `TmuxClient.Attach(sessionName)`

- `internal/errors/`:
  - add error code `E_SESSION_NOT_FOUND`

- `internal/events/`:
  - no attach failure events (attach stays purely interactive)

- `internal/commands/stop.go`:
  - if run missing => `E_RUN_NOT_FOUND`
  - check session exists:
    - if missing: print `no session for <id>` to stderr, exit 0, do not mutate flags, no event
    - if exists:
      - `TmuxClient.SendKeys(session, [KeyCtrlC])`
      - set `meta.flags.needs_attention=true`
      - append event `stop` with keys list
      - exit 0

- `internal/commands/kill.go`:
  - if run missing => `E_RUN_NOT_FOUND`
  - check session exists:
    - if missing: stderr message, exit 0, no event
    - if exists:
      - `TmuxClient.KillSession(session)`
      - append event `kill_session`
      - exit 0

### acceptance
given run exists, session missing:
- `agency attach <id>` exits non-zero with `E_SESSION_NOT_FOUND`
- stdout empty; stderr contains suggestion

tests:
- unit tests for attach command using fake tmux client:
  - session exists => attach invoked
  - session missing => correct error code
- unit tests for stop/kill commands using fake tmux:
  - stop existing session => SendKeys called, meta mutated, event written
  - stop missing session => no key send, no flag mutation, no event
  - kill existing => kill called, event written
  - kill missing => no event

### guardrails
- attach does not create sessions
- attach does not mutate meta/events
- stop/kill bypass repo lock but must still write meta/events atomically

---

## pr-04c: resume (create/attach/restart/detached) + worktree missing + locking

### goal
implement `agency resume <id> [--detached] [--restart] [--yes]` with correct locking and robust failure modes.

### scope
- CLI:
  - add command `resume` to `internal/cli` and `internal/commands/resume.go`
  - flags:
    - `--detached`
    - `--restart`
    - `--yes` (only relevant when `--restart` and session exists)

- behavior:
  - resolve run meta (run exists else `E_RUN_NOT_FOUND`)
  - validate worktree path exists:
    - if missing and `meta.archive.archived_at != null` => `E_WORKTREE_MISSING` with reason `archived`
    - if missing and not archived => `E_WORKTREE_MISSING` with reason `missing`
    - append event `resume_failed` with `data.reason`
  - session existence check (no lock):
    - if session exists and not restart:
      - append event `resume_attach` (data.detached)
      - attach unless detached
  - if restart requested:
    - if session exists:
      - if not `--yes`: prompt y/N **only if** stdin+stderr are ttys
      - if not a tty and `--yes` missing: exit non-zero with `E_CONFIRMATION_REQUIRED`
      - user decline exits 0 (canceled)
      - print loud warning about losing in-tool history
    - acquire repo lock
    - re-check session existence under lock
    - kill if exists
    - create new session with cwd=worktree and argv=[runner cmd]
    - append event `resume_restart` (data.detached)
    - attach unless detached
  - if session missing (create path):
    - acquire repo lock
    - re-check session existence under lock
    - create if still missing
    - append event `resume_create` (data.detached)
    - attach unless detached

- runner command resolution:
  - v1: string-only runner command (as `[cmd]`)
  - no config schema changes in s4

### acceptance (manual)
1) kill session then `agency resume <id>` recreates session and starts runner in worktree
2) `agency resume <id> --detached` does not attach
3) `agency resume <id> --restart` prompts only if a session existed (unless `--yes`), then recreates
4) non-tty stdin/stderr + `--restart` without `--yes` exits non-zero with `E_CONFIRMATION_REQUIRED`

### acceptance (automated)
unit tests with fake tmux + temp fs:
- session exists => attaches (or returns if detached)
- session missing => creates session with correct cwd and argv
- restart => kill then create
- missing worktree archived vs missing => correct reason in error and event

locking tests:
- ensure the code path acquires lock only when creating/restarting (can assert via fake lock object / hook)

### guardrails
- resume never runs scripts
- resume never touches git
- resume uses tmux `-c` (no shell)
- no pid inspection

### files
- add/modify:
  - `internal/commands/resume.go`
  - `internal/cli/dispatcher.go` (register flags)
  - `internal/lock/` usage in resume
  - `internal/events/` for new events
  - `internal/errors/` add `E_WORKTREE_MISSING`
    - note: `E_WORKTREE_MISSING` is introduced in s4 for resume only; do not back-propagate to older commands in this slice
  - `internal/errors/` add `E_CONFIRMATION_REQUIRED`

---

## cross-pr requirements

### event names (append-only)
- `resume_attach`
- `resume_create`
- `resume_restart`
- `resume_failed`
- `stop`
- `kill_session`

event data minimum:
- `session_name`
- `detached` (resume)
- `restart` (resume)
- `keys` for stop

### error codes added in s4
- `E_SESSION_NOT_FOUND`
- `E_WORKTREE_MISSING` (existing; resume uses it)
- `E_CONFIRMATION_REQUIRED`

### atomicity
- meta updates use atomic read-modify-write (preserve unknown fields)
- events append uses line-delimited `O_APPEND`; last-write-wins is acceptable but must not corrupt JSON

### demo script (end-to-end after pr-04c)
```bash
# create run via slice 1
agency run --title "s4 demo" --runner claude
agency ls

# attach/detach
agency attach <id>

# stop and observe
agency stop <id>
agency show <id>

# kill session and validate attach fails
agency kill <id>
agency attach <id>   # E_SESSION_NOT_FOUND

# resume and validate session recreated
agency resume <id>
```

tests that must pass for every pr:
- `go test ./...`
- no tmux required in CI (fake client for command tests)

guardrails for the whole slice:
- do not change push/merge/clean/archive behavior
- do not change status derivation beyond flags/events consistency
- keep one-session/one-pane assumption
- do not introduce new config schema features (argv arrays) inside s4
