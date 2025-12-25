# agency — slice s0 spec: run lifecycle foundation

## goal

establish the minimal, correct machinery for creating, executing, and cleaning up isolated runs.

## scope (in)

s0 implements:

- creation of a run with a unique id
- creation of an isolated git worktree + branch for the run
- launching an external runner inside the worktree in a dedicated tmux session
- capturing runner stdout/stderr to log files
- minimal run state tracking: `queued -> running -> completed|failed|killed`
- stopping a run without affecting other runs
- explicit cleanup via `agency rm` (no implicit deletion)
- json output mode for all commands (`--json`)

## explicit non-goals (out)

s0 does not implement:

- rich dashboards / interactive tui
- run listing UX beyond minimal correctness needs (slice s1)
- diff/merge/approval workflows (slice s2)
- github integration
- environment bootstrapping (copy/symlink `.env`, setup scripts)
- directory inputs (inputs are files only)
- sandboxing/network isolation
- retries, restarts, or resumable runs

## user-visible commands (s0)

s0 introduces exactly these commands:

- `agency run`
- `agency stop <run_id>`
- `agency rm <run_id>`
- `agency attach <run_id>` (tmux attach convenience)

note: `agency ls/show/diff/merge` are not part of s0.

### common flags

all commands accept:

- `--json` (prints a single json object to stdout; no other output)
- `--config <path>` (optional; defaults to platform config location)
  - precedence: explicit `--config`, then `$agency_CONFIG`, then platform default path

## run invocation interface

runs are parameterized by a **run spec** plus a **prompt**. the prompt is natural language; the run spec is structured and machine-checkable.

the canonical interface is:

- `agency run --spec <run_spec.json>`

the cli may also accept flags as sugar; it must materialize an equivalent run spec internally and persist it in the run directory.

### run spec (v1 schema)

the run spec is a json object with the following fields:

required:

- `repo` (string, absolute path): path to a git repository
- `base_ref` (string): git ref to branch from (e.g. `main`, `HEAD`, sha)
- `runner.kind` (string enum): `claude_code` | `codex`
- `prompt.path` (string): path to a markdown prompt file (relative to repo or absolute)

optional:

- `new_branch` (string): branch name to create; default `agency/<run_id>`
- `inputs` (array): list of input file references
  - each item:
    - `path` (string): file path (relative to repo or absolute)
    - `mode` (string enum): `read` (only allowed value in v1)
- `runner.args` (array of strings): extra args passed to the runner
- `limits.max_minutes` (int): max wall time; default from config, may be unset (no limit) in v1
- `name` (string): human label

reserved (accepted but ignored in v1; stored for forward-compat):

- `commands`
- `artifacts_out`
- `patch_policy`
- `approval_policy`
- `context_pack`

### flag sugar for `agency run`

`s0` supports these optional flags in addition to `--spec`:

- `--base <ref>`: sets `base_ref`
- `--branch <name>`: sets `new_branch`
- `--runner <claude-code|codex>`: maps to `runner.kind`
- `--prompt-file <path>`: sets `prompt.path`
- `--prompt <text>`: writes `<run_dir>/prompt.md` and sets `prompt.path` to that file
- `--input <path>` (repeatable): appends to `inputs[]` with `{mode:"read"}`
- `--name <label>`: sets `name`

precedence rules (highest first):

1. `--spec` provides the base object
2. explicit cli flags override the corresponding fields in the spec
3. defaults are applied last

### input validation rules (v1)

validation runs on the fully materialized spec (after applying flag sugar and rewriting prompt paths for `--prompt`).

before creating a worktree, `agency run` must validate:

- `repo` exists and is a git repository
- `base_ref` resolves in the repo
- `prompt.path` exists and is a file
- every `inputs[].path` exists and is a file
- inputs are files only; directories are rejected
- all relative paths are resolved relative to repo root
- by default, `prompt.path` and all inputs must resolve **within the repo root**
  - if an absolute path is outside the repo, reject with an error in v1
  - exception: when `--prompt` is used, the materialized `prompt.path` is rewritten to `./.agency/prompt.md` inside the worktree, and validation is applied to that rewritten path

on success, record a fingerprint for each input (stored in metadata):

- path (repo-relative)
- size bytes
- sha256 hash of file contents

(note: this is provenance only; no file copying is performed in s0.)

### git branch + worktree creation rules

