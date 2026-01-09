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
- directory inputs (inputs are files only)
- sandboxing/network isolation
- retries, restarts, or resumable runs
- run types beyond code execution

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
  - precedence: explicit `--config`, then `$AGENCY_CONFIG`, then platform default path

## terminology

this spec distinguishes three artifact categories:

**git-tracked artifacts** (live in repo, versioned):
- **project state**: long-lived constraints, conventions, configuration
- **task specs**: documents describing what is being built (markdown files in repo)

**local artifacts** (not git-tracked):
- **run records**: sqlite database + run directory contents; operational state only

the word "spec" is reserved for task specs (git-tracked documents). run-time execution parameters are called **instructions** (free-form guidance) or **run records** (persisted state).

## run invocation interface

runs are parameterized by **instructions** (free-form text) and **task refs** (paths to git-tracked task spec documents).

### primary interface: CLI flags

the primary human interface is CLI flags:

```
agency run [flags]
```

required (at least one of):
- `--instructions <text>`: free-form text passed to the runner
- `--instructions-file <path>`: path to a file containing instructions
- `--task <path>` (repeatable): path to a git-tracked task spec document

if none of the above are provided, the runner starts an interactive session with no initial instructions.

additional flags:
- `--repo <path>`: path to git repository (default: current directory)
- `--base <ref>`: git ref to branch from (default: `HEAD`)
- `--branch <name>`: branch name to create (default: `agency/<run_id>`)
- `--runner <claude-code|codex>`: runner to use (default: from config)
- `--input <path>` (repeatable): input file reference
- `--name <label>`: human-readable label
- `--env-file <path>`: path to environment file (see environment handling)

### tool-facing interface: JSON

for programmatic use, `agency run` also accepts:

- `--run-config <path>`: path to a JSON file containing run parameters

the JSON format mirrors CLI semantics:

```json
{
  "repo": "/abs/path",
  "base_ref": "main",
  "new_branch": "agency/my-feature",
  "runner": {"kind": "claude_code", "args": []},
  "instructions": "implement the feature described in the task specs",
  "task_refs": ["docs/tasks/feature-x.md"],
  "inputs": [{"path": "src/config.ts", "mode": "read"}],
  "name": "feature-x-run",
  "env_file": "/path/outside/repo/.env"
}
```

precedence (highest first):
1. explicit CLI flags
2. `--run-config` JSON file
3. defaults

the CLI always materializes final parameters into a **run record** stored in the run directory.

### run record (v1 schema)

the run record is persisted as `run_record.json` in the run directory. it captures the fully resolved parameters used for execution:

required fields:
- `id` (string): unique run identifier
- `repo` (string, absolute path): path to the git repository
- `base_ref` (string): git ref branched from
- `runner.kind` (string enum): `claude_code` | `codex`

optional fields:
- `new_branch` (string): branch name; default `agency/<run_id>`
- `instructions` (string): free-form text passed to the runner
- `task_refs` (array of strings): paths to task spec documents (repo-relative)
- `inputs` (array): list of input file references
  - each item: `{path, mode, fingerprint}`
  - `mode`: `read` (only allowed value in v1)
- `runner.args` (array of strings): extra args passed to the runner
- `limits.max_minutes` (int): max wall time; default from config, may be unset
- `name` (string): human label
- `env_file` (string): path to injected environment file (if used)

reserved (accepted but ignored in v1; stored for forward-compat):
- `commands`
- `artifacts_out`
- `patch_policy`
- `approval_policy`
- `context_pack`

### input validation rules (v1)

validation runs on fully resolved parameters before any worktree is created.

`agency run` must validate:

- `repo` exists and is a git repository
- `base_ref` resolves in the repo
- every `task_refs[]` path exists, is a file, and resolves **within the repo root**
- every `inputs[].path` exists and is a file
- inputs are files only; directories are rejected
- all relative paths are resolved relative to repo root
- task refs must be within the repo; absolute paths outside repo are rejected

