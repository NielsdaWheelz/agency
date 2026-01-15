# s4 pr-04a report: tmux client interface + exec implementation + fakes

## summary

- introduced `tmux.Client` interface in `internal/tmux/client.go` defining the contract for testable tmux operations
- implemented `tmux.ExecClient` in `internal/tmux/client_exec.go` that shells out to tmux via `internal/exec.CommandRunner`
- added comprehensive table-driven tests in `internal/tmux/client_exec_test.go` using a fake `CommandRunner`
- all tests pass without tmux installed (CI-safe)
- existing `SessionName` helper retained from `capture.go` (already correct)

## problems encountered

1. **duplicate declarations**: initial test file duplicated `slicesEqual` and `TestSessionName` from `capture_test.go`
   - solution: removed duplicates from new test file; existing implementations work correctly

2. **hasession exit code semantics**: test initially used exit code 1 to test error formatting, but exit code 1 is the valid "session not found" response
   - solution: changed error formatting tests to use exit code 2+

3. **test case incomplete**: one test case for `NewSession` non-zero exit didn't include expected args
   - solution: added the expected args to the test case

## solutions implemented

- **client interface**: defined `tmux.Client` interface with 5 methods:
  - `HasSession(ctx, name) (bool, error)` - exit 0 = true, 1 = false, other = error
  - `NewSession(ctx, name, cwd, argv) error` - creates detached session with `--` separator
  - `Attach(ctx, name) error` - attaches to existing session
  - `KillSession(ctx, name) error` - kills session
  - `SendKeys(ctx, name, keys) error` - sends keys to session

- **key type**: defined `tmux.Key` string type with `KeyCtrlC` constant

- **exec client**: `ExecClient` uses `exec.CommandRunner` interface (already existing), enabling:
  - unit testing with fake runner (no tmux required)
  - consistent error handling across all methods
  - stderr capping at 4kb for error messages
  - deterministic error formatting: `tmux <subcmd> failed (exit=<n>): <stderr>`

- **testing strategy**: fake `exec.CommandRunner` records all calls and returns configurable responses

## decisions made

1. **kept existing tmux helpers**: `HasSession` (function), `CaptureScrollback`, `SessionTarget`, `SessionName`, and the `Executor` interface in `capture.go` remain untouched per spec guidance to minimize changes
   - the new `Client` interface is for slice-04 lifecycle commands
   - existing `Executor`-based code continues to work for scrollback capture

2. **context-first methods**: all `Client` methods accept `context.Context` as first parameter for cancellation support
   - no hidden timeouts in the tmux layer; callers control timeout

3. **error vs no-op semantics**: 
   - `HasSession`: exit 1 returns `(false, nil)` not an error (consistent with spec)
   - `NewSession/Attach/KillSession/SendKeys`: any non-zero exit returns error

4. **argv validation**: `NewSession` and `SendKeys` validate their slice arguments are non-empty before calling tmux

5. **no parallel exec paths**: as spec requested, all new tmux subprocess execution goes through `ExecClient`

## deviations from spec

1. **no separate `session_name.go` file**: spec suggested creating this file, but `SessionName` already exists in `capture.go` and works correctly; no need to duplicate or move it

2. **kept existing `Executor` interface**: spec mentioned consolidating tmux execution, but existing `Executor` interface is used by `CaptureScrollback` and works well; changing it would require updating existing tests and code unnecessarily

## how to run

### run tests

```bash
# run all tmux tests
go test ./internal/tmux/... -v

# run all project tests
go test ./...
```

### build

```bash
go build -o agency ./cmd/agency
```

no user-facing commands changed in this PR - only internal API additions.

## how to verify

```bash
# verify tests pass without tmux
go test ./internal/tmux/... -v

# verify no compilation errors
go build ./...

# verify all tests pass
go test ./...
```

## branch name

```
pr4/s4-04a-tmux-client-interface
```

## commit message

```
feat(tmux): add Client interface + ExecClient implementation for testable tmux ops

PR-04a for slice-04 (lifecycle control).

Add internal/tmux.Client interface defining the contract for tmux
operations needed by stop/kill/resume commands. The interface enables
unit testing without tmux installed by allowing a fake implementation
to be injected.

Interface methods (all context-first):
- HasSession(ctx, name) (bool, error)
- NewSession(ctx, name, cwd, argv) error
- Attach(ctx, name) error
- KillSession(ctx, name) error
- SendKeys(ctx, name, keys) error

Add ExecClient implementation that shells out to tmux via the existing
internal/exec.CommandRunner interface. Exit code handling follows tmux
semantics: has-session exit 0 = exists, exit 1 = not found, other = error.

Add Key type with KeyCtrlC constant for send-keys operations.

Add comprehensive table-driven tests using a fake CommandRunner that:
- Records all calls for assertion
- Returns configurable responses
- Does not require tmux installed (CI-safe)

Existing tmux helpers (SessionName, CaptureScrollback, Executor interface)
remain untouched to minimize blast radius.

Files added:
- internal/tmux/client.go (interface + Key type)
- internal/tmux/client_exec.go (ExecClient implementation)
- internal/tmux/client_exec_test.go (unit tests)

All tests pass: go test ./...
```
