# agency — slice s0 PR roadmap

**slice goal:** correct run lifecycle with isolated worktrees, tmux-supervised runners, durable state, and deterministic cleanup.

**invariants (apply to all PRs):**

* do not modify constitution or slices.md
* do not introduce s1/s2 functionality
* do not invent new run states
* do not add chat/council features
* all commands must support `--json`
* sqlite is the source of truth for run state

---

## PR-00 — repo bootstrap + core scaffolding (foundation)

**goal**
establish the rust workspace, shared core crate, and basic build/test plumbing.

**scope**

* create rust workspace:

  * `agency-cli`
  * `agencyd`
  * `agency-core`
* shared core types:

  * `RunId` (ULID, with optional `parse`)
  * `RunState` enum (explicit serde renames per variant or `rename_all = "snake_case"`)
  * `RunnerKind` enum (explicit serde renames: `claude_code`, `codex`)
  * `RunSummary` (no `schema_version` field; `#[serde(rename_all = "snake_case")]`)
  * `ErrorEnvelope` (error codes serialize as EXACT variant names; `#[serde(rename_all = "snake_case")]`)
* shared error codes enum (`ErrorCode`; no `rename_all`, exact variant names)
* config loading (toml + precedence: `--config` > `$agency_CONFIG` > platform default via `directories`)
* config schema with explicit struct fields and `#[serde(deny_unknown_fields)]`:

  * `runners.claude_code: Option<RunnerConfig>`
  * `runners.codex: Option<RunnerConfig>`
  * `RunnerConfig { exec, default_args }` with `#[serde(default)]` on `default_args`
* config loader: `load_config(explicit_path: Option<&Path>) -> Result<Config, ConfigError>`
* optional `$agency_SOCKET` override for daemon socket path
* shared `SCHEMA_VERSION: u32 = 1` constant for json envelopes
* data root constants:

  * data root: `~/.agency/`
  * db path: `~/.agency/agency.db`
  * runs root: `~/.agency/runs/<run_id>/`
  * locks root: `~/.agency/locks/`
  * (worktrees root defined in PR-03)
* logging setup (env-based level)

**explicit non-goals**

* no sqlite
* no tmux
* no git
* no daemon logic

**acceptance**

* `cargo build` succeeds
* `cargo test` runs (even if minimal)
* config precedence works; runner exec/default_args parse (no binary existence requirement)
* config with typo like `[runners.claude_cod]` fails at parse time due to `deny_unknown_fields`
* data root/db path constants exposed
* `RunId::new()` produces prefixed ULIDs
* `agency --help` and `agency --version` work (clap stub)

---

## PR-01 — daemon process + ipc + lifecycle bootstrap

**goal**
introduce the daemon (`agencyd`) and client ↔ daemon communication.

**scope**

* unix domain socket setup
* daemon start + graceful shutdown
* auto-spawn daemon from CLI if not running
* internal `Ping` RPC for health/auto-spawn; no user-facing `agency ping` subcommand
* pid/lock handling (best-effort, not perfect)
* daemon owns sqlite file at `~/.agency/agency.db`; CLI never opens db

**explicit non-goals**

* no run logic
* no tmux
* no sqlite writes beyond boot metadata

**acceptance**

* health check via internal RPC succeeds whether daemon already running or auto-started
* concurrent `agency` calls don’t start multiple daemons
* daemon logs startup + shutdown cleanly

**depends on**

* PR-00

---

## PR-02 — sqlite schema + run state machine

**goal**
make run state authoritative and durable.

**scope**

* sqlite initialization + migrations; WAL mode; `busy_timeout`; short transactions; daemon is the only writer (CLI writes via daemon RPC); db file at `~/.agency/agency.db`
* migrations via embedded SQL (e.g., `sqlx::migrate!` with bundled migrations)
* `runs` table exactly as per s0 spec
* run creation (`queued`)
* state transitions with validation
* `removed_at` handling (not a state)
* path helpers for data root and run dir: `~/.agency/runs/<run_id>/`
* reconciliation on daemon startup, and opportunistically on run/stop/rm RPCs (no periodic ticker in s0):

  * detect exited runners by consuming run_dir outputs (`exit_code.txt`, `runner_done.json`)
  * detect missing tmux sessions
  * update run state deterministically