on success, record a fingerprint for each input and task ref:
- path (repo-relative)
- size bytes
- sha256 hash of file contents

### git branch + worktree creation rules

- `new_branch` defaults to `agency/<run_id>` if not provided
- `new_branch` is created at `base_ref`'s commit; `base_ref` itself is never checked out directly
- worktree creation uses a new branch (`git worktree add -b <new_branch> <worktree_path> <base_ref>`)
- if `new_branch` already exists, abort with `E_BRANCH_EXISTS`
- runs must not share branches; each run gets a unique branch
- branch name uniqueness is enforced per repository

## environment handling

s0 supports **explicit, opt-in** environment file injection.

### mechanism

flag: `--env-file <path>`

- `<path>` must be an absolute path **outside** the repository
- agency copies the file into the worktree at `.agency/.env`
- the file is added to `.gitignore` in the worktree (not committed)

### constraints

- injection is **never automatic**; requires explicit `--env-file` flag
- the source file must exist and be readable
- if `--env-file` is provided and the path is inside the repo, reject with `E_ENV_FILE_IN_REPO`
- the injected file is **never committed**; it lives only in the worktree

### trust and reproducibility implications

**trust**: the env file may contain secrets. agency does not validate contents. users are responsible for ensuring the file does not leak secrets into commits.

**reproducibility**: runs using `--env-file` are not fully reproducible from git history alone. the run record captures `env_file` (the source path) but not contents. this is intentional: secrets should not be persisted.

**recommendation**: for reproducible runs, avoid `--env-file` and configure runner environment via other means (e.g., shell environment, runner config).

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
- removal is orthogonal to state; `removed_at` may be set by `agency rm`

a run is "done" when it is in a terminal state.

restarts are not supported in s0.

### runner session semantics

runner exit marks **execution completion**, not **session termination**.

- tmux sessions may persist after terminal state
- users can attach to inspect final state, review output, or interact further
- session cleanup happens only via explicit `agency rm`

this means:
- a run in `completed` state may still have an active tmux session
- `agency attach` works on terminal-state runs if the session exists

## filesystem + persistence

### worktree root

worktrees are centralized under:

- `~/.agency/worktrees/<repo_fingerprint>/<run_id>/`

`repo_fingerprint` is a stable identifier derived from repo absolute path (e.g. sha256(path) truncated).

note: this is path-based; moving the repo yields a new fingerprint.

### run directory (local artifacts)

each run has a directory:

- `~/.agency/runs/<run_id>/`

contents (minimum set for s0):

- `run_record.json` (fully resolved parameters used for execution)
- `meta.json` (redundant snapshot of key fields; sqlite remains authoritative)
- `instructions.md` (only if `--instructions` was used with inline text)
- `inputs.json` (resolved input list with fingerprints)
- `logs/runner.stdout.log`
- `logs/runner.stderr.log`
- `logs/runner.log` (combined stdout+stderr; raw ordering)
- `exit_code.txt` (written by wrapper upon runner exit)
- `done.json` (optional completion marker)
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
- `instructions` (nullable text)
- `task_refs_json` (nullable; JSON array of paths)
- `state` (enum)
- `name` (nullable)
- `created_at`, `updated_at`
- `exit_code` (nullable)
- `stdout_log_path`, `stderr_log_path`
- `tmux_session_name`
- `error` (nullable string; last fatal error)
- `removed_at` (nullable timestamp)
- `env_file_path` (nullable; source path if `--env-file` used)

invariants:

- `id` unique
- `worktree_path` unique
- a run never shares a worktree with another run
- state transitions must follow the state machine
- removal is represented by `removed_at`; it does not alter `state`

sqlite is the source of truth for state; `meta.json` is a convenience snapshot.

## tmux contract

each run owns exactly one tmux session.