- `new_branch` defaults to `agency/<run_id>` if not provided.
- `new_branch` is created at `base_ref`'s commit; `base_ref` itself is never checked out directly for the run.
- worktree creation uses a new branch (`git worktree add -b <new_branch> <worktree_path> <base_ref>` or equivalent).
- if `new_branch` already exists, abort with `E_BRANCH_EXISTS` (no branch reuse in s0).
- runs must not share branches; each run gets a unique branch.

## run state machine (s0)

states:

- `queued`
- `running`
- `completed` (runner exited with code 0)
- `failed` (runner exited with non-zero code)
- `killed` (user requested stop; runner terminated)

transitions:

- `queued -> running` when tmux session and runner process start successfully
- `running -> completed|failed` when runner exits
- `running -> killed` when `agency stop` is executed successfully
- terminal: `completed|failed|killed` are terminal
- removal is orthogonal to state; `removed_at` (nullable timestamp) may be set once by `agency rm` and does not change `state`

a run is “done” when it is in a terminal state.

restarts are not supported in s0.

## filesystem + persistence

### worktree root

worktrees are centralized under:

- `~/.agency/worktrees/<repo_fingerprint>/<run_id>/`

`repo_fingerprint` is a stable identifier derived from repo absolute path (e.g. sha256(path) truncated), stored in metadata.
- note: this is path-based; moving the repo yields a new fingerprint and therefore a new worktree namespace

### run directory (local metadata)

each run has a directory:

- `~/.agency/runs/<run_id>/`

contents (minimum set for s0):

- `meta.json` (redundant snapshot of key fields; sqlite remains authoritative)
- `spec.json` (materialized final run spec used for execution)
- `prompt.md` (only if `--prompt` was used; otherwise not duplicated)
- `inputs.json` (resolved input list with fingerprints)
- `logs/runner.stdout.log`
- `logs/runner.stderr.log`
- `logs/runner.log` (combined stdout+stderr; raw ordering)
- `exit_code.txt` (written by the wrapper upon runner exit)
- `done.json` (optional completion marker written by the wrapper)
- `worktree_path.txt`
- `tmux_session.txt`

### sqlite (authoritative run state)

sqlite stores canonical run records. minimal table:

`runs`:
- `id` (pk)
- `repo_path` (abs)
- `repo_fingerprint`
- `base_ref`
- `new_branch`
- `worktree_path` (unique)
- `runner_kind`
- `runner_args_json`
- `state` (enum)
- `name` (nullable)
- `created_at`, `updated_at`
- `exit_code` (nullable)
- `stdout_log_path`, `stderr_log_path`
- `tmux_session_name`
- `error` (nullable string; last fatal error)
- `removed_at` (nullable timestamp)

invariants:

- `id` unique
- `worktree_path` unique
- a run never shares a worktree with another run
- state transitions must follow the state machine above
- removal is represented solely by `removed_at`; it does not alter `state`

sqlite is the source of truth for state; `meta.json` is a convenience snapshot.

## tmux contract

each run owns exactly one tmux session.

- session name: `agency:<run_id>`
- window/pane layout: single window, single pane (v1)
- working directory: the run’s worktree root (repo checkout inside the worktree)
- command: tmux launches a wrapper script, e.g. `~/.agency/bin/run_wrapper.sh <run_id>` (path configurable)
  - wrapper responsibilities:
    - launch the configured runner with cwd = worktree
    - stream stdout/stderr to `logs/runner.stdout.log` and `logs/runner.stderr.log` and tee to `logs/runner.log`
    - record exit code to `exit_code.txt`
    - optionally emit `done.json` with timing/status metadata
    - exit after the runner exits
    - leave the tmux session intact; the daemon watches exit markers to update state
- attach behavior:
  - outside tmux: `tmux attach -t agency:<run_id>`
  - inside tmux (`$TMUX` set): `tmux switch-client -t agency:<run_id>`
  - if the session does not exist, return `E_TMUX_SESSION_NOT_FOUND`

### logging

stdout and stderr from the runner must be streamed to files in the run directory:

- `logs/runner.stdout.log`
- `logs/runner.stderr.log`
- `logs/runner.log` (combined stream order; may contain mixed control sequences)

tmux is not the log store; it is only the interactive surface.

## runner adapter contract (s0)

agency does not implement coding agency. it launches external runners in tmux.

s0 supports two runner kinds:

- `claude_code`
- `codex`

each runner kind maps to a configured executable + default args (from config). s0 does not hardcode installation paths.

### runner environment (s0)

- runner is launched with cwd set to the worktree repo directory
- no special sandboxing or network isolation in s0
- no env copying/symlinking in s0