**explicit non-goals**

* no worktrees
* no tmux
* no runner execution

**acceptance**

* can create/update/query runs via core APIs
* invalid state transitions are rejected
* reconciliation logic consumes wrapper outputs and updates state deterministically
* sqlite is in WAL mode with `busy_timeout`; only the daemon writes
* migrations are embedded and applied deterministically on startup

**depends on**

* PR-00
* PR-01

---

## PR-02.5 — json schema + snapshot tests

**goal**
lock `--json` output shapes early with strict, deterministic snapshots.

**scope**

* serde structs for all s0 CLI `--json` envelopes (`run`/`stop`/`rm`/`attach` as applicable)
* insta snapshot tests on normalized JSON:

  * redact timestamps/pids/run ids and absolute paths to placeholders (`<TIMESTAMP>`, `<RUN_ID>`, `<PATH>`)
  * no “allow extra fields”; adding fields is a deliberate snapshot update (or schema_version bump)
* fixtures live under `tests/fixtures/json`

**explicit non-goals**

* no tmux/git integration; stubs/mocks are fine
* no s1 commands (e.g., `list`), no ping command

**acceptance**

* snapshots are deterministic after normalization
* schema_version enforced where present; snapshot deltas are intentional
* CI runs these tests

**depends on**

* PR-00
* PR-01
* PR-02

---

## PR-03 — git adapter + worktree lifecycle

**goal**
create and destroy isolated git worktrees safely.

**scope**

* `Git` adapter over `Exec`
* repo validation
* repo root discovery via `git rev-parse --show-toplevel`; `repo_path` canonicalized to root; `repo_fingerprint` uses root
* `base_ref` resolution
* branch creation rules:

  * error if branch exists
  * branch created at base_ref
* repo-level lock: `~/.agency/locks/<repo_fingerprint>.lock` serializes branch creation and worktree add/rm
* worktree creation under:

  * `~/.agency/worktrees/<repo_fingerprint>/<run_id>/`
* worktree removal
* invariant enforcement: no reuse

**explicit non-goals**

* no runner execution
* no tmux
* no merge logic

**acceptance**

* worktree created with correct branch
* worktree removed deterministically
* repo_path resolves from subdirectories to root; fingerprint reflects root
* repo lock enforced for branch/worktree mutations
* errors surfaced as defined error codes

**depends on**

* PR-00
* PR-02

---

## PR-04 — tmux adapter + runner wrapper

**goal**
launch and supervise runners in tmux with reliable logging and exit detection.

**scope**

* `Tmux` adapter over `Exec`
* session naming: `agency:<run_id>`
* attach semantics:

  * inside tmux → `switch-client`
  * outside → `attach`
* runner wrapper script:

  * builds runner command from config (`runners.<id>.exec` + `default_args[]`); fails fast with `E_RUNNER_NOT_CONFIGURED` if missing
  * assumes it is launched inside tmux by the daemon; it does not create tmux sessions
  * tees stdout/stderr even when attached (no reliance on tmux capture)
  * writes under `<run_dir>/` (not the worktree):

    * `logs/runner.log`
    * `logs/runner.stdout.log`
    * `logs/runner.stderr.log`
    * `exit_code.txt`
    * required `runner_done.json` (schema_version 1: run_id, runner_kind, started_at, ended_at, exit_code; optional pid, signal, error); write atomically (temp + rename)
    * `wrapper.pid` (optional)

**explicit non-goals**

* no git
* no sqlite interactions
* no cleanup logic

**acceptance**

* tmux session starts per run
* wrapper writes logs/metadata to run_dir paths as specified
* exit code and runner_done.json captured reliably
* attach works in/out of tmux
* no sqlite writes performed here
* tmux session creation is owned by daemon via adapter; wrapper only runs inside tmux

**depends on**

* PR-00

