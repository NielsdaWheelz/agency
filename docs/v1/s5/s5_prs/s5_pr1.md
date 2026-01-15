# agency s5 pr 5.1 — verify runner core (process + record + precedence)

## goal

implement the core verify execution engine and canonical evidence writing:

- run `scripts.verify` as a normal subprocess (no tmux)
- capture output to `verify.log` (truncate per invocation)
- optionally read `<worktree>/.agency/out/verify.json`
- derive `ok` + `summary` deterministically
- write `${run_dir}/verify_record.json` atomically

**no cli command yet.** this pr is plumbing + pure unit tests.

---

## non-goals (explicit)

- do not add `agency verify` command (that is pr 5.3)
- do not update `meta.json`
- do not append `events.jsonl`
- do not acquire repo locks
- do not change `agency push` / `merge` / tmux behavior
- do not introduce new error codes

---

## repo conventions to preserve

- scripts are executed via **`sh -lc <script>`** (match `internal/runservice/service.go executeSetupScript`)
- logs live under `<run_dir>/logs/` and are **overwritten** (truncate) per invocation
- logs include a short header (timestamp + command + cwd), matching setup log style
- atomic json writes go through `internal/fs/atomic.go`:
  - `fs.WriteJSONAtomic(path, v, 0o644)`
  - `fs.WriteFileAtomic(fsys, path, data, 0o644)` when raw bytes are needed
- path building goes through `internal/store/store.go` helpers where appropriate
- internal/verify depends on `internal/store` schema types intentionally (same pattern as other pipeline code)

---

## public surface area

none.

this pr introduces new internal packages/types only.

---

## files to create / modify

### new

- `internal/verify/runner.go`
- `internal/verify/verifyjson.go`
- `internal/store/verify_record.go`
- `internal/verify/verifyjson_test.go`
- `internal/verify/derive_test.go`

### modify (allowed)

- `internal/store/store.go` (add path helpers only; no refactors)

### explicitly do not touch

- `cmd/agency/main.go`
- `internal/cli/dispatch.go`
- `internal/commands/*`
- `internal/events/*`
- `internal/store/run_meta.go`
- `internal/lock/*`
- `internal/runservice/service.go` (use as reference only)

---

## implementation: required interfaces

### 1) verify record schema (canonical evidence)

create `internal/store/verify_record.go`:

- define:

```go
package store

type VerifyRecord struct {
  SchemaVersion  string `json:"schema_version"`
  RepoID         string `json:"repo_id"`
  RunID          string `json:"run_id"`
  ScriptPath     string `json:"script_path"`

  StartedAt      string `json:"started_at,omitempty"`   // RFC3339Nano UTC
  FinishedAt     string `json:"finished_at,omitempty"`  // RFC3339Nano UTC
  DurationMS     int64  `json:"duration_ms"`
  TimeoutMS      int64  `json:"timeout_ms"`

  TimedOut       bool   `json:"timed_out"`
  Cancelled      bool   `json:"cancelled"`

  ExitCode       *int   `json:"exit_code"`              // null if unknown or signaled
  Signal         *string `json:"signal"`                // e.g. "SIGKILL"
  Error          *string `json:"error"`                 // internal errors only (exec/log/io/json)
  OK             bool   `json:"ok"`

  VerifyJSONPath *string `json:"verify_json_path"`
  LogPath        string  `json:"log_path"`

  Summary        string  `json:"summary"`
}

notes:
	•	timestamps are strings (RFC3339Nano UTC).
	•	exit_code is null if:
	•	process failed to start
	•	process was terminated by signal we sent / detected
	•	signal is set when we terminate the process group (timeout/cancel) and should generally be "SIGKILL".
	•	timed_out and cancelled are mutually exclusive; never both true.

2) verify.json parsing (optional input)

create internal/verify/verifyjson.go:
	•	define:

package verify

type VerifyJSON struct {
  SchemaVersion string          `json:"schema_version"`
  OK            bool            `json:"ok"`
  Summary       string          `json:"summary,omitempty"`
  Data          json.RawMessage `json:"data,omitempty"`
}

“valid enough” rules:
	•	valid iff:
	•	schema_version exists and is non-empty
	•	ok is a boolean (normal json unmarshal)
	•	tolerate missing summary and missing data.
	•	if file exists but is invalid json or fails validation:
	•	treat as absent for ok derivation
	•	but still return verifyJSONPath as exists to record in VerifyRecord
	•	and return a parse/validation error string to be recorded in VerifyRecord.Error only if there is no other internal error (do not overwrite exec/log errors)

provide helpers:
	•	ReadVerifyJSON(path string) (vj *VerifyJSON, exists bool, err error)
	•	exists=true only if file exists
	•	vj=nil on invalid json / invalid shape
	•	err carries parse/validation info when exists=true

3) ok + summary derivation (pure)

in internal/verify/derive.go (or inside verifyjson.go if you prefer, but keep pure + testable), implement:
	•	DeriveOK(timedOut, cancelled bool, exitCode *int, vj *VerifyJSON) bool

locked precedence (v1):
	1.	if timedOut or cancelled => false
	2.	else if exitCode == nil => false
	3.	else if *exitCode != 0 => false
	4.	else if vj != nil => vj.OK
	5.	else => true
	note: verify.json may downgrade a successful exit to failure; it never upgrades a failing exit.

	•	DeriveSummary(timedOut, cancelled bool, exitCode *int, vj *VerifyJSON) string

summary rules:
	•	if vj != nil and vj.Summary != "" => use it
	•	else if timedOut => "verify timed out"
	•	else if cancelled => "verify cancelled"
	•	else if exitCode == nil => "verify failed (no exit code)"
	•	else if *exitCode == 0 => "verify succeeded"
	•	else => fmt.Sprintf("verify failed (exit %d)", *exitCode)

⸻

implementation: verify runner (subprocess + record writing)

create internal/verify/runner.go with:

type RunConfig struct {
  RepoID         string
  RunID          string
  WorkDir        string          // worktree root
  Script         string          // exact string executed (from agency.json)
  Env            []string        // full env; caller provides merged env (verify does not modify it)
  Timeout        time.Duration

  LogPath        string          // absolute: <run_dir>/logs/verify.log
  VerifyJSONPath string          // absolute: <workdir>/.agency/out/verify.json
  RecordPath     string          // absolute: <run_dir>/verify_record.json
}

func Run(ctx context.Context, cfg RunConfig) (store.VerifyRecord, error)

hard execution requirements:
	•	execute via shell: sh -lc <cfg.Script>
	•	cmd.Dir = cfg.WorkDir
	•	cmd.Env = cfg.Env
	•	cmd.Stdin = /dev/null
	•	start process in its own process group:
	•	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	•	open cfg.LogPath with truncate (O_TRUNC|O_CREATE|O_WRONLY, 0644)
	•	set cmd.Stdout and cmd.Stderr to the same log file (no tee)
	•	write a header at the top of verify.log (timestamp + command + cwd), matching setup.log style

timeout + cancellation:
	•	effective timeout is cfg.Timeout (caller supplies default 30m later)
	•	implement:
	•	ctx cancellation => cancelled path
	•	timeout fires => timed_out path
	•	on timeout/cancel:
	•	send SIGINT to process group (syscall.Kill(-pgid, syscall.SIGINT))
	•	wait 3s
	•	send SIGKILL to process group (syscall.Kill(-pgid, syscall.SIGKILL))
	•	record:
	•	TimedOut=true OR Cancelled=true
	•	Signal="SIGKILL"
	•	ExitCode=nil

exit code capture:
	•	on normal exit:
	•	if exit status available, set ExitCode to that integer
	•	if terminated by external signal (rare): set Signal and ExitCode=nil

record writing (always attempt):
	•	compute timestamps using time.Now().UTC().Format(time.RFC3339Nano)
	•	parse verify.json:
	•	call ReadVerifyJSON(cfg.VerifyJSONPath); record VerifyJSONPath if exists
	•	derive OK and Summary using pure helpers above
	•	write store.VerifyRecord to cfg.RecordPath atomically using internal/fs.WriteJSONAtomic(path, v, 0o644)
	•	ensure parent dirs exist (os.MkdirAll)
	•	return the record and nil error if execution started
	•	verify failure is represented in VerifyRecord.OK/ExitCode, not as a returned error
	•	return error only for internal failures that prevent running or writing:
	•	log open failure
	•	exec start failure
	•	json write failure
	•	(verify.json invalid is not a returned error; it is recorded into VerifyRecord.Error)

important: this pr does not update meta.json; callers will do so in pr 5.2.

⸻

path helper additions (minimal)

in internal/store/store.go add only:
	•	func VerifyRecordPath(repoID, runID string) string
returns ${AGENCY_DATA_DIR}/repos/<repo_id>/runs/<run_id>/verify_record.json

do not refactor existing show/render logic; just add helpers for the new files.

⸻

behaviors: acceptance (pr 5.1)

1) precedence (pure)

given the precedence table, unit tests must prove:
	•	timeout/cancel always => ok=false
	•	exit_code nil => ok=false
	•	exit_code != 0 => ok=false
	•	exit_code == 0 and verify.json valid => ok == verify.json.ok
	•	exit_code == 0 and verify.json invalid/absent => ok=true

2) verify.json validity (pure)
	•	missing file => exists=false
	•	invalid json => exists=true, vj=nil
	•	missing schema_version or empty => exists=true, vj=nil
	•	valid minimal => exists=true, vj != nil

3) summary derivation (pure)
	•	verify.json.summary wins when provided
	•	otherwise generic messages match rules above

⸻

tests

run:

go test ./...

required unit tests (no subprocess):
	•	TestDeriveOK_Precedence (table-driven)
	•	TestReadVerifyJSON_Validation (missing/invalid/valid)
	•	TestDeriveSummary (table-driven)

explicitly deferred to pr 5.2 integration tests:
	•	actually launching a script
	•	timeout/cancel killing the process group
	•	verify.log truncation behavior
	•	end-to-end verify_record writing

note:
	•	Run is not unit-tested in 5.1 by design; it is covered by integration tests in pr 5.2.

⸻

guardrails (hard)
	•	do not add or modify any CLI commands
	•	do not write or mutate meta.json
	•	do not append to events.jsonl
	•	do not add locking
	•	do not touch tmux code paths
	•	do not change script execution semantics elsewhere (setup stays as-is)

⸻

demo (developer sanity check; optional, not tested here)

if you want a manual smoke test locally (not required in this pr):
	•	write a tiny script in a temp dir and call internal/verify.Run from a scratch go test or temporary main
	•	confirm verify_record.json appears and verify.log truncates

do not commit any scratch code.
