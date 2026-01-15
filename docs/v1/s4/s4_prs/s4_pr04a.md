# agency l4: pr-04a spec — tmux client interface + exec implementation + fakes

goal: introduce a unit-testable tmux integration layer in `internal/tmux` by defining a `TmuxClient` interface, an exec-backed implementation that shells out to `tmux` via `internal/exec.CommandRunner`, and a pure helper for session naming. no CLI/command behavior changes in this PR.

---

## scope

in-scope:
- define `internal/tmux.Client` interface (ctx-first) for the minimal tmux ops needed by slice-04.
- implement `internal/tmux.ExecClient` (or similar) that runs tmux subprocesses via `internal/exec.CommandRunner`.
- add `internal/tmux.SessionName(runID string) string` helper returning `agency_<run_id>`.
- add unit tests that:
  - do not require tmux installed
  - validate `HasSession` exit-code mapping
  - validate argv construction for `NewSession`, `Attach`, `KillSession`, `SendKeys`
- ensure `go test ./...` passes without tmux installed.

out-of-scope:
- wiring any existing commands to this interface (no changes to `internal/commands/*`).
- adding new CLI commands/flags.
- changing status derivation, meta/events behavior, or any non-tmux packages.
- transcript capture / ANSI stripping changes (existing helpers remain untouched unless strictly necessary for compilation).
- keeping parallel tmux exec paths: any tmux subprocess execution must go through the exec-backed client (see below).

---

## public surface area

### new go API (internal-only)
- `internal/tmux.Client`
- `internal/tmux.Key` + constants
- `internal/tmux.SessionName(runID string) string`
- `internal/tmux.NewExecClient(runner exec.CommandRunner) Client` (constructor) (name may vary)

no user-facing command surface changes.

---

## design constraints

locked decisions (from slice-04 roadmap):
- tmux runtime signal in v1 is session existence via `tmux has-session`.
- use exec-style args for session creation:
  - `tmux new-session -d -s <name> -c <cwd> -- <cmd> <args...>`
  - no shell quoting
- all tmux ops accept `context.Context`; no hidden timeouts in this layer.
- `internal/exec.CommandRunner` already returns `CmdResult` with `ExitCode`, and returns `err != nil` only for execution failures.
- tmux command execution must be centralized: if any existing `internal/tmux` helper shells out to `tmux`, refactor it to call `ExecClient` (or rename/relocate it) so there is no parallel tmux exec path.

---

## implementation

### 1) `internal/tmux/client.go`
define the interface and key type.

