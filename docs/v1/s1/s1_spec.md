# agency slice 01: run workspace + setup + tmux runner (v1 mvp)

## goal (1–2 lines)

implement `agency run` to create an isolated git worktree + branch, execute the repo’s **setup** script, and launch a **real** `claude`/`codex` tui inside a detached tmux session. implement `agency attach` to attach to that session.

---

## scope

### in-scope

- `agency run`:
  - requires a clean parent working tree (repo root checkout)
  - creates a new branch off a local parent branch
  - creates a git worktree under the agency global data dir
  - creates `<worktree>/.agency/{out,tmp}/` and `<worktree>/.agency/report.md`
  - runs `scripts.setup` **outside tmux** with injected env + timeout
  - on setup success: creates tmux session `agency:<run_id>` running the runner command in the worktree (detached by default)
  - persists run metadata to `${AGENCY_DATA_DIR}/repos/<repo_id>/runs/<run_id>/meta.json`
  - writes logs for setup under `${AGENCY_DATA_DIR}/repos/<repo_id>/runs/<run_id>/logs/setup.log`

- `agency attach <id>`:
  - attaches to tmux session `agency:<run_id>` (no creation; no resume behavior in this slice)

- no `agency ls` in this slice (defer to slice 2)

### out-of-scope

- any github operations: no `push`, no PR create/update, no merge
- `resume`, `stop`, `kill`, `clean`, `merge`, `show`, `mark`
- verify/archive scripts
- runner pid inspection (tmux session existence is the only runtime signal)
- transcript capture (tmux scrollback remains available via tmux itself)
- modifying `.gitignore` or `.git/info/exclude` (init owns that)

---

## public surface area added/changed

### commands

- `agency run [--title <string>] [--runner <claude|codex>] [--parent <branch>] [--attach]`
- `agency attach <run_id>`

### flags

- `agency run`
  - `--title`: optional; default `untitled-<shortid>`
  - `--runner`: optional; default from `agency.json defaults.runner`
  - `--parent`: optional; default from `agency.json defaults.parent_branch`
  - `--attach`: optional; if set, attach immediately after tmux session is created

---

## files created/modified

### required pre-existing (repo root)

- `agency.json` (validated; required)

### created/updated (global; under `${AGENCY_DATA_DIR}`)

- `${AGENCY_DATA_DIR}/repos/<repo_id>/repo.json` (create/update “last seen” + origin info if present)
- `${AGENCY_DATA_DIR}/repos/<repo_id>/runs/<run_id>/meta.json` (create; may be updated during run)
- `${AGENCY_DATA_DIR}/repos/<repo_id>/runs/<run_id>/logs/setup.log` (create; overwrite on re-run)
- `${AGENCY_DATA_DIR}/repos/<repo_id>/worktrees/<run_id>/` (git worktree directory)

### created/updated (workspace-local; under the worktree)

- `<worktree>/.agency/`
- `<worktree>/.agency/out/`
- `<worktree>/.agency/tmp/`
- `<worktree>/.agency/report.md` (created on run, unless already exists)

---

## new error codes (add to l0 error code list if missing)

- `E_PARENT_BRANCH_NOT_FOUND` — parent branch ref not found locally
- `E_WORKTREE_CREATE_FAILED` — `git worktree add` / branch creation failed
- `E_TMUX_SESSION_EXISTS` — tmux session name collision for this run_id (should be extremely rare)
- `E_TMUX_FAILED` — tmux session creation failed (non-zero exit)
- `E_TMUX_SESSION_MISSING` — attach requested but tmux session does not exist for a known run

(existing error codes used in this slice)
- `E_NO_REPO`
- `E_NO_AGENCY_JSON`
- `E_INVALID_AGENCY_JSON`
- `E_RUNNER_NOT_CONFIGURED`
- `E_TMUX_NOT_INSTALLED`
- `E_PARENT_DIRTY`
- `E_SCRIPT_TIMEOUT`
- `E_SCRIPT_FAILED`

---

## behaviors (given / when / then)

### 1) successful run (detached default)

**given**
- cwd is inside a git repo
- repo root checkout is clean: `git status --porcelain` is empty
- `agency.json` exists and validates
- local parent branch exists: `refs/heads/<parent_branch>`
  - checked via `git show-ref --verify refs/heads/<parent_branch>`
- setup script exits 0 within 10m
- tmux installed

**when**
- user runs: `agency run --title "test run" --runner claude`

**then**
- a new run_id is generated and printed
- a new branch and worktree are created in one step:
  - `git worktree add -b <branch> <worktree_path> <parent_branch>`
- `<worktree>/.agency/{out,tmp}/` exist
- `<worktree>/.agency/report.md` exists (templated; title prefilled)
- setup script executes outside tmux with injected env; stdout/stderr captured to `logs/setup.log`
- tmux session `agency:<run_id>` is created detached, with pane command:
  - runner command is a single string from `agency.json`
  - runner command is executed via `sh -lc` and treated as a shell command string (users may include wrappers/args)
  - agency does not escape or parse the runner command; it is passed as-is to the shell
  - example: `sh -lc 'cd "<worktree>" && exec <runner_cmd>'`
- `meta.json` exists and includes required fields (see below)
- command exits 0
- command prints next steps and run details:
  - `run_id`
  - `worktree_path`
  - `tmux_session_name`
  - `agency attach <run_id>`

