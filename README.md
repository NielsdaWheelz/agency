# agency

local-first runner manager: creates isolated git workspaces, launches `claude`/`codex` TUIs in tmux, opens GitHub PRs via `gh`.

## status

**v1 in development** — slice 0 (bootstrap) complete, slice 1 complete, slice 2 complete, slice 3 (push + PR) complete, slice 4 (lifecycle control) complete, slice 5 (verify recording) complete, slice 6 (merge + archive) in progress.

slice 0 progress:
- [x] PR-00: project skeleton + shared contracts
- [x] PR-01: directory resolution + repo discovery + origin parsing
- [x] PR-02: agency.json schema + validation
- [x] PR-03: persistence schemas + repo store
- [x] PR-04: `agency init` command
- [x] PR-05: `agency doctor` command
- [x] PR-06: docs + cleanup

slice 1 progress:
- [x] PR-01: core utilities + errors + subprocess + atomic json
- [x] PR-02: repo detection + safety gates + repo.json update
- [x] PR-03: agency.json load + runner resolution for S1
- [x] PR-04: run pipeline orchestration (internal API)
- [x] PR-05: worktree + scaffolding + collision handling
- [x] PR-06: meta.json writer + run dir creation
- [x] PR-07: setup script execution + logging
- [x] PR-08: tmux session creation + attach command
- [x] PR-09: wire agency run end-to-end + --attach UX

slice 2 progress:
- [x] PR-00: repo lock helper
- [x] PR-01: run discovery + parsing + broken run records
- [x] PR-02: run id resolution (exact + unique prefix)
- [x] PR-03: derived status computation (pure)
- [x] PR-04: `agency ls` command
- [x] PR-05: `agency show` command
- [x] PR-06: transcript capture + events.jsonl

slice 3 progress:
- [x] PR-01: core plumbing for push (error codes, meta fields)
- [x] PR-02: preflight + git fetch/ahead/push + report gating
- [x] PR-03: gh PR idempotency + create + body sync + metadata persistence
- [x] PR-04: polish + docs sync

slice 4 progress:
- [x] PR-04a: tmux client interface + exec implementation + fakes
- [x] PR-04b: attach + stop + kill
- [x] PR-04c: resume (create/attach/restart/detached) + worktree missing + locking

slice 5 progress:
- [x] PR-01: verify runner core (process + record + precedence)
- [x] PR-02: meta + flags + events integration (+ new error code)
- [x] PR-03: CLI command `agency verify` + UX output

slice 6 progress:
- [x] PR-01: archive pipeline + `agency clean` command
- [x] PR-02: verify runner + merge prechecks (no gh merge yet)

next: slice 6 PR-03 (gh merge + full merge flow + idempotency)

## installation

### from source (development)

```bash
go install github.com/NielsdaWheelz/agency@latest
```

### from releases