```go
package tmux

import "context"

type Key string

const (
  KeyCtrlC Key = "C-c"
)

type Client interface {
  HasSession(ctx context.Context, name string) (bool, error)
  NewSession(ctx context.Context, name, cwd string, argv []string) error
  Attach(ctx context.Context, name string) error
  KillSession(ctx context.Context, name string) error
  SendKeys(ctx context.Context, name string, keys []Key) error
}

notes:
	•	no pane targeting in v1; name targets the session.
	•	argv must be exec-style: first element is command, remaining are args.

2) internal/tmux/session_name.go

pure helper:

package tmux

func SessionName(runID string) string {
  return "agency_" + runID
}

3) internal/tmux/client_exec.go

exec-backed implementation using internal/exec.CommandRunner.

required behavior:

HasSession
	•	run: tmux has-session -t <name>
	•	interpret exit codes:
	•	ExitCode == 0 => (true, nil)
	•	ExitCode == 1 => (false, nil)
	•	other => (false, error) including exit code and stderr (best-effort)
	•	if CommandRunner.Run(...) returns err != nil, return that error unchanged.

NewSession
	•	require len(argv) >= 1 else return error (plain error; higher layers map if needed)
	•	run:
	•	tmux new-session -d -s <name> -c <cwd> -- <argv...>
	•	if non-zero exit code, return error including stderr.

Attach
	•	run: tmux attach -t <name>
	•	if non-zero exit, return error including stderr.

KillSession
	•	run: tmux kill-session -t <name>
	•	if non-zero exit, return error including stderr.

SendKeys
	•	require len(keys) >= 1 else return error (programmer error)
	•	run: tmux send-keys -t <name> <key1> <key2> ...
	•	keys rendered as string(key)
	•	if non-zero exit, return error including stderr.

implementation detail:
	•	use internal/exec.CommandRunner.Run(ctx, "tmux", args, opts) where:
	•	opts sets Cwd to empty (tmux ops not dependent on cwd).
	•	no stdin (CommandRunner.Run does not set stdin).
	•	opts.Env is empty (no overrides).
	•	capture stdout/stderr as supported by CommandRunner.
	•	centralize common “non-zero exit -> error” formatting helper in this file.
	•	format errors deterministically: cap stderr (e.g. 4kb), trim trailing whitespace, include subcommand + exit code.
	•	example: fmt.Errorf("tmux %s failed (exit=%d): %s", subcmd, code, trimmedStderr)

4) naming

pick one of:
	•	type ExecClient struct { runner exec.CommandRunner }
	•	func NewExecClient(r exec.CommandRunner) Client
	•	file names as in roadmap: client.go, client_exec.go, session_name.go.

do not add extra packages.

⸻

tests

tests must be table-driven and must not require tmux installed.

test strategy
	•	write a fake exec.CommandRunner stub in _test.go that:
	•	records name, args, and any opts it receives
	•	returns a configurable CmdResult{ExitCode, Stdout, Stderr} with err == nil
	•	can optionally return err != nil to simulate exec failures
	•	instantiate the exec-backed client with the fake runner.

required unit tests

SessionName
	•	input: "20250109-a3f2" -> output: "agency_20250109-a3f2"

HasSession exit-code mapping
cases:
	•	exit 0 => exists true
	•	exit 1 => exists false
	•	exit 2 => error
	•	exit 127 => error
	•	err != nil => error

also assert argv exactly:
	•	["has-session", "-t", "<name>"]

NewSession argv construction
given:
	•	name "agency_123"
	•	cwd "/tmp/wt"
	•	argv []string{"claude"}
expect tmux args invariants:
	•	includes new-session, -d, -s <name>, -c <cwd>
	•	includes `--` separator
	•	includes command argv tail (claude)

and for argv []string{"claude", "--foo", "bar"} expect tail includes all args after --.

also test:
	•	argv empty => error

Attach argv
	•	["attach", "-t", "<name>"]

KillSession argv
	•	["kill-session", "-t", "<name>"]

SendKeys argv
	•	keys [KeyCtrlC] => ["send-keys", "-t", "<name>", "C-c"]
	•	keys empty => error

error formatting tests
	•	non-zero exit errors include subcommand name and exit code
	•	stderr is capped and trimmed (assert prefix/substring, not full content)

⸻

observable behavior

no user-facing behavior changes in this PR. only adds internal code and tests.

⸻

guardrails
	•	do not modify any command implementations in internal/commands/.
	•	do not modify schema/config behavior.
	•	do not modify status derivation or store/meta/events behavior.
	•	do not introduce any timeouts at the tmux layer; context is caller-controlled.
	•	keep existing tmux transcript/ansi helpers untouched unless required for compilation.
	•	if any existing tmux helpers execute tmux subprocesses, refactor them to use ExecClient (no parallel exec paths).

⸻

how to test (developer)

go test ./...

tmux does not need to be installed for tests.

⸻

files

add:
	•	internal/tmux/client.go
	•	internal/tmux/client_exec.go
	•	internal/tmux/session_name.go
	•	internal/tmux/client_exec_test.go (or similar)

modify:
	•	only as necessary to keep internal/tmux package consistent (e.g., adjust existing files to avoid name conflicts). avoid unrelated changes.

⸻