### 2) successful run with immediate attach

**when**
- user runs: `agency run --attach`

**then**
- all outcomes of (1) occur
- after tmux session creation, agency attaches to `agency:<run_id>` (blocking until user detaches/exits tmux client)

### 3) parent working tree dirty

**given**
- `git status --porcelain` non-empty in repo root checkout

**when**
- `agency run ...`

**then**
- exit non-zero with `E_PARENT_DIRTY`
- do not create branch, worktree, tmux session, or run metadata

### 4) missing local parent branch

**given**
- local `refs/heads/<parent_branch>` does not exist

**when**
- `agency run --parent <parent_branch>`

**then**
- exit non-zero with `E_PARENT_BRANCH_NOT_FOUND`
- do not create worktree
- print actionable fix: checkout/fetch parent locally

### 5) setup script fails

**given**
- worktree created successfully
- setup script exits non-zero or times out

**when**
- `agency run ...`

**then**
- exit non-zero with `E_SCRIPT_FAILED` or `E_SCRIPT_TIMEOUT`
- write `meta.json` with `flags.setup_failed=true`
- retain worktree and branch for inspection
- do **not** create tmux session
- setup log exists at `logs/setup.log`

### 6) `.agency/` not ignored

**given**
- `.agency/` does not appear to be ignored (best-effort check; see guardrails)

**when**
- `agency run ...`

**then**
- proceed normally
- print a warning recommending `agency init` to add `.agency/` to `.gitignore`

### 7) attach to existing run

**given**
- tmux session `agency:<run_id>` exists

**when**
- `agency attach <run_id>`

**then**
- attach to session
- exit code is 0 when the tmux client detaches/exits

### 8) attach to missing session

**given**
- run exists but tmux session does not

**when**
- `agency attach <run_id>`

**then**
- exit non-zero with `E_RUN_NOT_FOUND` (if run_id unknown) or `E_TMUX_SESSION_MISSING` (if run exists but no session)
- message instructs user that resume is not in this slice (deferred) and provides:
  - `worktree_path` (from `meta.json`)
  - suggested command: `cd <worktree_path> && <runner_cmd>`


---

## persistence (what new state is written where)

### `meta.json` required fields written by `agency run`

- `schema_version` (string, `1.0`)
- `run_id`
- `repo_id`
- `title`
- `runner`
- `parent_branch`
- `branch`
- `worktree_path`
- `created_at` (rfc3339)
- `tmux_session_name` (set only if tmux started successfully)

optional fields updated in this slice:
- `flags.setup_failed` (boolean)
- `flags.needs_attention` (not set in this slice)
- `last_seen_at` (optional; if implemented, must be rfc3339)
- `archive.archived_at` (optional; rfc3339 if set)

timestamp note:
- `created_at` is written in UTC (e.g., `time.Now().UTC().Format(time.RFC3339)`)

versioning note:
- `agency.json` uses integer `version` (e.g., `1`)
- `meta.json` uses string `schema_version` (e.g., `"1.0"`)

### atomic write requirement

- all JSON writes (`repo_index.json`, `repo.json`, `meta.json`) must be atomic:
  - write to temp file in same dir
  - fsync best-effort
  - rename

---

## tests

### manual smoke (required)

1) create a tiny repo:
```bash
mkdir /tmp/agency_s1 && cd /tmp/agency_s1
git init
echo hi > README.md
git add -A && git commit -m "init"

	2.	add agency.json with setup script:

	•	scripts/agency_setup.sh = #!/bin/sh; exit 0

	3.	run:

agency run --title "s1 smoke" --runner claude
tmux ls | grep agency:
agency attach <run_id>   # verify you land in the runner tui

	4.	dirty parent:

echo x >> README.md
agency run
# must fail E_PARENT_DIRTY

automated (minimal)
	•	unit tests (table-driven):
	•	slugify(title) → slug
	•	branch name format: agency/<slug>-<shortid>
	•	origin parsing → repo_key
	•	agency.json validation (good/bad fixtures)
	•	integration test (optional; may be behind build tag integration):
	•	temp repo + no-op setup script
	•	run agency run and assert:
	•	worktree dir exists
	•	meta.json exists and required fields set
	•	tmux has-session -t agency:<run_id> succeeds

⸻

guardrails (what not to touch)
	•	do not add any github/PR behavior (push, merge, gh pr create)
	•	do not implement resume/stop/kill/clean
	•	do not run setup script inside tmux
	•	do not modify the parent repo checkout (beyond reading status/root)
	•	do not add new persistent formats beyond those listed
	•	do not create additional global daemons; tmux is the only substrate
	•	`.agency/` ignore check (best-effort):
	•	run `git -C <worktree> check-ignore -q .agency/` after the worktree exists
	•	exit 0 = ignored, 1 = not ignored, 128 = error (treat as unknown, do not warn)

⸻

rollout notes
	•	expect first-run failures due to PATH differences inside tmux.
	•	mitigation: runner command is resolved from agency.json runners.<name>; users can provide wrapper commands there.
	•	expect users who skipped agency init to accidentally commit .agency/.
	•	mitigation: warning on run when .agency/ appears unignored; keep init explicit.