prebuilt binaries available on [GitHub releases](https://github.com/NielsdaWheelz/agency/releases) for:
- darwin-amd64
- darwin-arm64
- linux-amd64

### homebrew (coming soon)

```bash
brew install NielsdaWheelz/tap/agency
```

## prerequisites

agency requires:
- `git`
- `gh` (authenticated via `gh auth login`)
- `tmux`
- configured runner (`claude` or `codex` on PATH)

## quick start

```bash
cd myrepo
agency init       # create agency.json + stub scripts
agency doctor     # verify prerequisites
agency run --title "implement feature X"
agency attach <id>
agency push <id>
agency merge <id>
```

## commands

```
agency init [--no-gitignore] [--force]
                                  create agency.json template + stub scripts
agency run [--title] [--runner] [--parent]
                                  create workspace, setup, start tmux
agency ls                         list runs + statuses
agency show <id> [--path]         show run details
agency attach <id>                attach to tmux session
agency resume <id> [--detached] [--restart]
                                  attach to tmux session (create if missing)
agency stop <id>                  send C-c to runner (best-effort)
agency kill <id>                  kill tmux session
agency push <id> [--force]        push + create/update PR
agency verify <id> [--timeout]    run verify script and record results
agency merge <id> [--squash|--merge|--rebase] [--force]
                                  verify, confirm, merge PR, archive
agency clean <id>                 archive without merging (abandon run)
agency doctor                     check prerequisites + show paths
```

### `agency init`

creates `agency.json` template and stub scripts in the current git repo.

**flags:**
- `--no-gitignore`: do not modify `.gitignore` (by default, `.agency/` is appended)
- `--force`: overwrite existing `agency.json` (scripts are never overwritten)

**files created:**
- `agency.json` — configuration file with defaults
- `scripts/agency_setup.sh` — stub setup script (exits 0)
- `scripts/agency_verify.sh` — stub verify script (exits 1, must be replaced)
- `scripts/agency_archive.sh` — stub archive script (exits 0)
- `.gitignore` entry for `.agency/` (unless `--no-gitignore`)

**output:**
```
repo_root: /path/to/repo
agency_json: created
scripts_created: scripts/agency_setup.sh, scripts/agency_verify.sh, scripts/agency_archive.sh
gitignore: updated
```

### `agency doctor`

verifies all prerequisites are met for running agency commands.

**checks:**
- repo root discovery via `git rev-parse --show-toplevel`
- `agency.json` exists and is valid
- required tools installed: `git`, `tmux`, `gh`
- `gh` is authenticated (`gh auth status`)
- runner command exists (e.g., `claude` or `codex` on PATH)
- scripts exist and are executable

**on success:**
- writes/updates `${AGENCY_DATA_DIR}/repo_index.json`
- writes/updates `${AGENCY_DATA_DIR}/repos/<repo_id>/repo.json`

**output (stable key: value format):**
```
repo_root: /path/to/repo
agency_data_dir: ~/Library/Application Support/agency
agency_config_dir: ~/Library/Preferences/agency
agency_cache_dir: ~/Library/Caches/agency
repo_key: github:owner/repo
repo_id: abcd1234ef567890
origin_present: true
origin_url: git@github.com:owner/repo.git
origin_host: github.com
github_flow_available: true
git_version: git version 2.40.0
tmux_version: tmux 3.3a
gh_version: gh version 2.40.0 (2024-01-15)
gh_authenticated: true
defaults_parent_branch: main
defaults_runner: claude
runner_cmd: claude
script_setup: /path/to/repo/scripts/agency_setup.sh
script_verify: /path/to/repo/scripts/agency_verify.sh
script_archive: /path/to/repo/scripts/agency_archive.sh
status: ok
```

**error codes:**
- `E_NO_REPO` — not inside a git repository
- `E_NO_AGENCY_JSON` — agency.json not found
- `E_INVALID_AGENCY_JSON` — agency.json validation failed
- `E_GIT_NOT_INSTALLED` — git not found
- `E_TMUX_NOT_INSTALLED` — tmux not found
- `E_GH_NOT_INSTALLED` — gh CLI not found
- `E_GH_NOT_AUTHENTICATED` — gh not authenticated
- `E_RUNNER_NOT_CONFIGURED` — runner command not found
- `E_SCRIPT_NOT_FOUND` — required script not found
- `E_SCRIPT_NOT_EXECUTABLE` — script is not executable (suggests `chmod +x`)
- `E_PERSIST_FAILED` — failed to write persistence files

### `agency run`

creates an isolated workspace and launches the runner in a tmux session.

**usage:**
```bash
agency run [--title <string>] [--runner <name>] [--parent <branch>] [--attach]
```

**flags:**
- `--title`: run title (default: `untitled-<shortid>`)
- `--runner`: runner name: `claude` or `codex` (default: agency.json `defaults.runner`)
- `--parent`: parent branch to branch from (default: agency.json `defaults.parent_branch`)
- `--attach`: attach to tmux session immediately after creation

**behavior:**
1. validates parent working tree is clean (`git status --porcelain`)
2. creates git worktree + branch under `${AGENCY_DATA_DIR}/repos/<repo_id>/worktrees/<run_id>/`
3. creates `.agency/`, `.agency/out/`, `.agency/tmp/` directories
4. creates `.agency/report.md` with template (title prefilled)
5. runs `scripts.setup` with injected environment variables (timeout: 10 minutes)
6. creates tmux session `agency_<run_id>` running the runner command
7. writes `meta.json` with run metadata

**success output:**
```
run_id: 20260110120000-a3f2
title: implement feature X
runner: claude
parent: main
branch: agency/implement-feature-x-a3f2
worktree: ~/Library/Application Support/agency/repos/abc123/worktrees/20260110120000-a3f2
tmux: agency_20260110120000-a3f2
next: agency attach 20260110120000-a3f2
```

**error codes:**
- `E_NO_REPO` — not inside a git repository
- `E_NO_AGENCY_JSON` — agency.json not found
- `E_INVALID_AGENCY_JSON` — agency.json validation failed
- `E_PARENT_DIRTY` — parent working tree has uncommitted changes
- `E_EMPTY_REPO` — repository has no commits
- `E_PARENT_BRANCH_NOT_FOUND` — specified parent branch does not exist locally
- `E_WORKTREE_CREATE_FAILED` — git worktree add failed
- `E_SCRIPT_FAILED` — setup script exited non-zero
- `E_SCRIPT_TIMEOUT` — setup script timed out (>10 minutes)
- `E_TMUX_FAILED` — tmux session creation failed
- `E_TMUX_ATTACH_FAILED` — tmux attach failed (with `--attach`)

**on failure:**

if the run fails after worktree creation, the error output includes:
- `run_id`
- `worktree` path (for inspection)
- `setup_log` path (if setup failed)

the worktree and metadata are retained for debugging; use `agency clean <id>` to remove.

### `agency ls`

lists runs and their statuses.

**usage:**
```bash
agency ls [--all] [--all-repos] [--json]
```

**flags:**
- `--all`: include archived runs (worktree deleted)
- `--all-repos`: list runs across all repos (ignores current repo scope)
- `--json`: output as JSON (stable format)

**default behavior:**
- if **inside a git repo**: lists runs for that repo only, excluding archived
- if **outside any git repo**: lists runs across all repos, excluding archived

**human output columns:**
- `RUN_ID`: full run identifier
- `TITLE`: run title (truncated to 50 chars; `<broken>` for corrupt meta; `<untitled>` for empty)
- `RUNNER`: runner name (empty for broken runs)
- `CREATED`: relative timestamp (e.g., "2 hours ago")
- `STATUS`: derived status (e.g., "active", "idle", "ready for review", "merged (archived)")
- `PR`: PR number if exists (e.g., "#123")

**status values:**
- `active` / `active (pr)`: tmux session exists
- `idle` / `idle (pr)`: no tmux session, worktree present
- `ready for review`: PR exists, pushed, report non-empty
- `needs attention`: verify failed, PR not mergeable, or stop requested
- `failed`: setup script failed
- `merged`: PR merged
- `abandoned`: explicitly abandoned
- `broken`: meta.json is unreadable/invalid
- `(archived)` suffix: worktree no longer exists

**json output:**
```json
{
  "schema_version": "1.0",
  "data": [
    {
      "run_id": "20260110120000-a3f2",
      "repo_id": "abc123",
      "repo_key": "github:owner/repo",
      "origin_url": "git@github.com:owner/repo.git",
      "title": "implement feature X",
      "runner": "claude",
      "created_at": "2026-01-10T12:00:00Z",
      "last_push_at": "2026-01-10T14:00:00Z",
      "tmux_active": true,
      "worktree_present": true,
      "archived": false,
      "pr_number": 123,
      "pr_url": "https://github.com/owner/repo/pull/123",
      "derived_status": "ready for review",
      "broken": false
    }
  ]
}
```

**sorting:**
- newest `created_at` first
- broken runs (null `created_at`) sort last
- tie-breaker: `run_id` ascending

**examples:**
```bash
agency ls                    # list current repo runs
agency ls --all              # include archived runs
agency ls --all-repos        # list all repos
agency ls --all-repos --all  # everything
agency ls --json             # machine-readable output
agency ls --json | jq '.data[].run_id'
```

### `agency show`

shows detailed information about a single run.

**usage:**
```bash
agency show <run_id> [--json] [--path] [--capture]
```

**arguments:**
- `run_id`: the run identifier (exact) or unique prefix

**flags:**
- `--json`: output as JSON (stable format)
- `--path`: output only resolved filesystem paths
- `--capture`: capture tmux scrollback to transcript files (mutating mode)

**behavior:**
- resolves run_id globally (works from anywhere, not just inside a repo)
- accepts exact run_id or unique prefix for convenience
- displays rich metadata, derived status, and paths

**id resolution:**
- exact match wins if found
- if no exact match, checks for unique prefix match
- multiple matches: fails with `E_RUN_ID_AMBIGUOUS` and lists candidates
- no matches: fails with `E_RUN_NOT_FOUND`

**human output:**
```
run: 20260110120000-a3f2
title: implement feature X
repo: abc123
runner: claude
parent: main
branch: agency/implement-feature-x-a3f2
worktree: ~/Library/Application Support/agency/repos/abc123/worktrees/20260110120000-a3f2

tmux: agency_20260110120000-a3f2
pr: https://github.com/owner/repo/pull/123 (#123)
last_push_at: 2026-01-10T14:00:00Z
last_report_sync_at: 2026-01-10T14:00:00Z
report_hash: abc123def456...
status: ready for review
```

note: there is a blank line between `worktree:` and `tmux:`.

when PR is missing: `pr: none (#-)`
when timestamps are missing: `last_push_at: none`

**json output:**
```json
{
  "schema_version": "1.0",
  "data": {
    "meta": { /* raw meta.json */ },
    "repo_id": "abc123",
    "repo_key": "github:owner/repo",
    "origin_url": "git@github.com:owner/repo.git",
    "archived": false,
    "derived": {
      "derived_status": "active",
      "tmux_active": true,
      "worktree_present": true,
      "report": { "exists": true, "bytes": 256, "path": "..." },
      "logs": { "setup_log_path": "...", "verify_log_path": "...", "archive_log_path": "..." }
    },
    "paths": {
      "repo_root": "/path/to/repo",
      "worktree_root": "/path/to/worktree",
      "run_dir": "/path/to/run",
      "events_path": "/path/to/events.jsonl",
      "transcript_path": "/path/to/transcript.txt"
    },
    "broken": false
  }
}
```

**path output:**
```
repo_root: /path/to/repo
worktree_root: /path/to/worktree
run_dir: /path/to/run
logs_dir: /path/to/run/logs
events_path: /path/to/run/events.jsonl
transcript_path: /path/to/run/transcript.txt
report_path: /path/to/worktree/.agency/report.md
```

**error codes:**
- `E_RUN_NOT_FOUND` — run not found
- `E_RUN_ID_AMBIGUOUS` — prefix matches multiple runs (lists candidates)
- `E_RUN_BROKEN` — run exists but meta.json is unreadable/invalid

**broken run handling:**
- `ls` shows broken runs with `<broken>` title and `broken` status
- `show` targeting a broken run fails with `E_RUN_BROKEN`
- `--json` still outputs envelope with `broken=true` and `meta=null`
- `--path` outputs best-effort paths and exits non-zero

**`--capture` behavior:**
- takes repo lock (mutating mode)
- emits `cmd_start` and `cmd_end` events to `events.jsonl`
- captures full tmux scrollback from the session's primary pane
- strips ANSI escape codes from captured text
- rotates `transcript.txt` to `transcript.prev.txt` (single backup)
- writes new `transcript.txt` atomically
- if session is missing: warns and continues without transcript
- capture failures never block `show` output

**transcript files:**
- `${AGENCY_DATA_DIR}/repos/<repo_id>/runs/<run_id>/transcript.txt`
- `${AGENCY_DATA_DIR}/repos/<repo_id>/runs/<run_id>/transcript.prev.txt`

**events file:**
- `${AGENCY_DATA_DIR}/repos/<repo_id>/runs/<run_id>/events.jsonl`
- append-only JSONL format
- each line contains: `schema_version`, `timestamp`, `repo_id`, `run_id`, `event`, `data`

**examples:**
```bash
agency show 20260110120000-a3f2           # show run details
agency show 20260110                       # unique prefix resolution
agency show 20260110120000-a3f2 --json    # machine-readable output
agency show 20260110120000-a3f2 --path    # print paths only
agency show 20260110120000-a3f2 --capture # capture transcript + show
agency show 20260110120000-a3f2 --json | jq '.data.derived.derived_status'
```

### `agency attach`

attaches to an existing tmux session for a run.

**usage:**
```bash
agency attach <run_id>
```

**arguments:**
- `run_id`: the run identifier (e.g., `20260110120000-a3f2`)

**behavior:**
- resolves repo root from current directory
- loads run metadata from `${AGENCY_DATA_DIR}/repos/<repo_id>/runs/<run_id>/meta.json`
- verifies tmux session exists
- attaches to the tmux session (blocks until user detaches)

**error codes:**
- `E_NO_REPO` — not inside a git repository
- `E_RUN_NOT_FOUND` — run not found (meta.json does not exist)
- `E_SESSION_NOT_FOUND` — tmux session does not exist (killed or system restarted)
- `E_TMUX_NOT_INSTALLED` — tmux not found

**when session is missing:**

if the run exists but the tmux session has been killed (e.g., system restarted), attach will fail with `E_SESSION_NOT_FOUND` and suggest using `agency resume <id>` instead:

```
E_SESSION_NOT_FOUND: tmux session 'agency_20260110120000-a3f2' does not exist

try: agency resume 20260110120000-a3f2
```

### `agency stop`

sends C-c to the runner in the tmux session (best-effort interrupt).

**usage:**
```bash
agency stop <run_id>
```

**arguments:**
- `run_id`: the run identifier (e.g., `20260110120000-a3f2`)

**behavior:**
- if session exists: sends C-c to the primary pane, sets `needs_attention` flag, appends `stop` event
- if session missing: prints `no session for <id>` to stderr and exits 0 (no-op)

**notes:**
- best-effort only; does not guarantee the runner stops
- session remains alive; use `agency resume --restart` to guarantee a fresh runner
- does not mutate meta or events if session is missing

**error codes:**
- `E_RUN_NOT_FOUND` — run not found
- `E_TMUX_NOT_INSTALLED` — tmux not found
- `E_TMUX_FAILED` — tmux send-keys failed
- `E_PERSIST_FAILED` — failed to write event

### `agency kill`

kills the tmux session for a run. Workspace remains intact.

**usage:**
```bash
agency kill <run_id>
```

**arguments:**
- `run_id`: the run identifier (e.g., `20260110120000-a3f2`)

**behavior:**
- if session exists: kills the tmux session, appends `kill_session` event
- if session missing: prints `no session for <id>` to stderr and exits 0 (no-op)

**notes:**
- does not delete the worktree (use `agency clean <id>` for that)
- does not set any flags on the run
- does not append events if session is missing

**error codes:**
- `E_RUN_NOT_FOUND` — run not found
- `E_TMUX_NOT_INSTALLED` — tmux not found
- `E_TMUX_FAILED` — tmux kill-session failed
- `E_PERSIST_FAILED` — failed to write event

### `agency resume`

attaches to the tmux session for a run. If session is missing, creates one and starts the runner.

**usage:**
```bash
agency resume <run_id> [--detached] [--restart] [--yes]
```

**arguments:**
- `run_id`: the run identifier (e.g., `20260110120000-a3f2`)

**flags:**
- `--detached`: do not attach; return after ensuring session exists
- `--restart`: kill existing session (if any) and recreate
- `--yes`: skip confirmation prompt for `--restart`

**behavior:**
- if session exists (no `--restart`): attaches to session (unless `--detached`)
- if session missing: creates new tmux session with cwd in worktree, starts runner, then attaches (unless `--detached`)
- if `--restart`: prompts for confirmation (unless `--yes` or non-interactive), kills session if exists, creates new session

**locking:**
- resume acquires repo lock **only** when creating or restarting a session
- uses double-check pattern: check session existence, acquire lock, re-check under lock

**notes:**
- resume **never** runs scripts (setup/verify/archive)
- resume **never** touches git (worktree state preserved)
- `--restart` will lose in-tool history (chat context, etc.) but git state is unchanged
- archived runs cannot be resumed (`E_WORKTREE_MISSING`)

**output (detached mode):**
```
ok: session agency_20260110120000-a3f2 ready
```

**confirmation prompt (restart with existing session):**
```
restart session? in-tool history will be lost (git state unchanged) [y/N]:
```

**events:**
- `resume_attach`: session existed, attached
- `resume_create`: session missing, created new session
- `resume_restart`: `--restart` used, killed and recreated session
- `resume_failed`: worktree missing (archived or corrupted)

**error codes:**
- `E_RUN_NOT_FOUND` — run not found
- `E_WORKTREE_MISSING` — run worktree path is missing on disk
- `E_CONFIRMATION_REQUIRED` — `--restart` attempted in non-interactive mode without `--yes`
- `E_REPO_LOCKED` — another agency process holds the lock
- `E_TMUX_NOT_INSTALLED` — tmux not found
- `E_TMUX_FAILED` — tmux operation failed
- `E_RUNNER_NOT_CONFIGURED` — runner command not found

**examples:**
```bash
agency resume 20260110120000-a3f2               # attach (create if needed)
agency resume 20260110120000-a3f2 --detached    # ensure session exists
agency resume 20260110120000-a3f2 --restart     # force fresh runner (prompts)
agency resume 20260110120000-a3f2 --restart --yes  # non-interactive restart
```

### `agency push`

pushes the run branch to origin and creates/updates a GitHub PR.

**usage:**
```bash
agency push <run_id> [--force]
```

**arguments:**
- `run_id`: the run identifier (exact or unique prefix)

**flags:**
- `--force`: proceed even if `.agency/report.md` is missing or effectively empty (< 20 chars)

**preflight checks (in order):**
1. resolve run_id and load metadata
2. verify worktree exists on disk
3. acquire repo lock (mutating command)
4. verify `origin` remote exists
5. verify origin host is exactly `github.com`
6. report gating (missing/empty report requires `--force`)
7. warn if worktree has uncommitted changes
8. verify `gh auth status` succeeds

**git operations (after preflight passes):**
1. `git fetch origin` (non-destructive)
2. resolve parent ref (local branch preferred, else `origin/<parent_branch>`)
3. compute commits ahead via `git rev-list --count <parent_ref>..<branch>`
4. refuse if ahead == 0 (`--force` does NOT bypass this)
5. `git push -u origin <branch>` (no force push)

**pr operations (after git push succeeds):**
1. look up existing PR:
   - first by stored `pr_number` in meta.json
   - fallback to `gh pr view --head <branch>`
2. if PR exists but not OPEN (CLOSED or MERGED): fail with `E_PR_NOT_OPEN`
3. if no PR exists: create via `gh pr create`
   - title: `[agency] <run_title>` (or branch name if untitled)
   - body: contents of `.agency/report.md` (or placeholder with `--force`)
4. sync report to PR body:
   - compute sha256 hash of report
   - if hash unchanged from `last_report_hash`: skip sync
   - else: update PR body via `gh pr edit --body-file`

**success output:**
```
pr: https://github.com/owner/repo/pull/123
```

**metadata persistence:**
- updates `meta.json` with:
  - `last_push_at` timestamp
  - `pr_number` and `pr_url`
  - `last_report_sync_at` and `last_report_hash` (when report synced)
- appends events to `events.jsonl`:
  - `push_started`, `git_fetch_finished`, `git_push_finished`
  - `pr_created` (if created)
  - `pr_body_synced` (if body updated)
  - `push_finished` (on success)
  - `push_failed` (on failure)

**error codes:**
- `E_RUN_NOT_FOUND` — run not found
- `E_RUN_ID_AMBIGUOUS` — prefix matches multiple runs
- `E_WORKTREE_MISSING` — run worktree path is missing on disk
- `E_REPO_LOCKED` — another agency process holds the lock
- `E_NO_ORIGIN` — no origin remote configured
- `E_UNSUPPORTED_ORIGIN_HOST` — origin is not github.com
- `E_REPORT_INVALID` — report missing/empty without `--force`
- `E_GH_NOT_INSTALLED` — gh CLI not found
- `E_GH_NOT_AUTHENTICATED` — gh not authenticated
- `E_PARENT_NOT_FOUND` — parent branch not found locally or on origin
- `E_EMPTY_DIFF` — no commits ahead of parent branch
- `E_GIT_PUSH_FAILED` — git push failed
- `E_GH_PR_CREATE_FAILED` — gh pr create failed
- `E_GH_PR_EDIT_FAILED` — gh pr edit failed
- `E_GH_PR_VIEW_FAILED` — gh pr view failed after create (retries exhausted)
- `E_PR_NOT_OPEN` — PR exists but is not OPEN (CLOSED or MERGED)

**notes:**
- all git/gh subprocesses run with non-interactive environment:
  - `GIT_TERMINAL_PROMPT=0`
  - `GH_PROMPT_DISABLED=1`
  - `CI=1`
- PR creation uses `--body-file` to preserve markdown formatting
- PR title is NOT updated after creation (v1)
- `--force` does NOT bypass `E_EMPTY_DIFF` (must have commits)

**examples:**
```bash
agency push 20260110120000-a3f2           # push branch + create/update PR
agency push 20260110120000-a3f2 --force   # push with empty report (placeholder body)
agency push 20260110                       # unique prefix resolution
```

### `agency verify`

runs the repo's `scripts.verify` for a run and records deterministic verification evidence.

**usage:**
```bash
agency verify <run_id> [--timeout <dur>]
```

**arguments:**
- `run_id`: the run identifier (exact or unique prefix)

**flags:**
- `--timeout`: script timeout (default: `30m`, Go duration format like `10m`, `90s`)

**behavior:**
1. resolve run_id globally (works from anywhere, not just inside a repo)
2. validate workspace exists (not archived)
3. acquire repo lock for the duration of verification
4. run `scripts.verify` with L0 environment variables (timeout: 30m default)
5. read optional `.agency/out/verify.json` structured output
6. write canonical `verify_record.json` with full evidence
7. update `meta.json` with `last_verify_at` and `flags.needs_attention`
8. append `verify_started` and `verify_finished` events to `events.jsonl`

**verify_record.json:**

canonical evidence record written to `${AGENCY_DATA_DIR}/repos/<repo_id>/runs/<run_id>/verify_record.json`:
- `schema_version`: always `"1.0"`
- `repo_id`, `run_id`: identifiers
- `script_path`: exact script string from agency.json
- `started_at`, `finished_at`: RFC3339Nano timestamps
- `duration_ms`, `timeout_ms`: timing info
- `exit_code`: integer or null (null if signal-terminated)
- `signal`: signal name if terminated (e.g., `"SIGKILL"`)
- `timed_out`, `cancelled`: boolean flags (mutually exclusive)
- `ok`: final result after precedence rules
- `summary`: human-readable summary
- `error`: internal errors only (not script failures)

**ok derivation precedence:**
1. if `timed_out` or `cancelled` => `ok=false`
2. else if `exit_code` is null => `ok=false`
3. else if `exit_code != 0` => `ok=false`
4. else if `verify.json` valid => `ok = verify.json.ok`
5. else => `ok=true`

**needs_attention rules:**
- verify ok clears `needs_attention` **only if** reason was `verify_failed`
- verify fail sets `needs_attention=true` with reason `verify_failed`
- verify ok does **not** clear attention for other reasons (e.g., `stop_requested`)

**success output:**
```
ok verify 20260110120000-a3f2 record=/path/to/verify_record.json log=/path/to/verify.log
```

**failure output:**
```
E_SCRIPT_FAILED: verify failed (exit 1) record=/path/to/verify_record.json log=/path/to/verify.log
```

**error codes:**
- `E_RUN_NOT_FOUND` — run not found
- `E_RUN_ID_AMBIGUOUS` — prefix matches multiple runs
- `E_WORKSPACE_ARCHIVED` — run exists but worktree missing or archived
- `E_REPO_LOCKED` — another agency process holds the lock
- `E_SCRIPT_FAILED` — verify script exited non-zero
- `E_SCRIPT_TIMEOUT` — verify script timed out

**notes:**
- does **not** affect `agency push` behavior (push does not run verify)
- does **not** require being in the repo directory
- logs are written to `${AGENCY_DATA_DIR}/repos/<repo_id>/runs/<run_id>/logs/verify.log`
- logs are overwritten per verify run (not appended)
- user cancellation (Ctrl-C) is recorded as `cancelled=true`

**examples:**
```bash
agency verify 20260110120000-a3f2              # run verify with 30m timeout
agency verify 20260110120000-a3f2 --timeout 10m  # custom timeout
agency verify 20260110                          # unique prefix resolution
```

### `agency merge`

verifies, confirms, merges a GitHub PR, and archives the workspace.
requires cwd to be inside the target repo.
requires an interactive terminal for confirmation.

**usage:**
```bash
agency merge <run_id> [--squash|--merge|--rebase] [--force]
```

**arguments:**
- `run_id`: the run identifier (e.g., `20260110120000-a3f2`)

**flags:**
- `--squash`: use squash merge strategy (default)
- `--merge`: use regular merge strategy
- `--rebase`: use rebase merge strategy
- `--force`: bypass verify-failed prompt (still runs verify, still records failure)

**behavior:**
1. runs prechecks:
   - run exists, worktree present
   - origin remote exists and is github.com
   - gh is authenticated
   - PR exists (must run `agency push` first)
   - PR is open, not a draft
   - PR is mergeable (not conflicting)
   - local head matches origin (up-to-date)
2. runs `scripts.verify` (timeout: 30 minutes)
3. if verify fails and no `--force`: prompts to continue (`[y/N]`)
4. prompts for typed confirmation (must type `merge`)
5. merges PR via `gh pr merge` with strategy flag
6. archives workspace (runs archive script, kills tmux, deletes worktree)

**confirmation prompts:**
```
verify failed. continue anyway? [y/N]
confirm: type 'merge' to proceed:
```

**success output:**
```
merged: https://github.com/owner/repo/pull/123
archived: 20260110120000-a3f2
```

**events:**
- `merge_started`, `merge_prechecks_passed`
- `verify_started`, `verify_finished`
- `verify_continue_prompted`, `verify_continue_accepted|rejected` (if verify failed)
- `merge_confirm_prompted`, `merge_confirmed`
- `gh_merge_started`, `gh_merge_finished`
- `archive_started`, `archive_finished|archive_failed`
- `merge_finished`

**error codes:**
- `E_RUN_NOT_FOUND` — run not found
- `E_WORKTREE_MISSING` — run worktree path is missing on disk
- `E_REPO_LOCKED` — another agency process holds the lock
- `E_NOT_INTERACTIVE` — not running in an interactive terminal
- `E_NO_ORIGIN` — no origin remote configured
- `E_UNSUPPORTED_ORIGIN_HOST` — origin is not github.com
- `E_GH_NOT_AUTHENTICATED` — gh not authenticated
- `E_GH_REPO_PARSE_FAILED` — failed to parse owner/repo from origin URL
- `E_NO_PR` — no PR exists for the run (run `agency push` first)
- `E_GH_PR_VIEW_FAILED` — gh pr view failed or returned invalid schema
- `E_PR_NOT_OPEN` — PR is CLOSED or already MERGED
- `E_PR_DRAFT` — PR is a draft
- `E_PR_MISMATCH` — PR head branch doesn't match expected branch
- `E_PR_NOT_MERGEABLE` — PR has conflicts
- `E_PR_MERGEABILITY_UNKNOWN` — GitHub couldn't determine mergeability
- `E_GIT_FETCH_FAILED` — git fetch failed
- `E_REMOTE_OUT_OF_DATE` — local head differs from origin (run `agency push`)
- `E_SCRIPT_FAILED` — verify script exited non-zero
- `E_SCRIPT_TIMEOUT` — verify script timed out
- `E_ABORTED` — user declined confirmation or typed wrong token
- `E_GH_PR_MERGE_FAILED` — gh pr merge failed
- `E_ARCHIVE_FAILED` — archive step failed

**notes:**
- `--force` does NOT bypass: missing PR, non-mergeable PR, gh auth failure, remote out-of-date
- at most one of `--squash`/`--merge`/`--rebase` may be specified
- if already merged (idempotent): skips merge, archives workspace
- PR must exist before merge; agency does NOT call `push` implicitly

**examples:**
```bash
agency merge 20260110120000-a3f2              # squash merge (default)
agency merge 20260110120000-a3f2 --merge      # regular merge
agency merge 20260110120000-a3f2 --rebase     # rebase merge
agency merge 20260110120000-a3f2 --force      # skip verify-fail prompt
```

### `agency clean`

archives a run without merging (abandons the run).
requires cwd to be inside the target repo.
requires an interactive terminal for confirmation.

**usage:**
```bash
agency clean <run_id>
```

**arguments:**
- `run_id`: the run identifier (e.g., `20260110120000-a3f2`)

**behavior:**
1. acquires repo lock
2. prompts for confirmation (must type `clean`)
3. runs `scripts.archive` (timeout: 5 minutes)
4. kills tmux session if exists
5. deletes worktree (git worktree remove, fallback to safe rm -rf)
6. retains metadata and logs in `${AGENCY_DATA_DIR}/repos/<repo_id>/runs/<run_id>/`
7. marks run as abandoned (`flags.abandoned=true`, `archive.archived_at` set)

**confirmation prompt:**
```
confirm: type 'clean' to proceed:
```

**success output:**
```
cleaned: 20260110120000-a3f2
log: /path/to/logs/archive.log
```

**archive failure handling:**
- archive is best-effort: all steps are attempted even if earlier steps fail
- if any step fails: returns `E_ARCHIVE_FAILED` but does not set `archive.archived_at`
- worktree deletion fallback (rm -rf) is only allowed if path is under `${AGENCY_DATA_DIR}/repos/<repo_id>/worktrees/`

**idempotency:**
- if run is already archived: prints `already archived` and exits 0

**events:**
- `clean_started`, `archive_started`
- `archive_finished` (on success) or `archive_failed` (on any failure)
- `clean_finished`

**error codes:**
- `E_NO_REPO` — not inside a git repository
- `E_RUN_NOT_FOUND` — run not found
- `E_WORKTREE_MISSING` — run worktree path is missing on disk
- `E_REPO_LOCKED` — another agency process holds the lock
- `E_NOT_INTERACTIVE` — not running in an interactive terminal
- `E_ABORTED` — user declined confirmation or typed wrong token
- `E_ARCHIVE_FAILED` — archive step failed (script, tmux, or delete failure)

**notes:**
- does **not** merge any PR (use `agency merge` for that)
- does **not** delete git branches (local or remote)
- worktree and tmux session are deleted; metadata and logs are retained

**examples:**
```bash
agency clean 20260110120000-a3f2    # archive without merging
```

## development

### build

```bash
go build -o agency ./cmd/agency
```

### test

```bash
go test ./...
```

### run from source

```bash
go run ./cmd/agency --help
go run ./cmd/agency init --help
go run ./cmd/agency doctor --help
```

## project structure

```
agency/
├── cmd/agency/           # main entry point
├── internal/
│   ├── archive/          # archive pipeline (S6) - script execution, tmux kill, worktree deletion
│   ├── cli/              # command dispatcher (stdlib flag)
│   ├── commands/         # command implementations (init, doctor, run, ls, show, attach, clean, etc.)
│   ├── config/           # agency.json loading + validation (LoadAndValidate, ValidateForS1)
│   ├── core/             # run id generation, slugify, branch naming, shell escaping
│   ├── errors/           # stable error codes + AgencyError type
│   ├── events/           # per-run event logging (events.jsonl append)
│   ├── exec/             # CommandRunner interface + RunScript with timeout
│   ├── fs/               # FS interface + atomic write + WriteJSONAtomic + SafeRemoveAll
│   ├── git/              # repo discovery + origin info + safety gates
│   ├── identity/         # repo_key + repo_id derivation
│   ├── ids/              # run id resolution (exact + unique prefix)
│   ├── lock/             # repo-level locking for mutating commands
│   ├── paths/            # XDG directory resolution
│   ├── pipeline/         # run pipeline orchestrator (step execution, error handling)
│   ├── render/           # output formatting for ls/show (human tables + JSON envelopes)
│   ├── repo/             # repo safety checks + CheckRepoSafe API
│   ├── runservice/       # concrete RunService implementation (wires all steps, setup execution)
│   ├── scaffold/         # agency.json template + stub script creation
│   ├── status/           # pure status derivation from meta + local snapshot
│   ├── store/            # repo_index.json + repo.json + run meta.json + run scanning
│   ├── tmux/             # tmux Client interface, exec-backed impl, session detection, scrollback capture, ANSI stripping
│   ├── tty/              # TTY detection helpers for interactive prompts
│   ├── verify/           # verify script execution engine + evidence recording
│   ├── verifyservice/    # verify pipeline entrypoint (S5) + meta/events integration
│   ├── version/          # build version
│   └── worktree/         # git worktree creation + workspace scaffolding + removal
└── docs/                 # specifications
```

## documentation

- [constitution](docs/v1/constitution.md) — full v1 specification
- [slice roadmap](docs/v1/slice_roadmap.md) — implementation plan
- [slice 0 spec](docs/v1/s0/s0_spec.md) — bootstrap slice detailed spec
- [slice 0 PRs](docs/v1/s0/s0_prs.md) — slice 0 PR breakdown
- [slice 1 spec](docs/v1/s1/s1_spec.md) — run workspace slice detailed spec
- [slice 1 PRs](docs/v1/s1/s1_prs.md) — slice 1 PR breakdown
- [slice 2 spec](docs/v1/s2/s2_spec.md) — observability slice detailed spec
- [slice 2 PRs](docs/v1/s2/s2_prs.md) — slice 2 PR breakdown
- [slice 3 spec](docs/v1/s3/s3_spec.md) — push + PR slice detailed spec
- [slice 3 PRs](docs/v1/s3/s3_prs.md) — slice 3 PR breakdown
- [slice 4 spec](docs/v1/s4/s4_spec.md) — lifecycle control slice detailed spec
- [slice 4 PRs](docs/v1/s4/s4_prs.md) — slice 4 PR breakdown
- [slice 5 spec](docs/v1/s5/s5_spec.md) — verify recording slice detailed spec
- [slice 5 PRs](docs/v1/s5/s5_prs.md) — slice 5 PR breakdown
- [slice 6 spec](docs/v1/s6/s6_spec.md) — merge + archive slice detailed spec
- [slice 6 PRs](docs/v1/s6/s6_prs.md) — slice 6 PR breakdown

## license

MIT