- session name: `agency:<run_id>`
- window/pane layout: single window, single pane (v1)
- working directory: the run's worktree root
- command: tmux launches a wrapper script
  - wrapper responsibilities:
    - launch the configured runner with cwd = worktree
    - pass instructions and task refs to the runner
    - stream stdout/stderr to log files
    - record exit code to `exit_code.txt`
    - optionally emit `done.json`
    - exit after runner exits
    - leave tmux session intact for user inspection
- attach behavior:
  - outside tmux: `tmux attach -t agency:<run_id>`
  - inside tmux: `tmux switch-client -t agency:<run_id>`
  - if session does not exist, return `E_TMUX_SESSION_NOT_FOUND`

### logging

stdout and stderr from the runner are streamed to:

- `logs/runner.stdout.log`
- `logs/runner.stderr.log`
- `logs/runner.log` (combined)

tmux is the interactive surface, not the log store.

## runner adapter contract (s0)

agency launches external runners in tmux. s0 supports:

- `claude_code`
- `codex`

each runner kind maps to a configured executable + default args. s0 does not hardcode installation paths.

### runner invocation

the runner receives:

1. **instructions**: passed via stdin, file, or runner-specific flag (runner-dependent)
2. **task refs**: paths to task spec documents (available in worktree at same repo-relative paths)
3. **working directory**: worktree root

v1 assumes all runs are code execution (`run_type = code`). run type dispatch is not implemented.

### runner environment (s0)

- runner is launched with cwd = worktree
- no sandboxing or network isolation
- if `--env-file` was used, `.agency/.env` exists in worktree

### completion detection

- daemon transitions `running -> completed|failed` based on wrapper exit markers
- tmux session presence alone is not a completion signal
- precedence: `done.json` > `exit_code.txt`
- conflicting signals treated as `failed` with `E_RUNNER_DISAPPEARED`

### passing instructions and task refs

**instructions**:
- if `--instructions <text>` was used, text is written to `instructions.md` in run directory and copied to `.agency/instructions.md` in worktree
- if `--instructions-file <path>` was used, file is copied to `.agency/instructions.md` in worktree
- runner is invoked with reference to this file (runner-specific mechanism)

**task refs**:
- task refs point to files inside the repo
- these files are available in the worktree at their original repo-relative paths
- runner prompt/instructions should reference them by path

### passing inputs (references)

inputs are **declarative** (for future use) and **validated** (for provenance), but s0 does not enforce read-only access.

the runner may reference input files by repo-relative path.

## cleanup contract

cleanup is explicit in s0.

- `agency stop <run_id>`:
  - only valid when state is `running`
  - if tmux session is gone, transition to `failed` with `E_RUNNER_DISAPPEARED`
  - terminates the tmux session
  - records state `killed`
  - does **not** delete worktree (user can inspect partial changes)

- `agency rm <run_id>`:
  - valid only in terminal states
  - removes git worktree directory
  - deletes tmux session if still exists
  - sets `removed_at` in sqlite; `state` unchanged
  - must never affect other runs

if rm fails, report which resources remain and how to remove manually.

## error model (s0)

errors are categorized and return non-zero exit code.

error codes:

- `E_NOT_GIT_REPO`
- `E_BAD_REF`
- `E_INVALID_PATH`
- `E_INPUT_NOT_FILE`
- `E_TASK_REF_NOT_FOUND`
- `E_TASK_REF_OUTSIDE_REPO`
- `E_ENV_FILE_IN_REPO`
- `E_ENV_FILE_NOT_FOUND`
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

non-json output must include error code.

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
6. `agency rm` removes worktree + tmux session deterministically for terminal runs
7. all commands support `--json` and produce parseable single-object output
8. invalid inputs fail before any worktree is created
9. crash reconciliation: `running` rows with missing tmux sessions marked `failed`
10. `--env-file` correctly injects environment file into worktree without committing
11. task refs are validated as existing files within the repo
12. runs can start with no instructions (interactive session)