(can be developed in parallel with PR-03)

---

## PR-05 — `agency run` command (integration)

**goal**
end-to-end run creation and execution.

**scope**

* run spec parsing + validation
* flag → spec materialization
* input validation + fingerprinting
* prompt handling:

  * `--prompt` writes to run dir and copies into worktree `.agency/prompt.md`
  * materialized `spec.json` rewrites prompt path to `.agency/prompt.md`
  * validation runs on the materialized spec
* validation ownership/flow:

  * CLI materializes spec/flags and RPCs `create_run(spec)` to daemon
  * daemon performs all validation (repo rules, config, runner availability)
  * two-phase validation/materialization:

    * pre-id validation on inputs not needing run_id
    * allocate run_id
    * materialize any run_id-dependent paths/values
    * post-id validation on materialized spec
* run dir lifecycle:

  * run dir path: `~/.agency/runs/<run_id>/`
  * daemon creates run dir after validation alongside sqlite row creation
* create sqlite run record (`queued`)
* create worktree
* start tmux runner
* transition to `running`
* json output contract

**explicit non-goals**

* stop/rm
* ls/show/diff
* merge
* persisting validation failures (no db row/run_dir/worktree on invalid input)

**acceptance**

* `agency run` creates:

  * sqlite record
  * worktree
  * tmux session
* invalid inputs fail before run_id allocation; no db row, no run_dir, no worktree
* `--json` output matches spec exactly
* prompt file exists in run dir and `.agency/prompt.md`; spec reflects `.agency/prompt.md`; validation runs on materialized spec
* run dir exists at `~/.agency/runs/<run_id>/` (daemon-created after validation)

**depends on**

* PR-01
* PR-02
* PR-03
* PR-04

---

## PR-06 — `agency stop` and `agency rm`

**goal**
controlled termination and deterministic cleanup.

**scope**

* `agency stop <run_id>`:

  * valid only in `running`
  * kill tmux session
  * mark state `killed`
* `agency rm <run_id>`:

  * requires terminal state on first removal; if `removed_at` already set, return ok/idempotent (no state check)
  * delete worktree
  * delete tmux session if present
  * set `removed_at` (if not already)
  * missing resources surface as warnings, not failures
* error handling + partial cleanup reporting

**explicit non-goals**

* merge
* diff
* garbage collection policies

**acceptance**

* stopping one run never affects others
* rm is idempotent: already-removed runs return ok with `removed: true`; missing resources reported as warnings
* rm cleans all resources or reports leftovers
* invalid state operations error correctly
* `--json` responses include `warnings: []` for partial cleanup details

**depends on**

* PR-05

---

## PR-07 — fake runner + integration tests

**goal**
prove correctness under real git + tmux.

**scope**

* fake runner binary/script
* temp git repo fixture
* gated integration tests:

  * run → stop → rm
  * concurrent runs with repo-level lock for worktree mutations + sqlite WAL/busy_timeout
  * crash + reconciliation:

    * case A: runner finishes while daemon is down; on restart, daemon consumes exit_code/runner_done.json and sets completed/failed
    * case B: tmux session killed manually while daemon is down; on restart, daemon marks failed with `E_RUNNER_DISAPPEARED`
* skip tests cleanly if tmux missing

**explicit non-goals**

* testing real claude-code/codex

**acceptance**

* integration tests pass when enabled
* tests are skipped (not failed) when tmux absent
* no flakes under parallel runs (repo lock + sqlite WAL/busy_timeout)

**depends on**

* PR-05
* PR-06

---

## ordering summary

```
PR-00
 ├─ PR-01
 ├─ PR-02
 ├─ PR-02.5
 ├─ PR-03
 ├─ PR-04
 └─ PR-05 → PR-06 → PR-07
```

parallelizable:

* PR-03 and PR-04
* PR-02.5 can start once PR-02 schema/types are set

---

## slice completion definition

slice s0 is complete when:

* PR-07 passes
* multiple runs execute concurrently
* no repo corruption is possible
* cleanup is boring and deterministic
* no s1/s2 features leaked