### completion detection

- the daemon transitions `running -> completed|failed` based on the wrapper’s exit markers (`exit_code.txt`, optionally `done.json`)
- tmux session presence/absence alone is not used as a completion signal

### passing the prompt

the runner is instructed to use the prompt file:

- if `prompt.path` is within the repo, it is available in the worktree at the same repo-relative path
- if `--prompt` was used, `prompt.md` is written into the run directory and must be copied into the worktree at a fixed path:
  - `./.agency/prompt.md` (created inside the worktree)
  - and `prompt.path` in the materialized spec must be rewritten to that path

### passing inputs (references)

inputs are **declarative** (for future council) and **validated** (for safety/provenance), but s0 does not enforce read-only access.

the runner prompt may reference these files by repo-relative path.

## cleanup contract

cleanup is explicit in s0.

- `agency stop <run_id>`:
  - only valid when state is `running`
  - terminates the tmux session
  - records state `killed`
  - does **not** delete the worktree directory (so the user can inspect partial changes)
- `agency rm <run_id>`:
  - valid only in terminal states (`completed|failed|killed`)
  - must:
    - remove the git worktree directory
    - delete the associated tmux session if it still exists
    - set `removed_at` (timestamp) in sqlite/meta; `state` remains whatever terminal state it already held
  - must never affect other runs

if rm fails to delete resources, it must report which resources remain and how to remove them manually.
there is no implicit deletion; only explicit `agency rm` may set `removed_at` and remove resources.

## error model (s0)

errors are categorized and must return non-zero exit code.

minimum error codes (string):

- `E_NOT_GIT_REPO`
- `E_BAD_REF`
- `E_INVALID_PATH`
- `E_INPUT_NOT_FILE`
- `E_TMUX_NOT_FOUND`
- `E_TMUX_START_FAILED`
- `E_WORKTREE_CREATE_FAILED`
- `E_RUNNER_NOT_CONFIGURED`
- `E_RUN_NOT_FOUND`
- `E_INVALID_STATE`
- `E_CLEANUP_FAILED`
- `E_BRANCH_EXISTS`
- `E_DB_LOCKED`
- `E_DB_ERROR`
- `E_PERMISSION_DENIED`
- `E_TMUX_SESSION_NOT_FOUND`
- `E_RUNNER_DISAPPEARED`

json error output:

```json
{
  "ok": false,
  "schema_version": 1,
  "error": {
    "code": "E_BAD_REF",
    "message": "base_ref 'foo' does not resolve",
    "details": { "base_ref": "foo" }
  }
}
```

non-json (human) output may be concise but must include error code.

## json output contract (s0)

when `--json` is provided, the command prints exactly one json object.

### `agency run --json`

```json
{
  "ok": true,
  "schema_version": 1,
  "data": {
    "id": "r_01H...",
    "repo": "/abs/path",
    "base_ref": "main",
    "new_branch": "agency/r_01H...",
    "worktree_path": "/home/user/.agency/worktrees/.../r_01H...",
    "tmux_session": "agency:r_01H...",
    "state": "running",
    "stdout_log": "/home/user/.agency/runs/.../logs/runner.stdout.log",
    "stderr_log": "/home/user/.agency/runs/.../logs/runner.stderr.log"
  }
}
```

### `agency stop --json`

returns updated run summary with new state.

### `agency rm --json`

returns `{ok:true, schema_version:1, data:{id, removed_at, ...}}`; `state` remains the terminal state it held pre-removal. example:

```json
{
  "ok": true,
  "schema_version": 1,
  "data": {
    "id": "r_01H...",
    "state": "completed",
    "removed": true,
    "removed_at": "2025-01-10T12:00:00Z"
  }
}
```

## acceptance criteria (s0)

s0 is complete when:

1. creating a run creates exactly one new worktree and one new tmux session
2. multiple runs can execute concurrently in the same repo without interference
3. killing one run does not affect any other run
4. run state transitions match the state machine and are recorded in sqlite
5. logs are written and discoverable via run directory paths
6. `agency rm` removes worktree + tmux session deterministically for terminal runs and records `removed_at`
7. all commands support `--json` and produce parseable single-object output
8. invalid inputs fail before any worktree is created
9. crash reconciliation on daemon start: `running` rows with missing tmux sessions are marked `failed` with `error=E_RUNNER_DISAPPEARED` and `exit_code=null`; orphan tmux sessions without sqlite rows are left untouched or reported as orphaned