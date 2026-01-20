# CLI reference

```
agency [--verbose] <command> [options]

global options:
  --verbose              show detailed error context

commands:
agency init [--repo] [--no-gitignore] [--force]
                                  create agency.json template + stub scripts
agency run --name <name> [--runner] [--parent] [--detached]
                                  create workspace, setup, start tmux, attach
agency ls [--repo] [--all] [--all-repos]
                                  list runs + statuses
agency show <id> [--path]         show run details (global)
agency path <id>                  output worktree path (for scripting, global)
agency open <id> [--editor]       open worktree in editor (global)
agency attach <id> [--repo]       attach to tmux session (global)
agency resume <id> [--repo] [--detached] [--restart]
                                  attach to tmux session (create if missing, global)
agency stop <id> [--repo]         send C-c to runner (global)
agency kill <id> [--repo]         kill tmux session (global)
agency push <id> [--allow-dirty] [--force] [--force-with-lease]
                                  push + create/update PR (validates report completeness)
agency verify <id> [--timeout]    run verify script and record results
agency merge <id> [--squash|--merge|--rebase] [--no-delete-branch] [--allow-dirty] [--force]
                                  verify, confirm, merge PR, delete branch, archive
agency clean <id> [--allow-dirty] archive without merging (abandon run)
agency resolve <id>               show conflict resolution guidance
agency doctor                     check prerequisites + show paths
agency completion <shell>         generate shell completion scripts (bash, zsh)
```

## `agency init`

creates `agency.json` template and stub scripts in the current git repo.

**flags:**
- `--no-gitignore`: do not modify `.gitignore` (by default, `.agency/` is appended)
- `--force`: overwrite existing `agency.json` (scripts are never overwritten)

**files created:**
- `agency.json` — configuration file with defaults
- `CLAUDE.md` — runner protocol file (instructs runners on status reporting)
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
claude_md: created
```

## `agency completion`

generates shell completion scripts for bash or zsh.

**usage:**
```bash
agency completion <shell>
```

**arguments:**
- `shell`: target shell (`bash` or `zsh`)

**behavior:**
- prints completion script to stdout
- does not write files or mutate state
- includes installation instructions as comments in the script

**examples:**
```bash
agency completion bash > ~/.agency-completion.bash
agency completion zsh > ~/.zsh/completions/_agency
```

## `agency doctor`

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

## `agency run`

creates an isolated workspace and launches the runner in a tmux session.
by default, attaches to the tmux session after creation.

**usage:**
```bash
agency run --name <name> [--runner <name>] [--parent <branch>] [--detached]
```

**flags:**
- `--name`: run name (required, 2-40 chars, lowercase alphanumeric with hyphens, must start with letter)
- `--runner`: runner name: `claude` or `codex` (default: agency.json `defaults.runner`)
- `--parent`: parent branch to branch from (default: agency.json `defaults.parent_branch`)
- `--detached`: do not attach to tmux session after creation

**behavior:**
1. validates parent working tree is clean (`git status --porcelain`)
2. creates git worktree + branch under `${AGENCY_DATA_DIR}/repos/<repo_id>/worktrees/<run_id>/`
3. creates `.agency/`, `.agency/out/`, `.agency/tmp/`, `.agency/state/` directories
4. creates `.agency/INSTRUCTIONS.md` with runner guidance (overwritten on every run)
5. creates `.agency/report.md` with template (name as heading, requires filling before push)
6. runs `scripts.setup` with injected environment variables (timeout: 10 minutes)
7. creates tmux session `agency_<run_id>` running the runner command
8. writes `meta.json` with run metadata
9. attaches to tmux session (unless `--detached`)

**success output (with `--detached`):**
```
run_id: 20260110120000-a3f2
name: feature-x
runner: claude
parent: main
branch: agency/feature-x-a3f2
worktree: ~/Library/Application Support/agency/repos/abc123/worktrees/20260110120000-a3f2
tmux: agency_20260110120000-a3f2
next: agency attach feature-x
```

note: the `next:` line is only shown with `--detached`. when attached (default), you are placed directly into the tmux session.

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
- `E_TMUX_ATTACH_FAILED` — tmux attach failed

**on failure:**

if the run fails after worktree creation, the error output includes:
- `run_id`
- `worktree` path (for inspection)
- `setup_log` path (if setup failed)

the worktree and metadata are retained for debugging; use `agency clean <id>` to remove.

## `agency ls`

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
- `NAME`: run name (truncated to 50 chars; `<broken>` for corrupt meta; `<untitled>` for empty)
- `STATUS`: derived status (e.g., "active", "idle", "ready for review", "merged (archived)")
- `SUMMARY`: runner-reported summary (truncated to 40 chars; shows stall duration for stalled runs; `-` if unavailable)
- `PR`: PR number if exists (e.g., "#123")

**example output:**
```
RUN_ID              NAME            STATUS            SUMMARY                    PR
20260119-a3f2       auth-fix        needs input       Which auth library?        #123
20260118-c5d2       bug-fix         stalled           (no activity for 45m)      -
20260118-e7f3       feature-x       working           Implementing validation    -
```

**empty state:**
- inside repo without `--all`: `no active runs (use --all to include archived)`
- inside repo with `--all`: `no runs found`
- outside repo / `--all-repos`: `no runs found`

**status values** (in precedence order):
- `broken`: meta.json is unreadable/invalid
- `merged`: PR merged
- `abandoned`: explicitly abandoned
- `failed`: setup script failed
- `needs attention`: verify failed, PR not mergeable, or stop requested
- `ready for review`: runner reports work complete
- `needs input`: runner waiting for user answer
- `blocked`: runner cannot proceed
- `working`: runner actively making progress
- `stalled`: no status update for 15+ minutes (tmux active)
- `active`: tmux session exists (fallback when no runner status)
- `idle`: no tmux session (fallback)
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
      "name": "feature-x",
      "runner": "claude",
      "created_at": "2026-01-10T12:00:00Z",
      "last_push_at": "2026-01-10T14:00:00Z",
      "tmux_active": true,
      "worktree_present": true,
      "archived": false,
      "pr_number": 123,
      "pr_url": "https://github.com/owner/repo/pull/123",
      "derived_status": "ready for review",
      "summary": "Implementing user authentication",
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

## `agency show`

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
name: feature-x
repo: abc123
runner: claude
parent: main
branch: agency/feature-x-a3f2
worktree: ~/Library/Application Support/agency/repos/abc123/worktrees/20260110120000-a3f2

tmux: agency_20260110120000-a3f2
pr: https://github.com/owner/repo/pull/123 (#123)
last_push_at: 2026-01-10T14:00:00Z
last_report_sync_at: 2026-01-10T14:00:00Z
report_hash: abc123def456...
status: ready for review

runner_status:
  status: needs_input
  updated: 5m ago
  summary: Implementing OAuth but need clarification
  questions:
    - Which OAuth provider should I use?
    - Should sessions persist across restarts?
```

note: there is a blank line between `worktree:` and `tmux:`.

when PR is missing: `pr: none (#-)`
when timestamps are missing: `last_push_at: none`
runner_status section only appears when `.agency/state/runner_status.json` exists and is valid.

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
      "logs": { "setup_log_path": "...", "verify_log_path": "...", "archive_log_path": "..." },
      "runner_status": {
        "status": "needs_input",
        "updated_at": "2026-01-10T14:00:00Z",
        "summary": "Implementing OAuth but need clarification",
        "questions": ["Which OAuth provider?"],
        "blockers": [],
        "how_to_test": "",
        "risks": []
      }
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
agency show my-feature           # show run details
agency show my-feature --json    # machine-readable output
agency show my-feature --path    # print paths only
agency show my-feature --capture # capture transcript + show
agency show my-feature --json | jq '.data.derived.derived_status'
```

## `agency attach`

attaches to an existing tmux session for a run.

**usage:**
```bash
agency attach <run_id>
```

**arguments:**
- `run_id`: the run identifier or name

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

if the run exists but the tmux session has been killed (e.g., system restarted), attach will fail with `E_SESSION_NOT_FOUND` and suggest using `agency resume <name>` instead:

```
E_SESSION_NOT_FOUND: tmux session 'agency_<run_id>' does not exist

try: agency resume <name>
```

## `agency stop`

sends C-c to the runner in the tmux session (best-effort interrupt).

**usage:**
```bash
agency stop <run_id>
```

**arguments:**
- `run_id`: the run identifier or name

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

## `agency kill`

kills the tmux session for a run. Workspace remains intact.

**usage:**
```bash
agency kill <run_id>
```

**arguments:**
- `run_id`: the run identifier or name

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

## `agency resume`

attaches to the tmux session for a run. If session is missing, creates one and starts the runner.

**usage:**
```bash
agency resume <run_id> [--detached] [--restart] [--yes]
```

**arguments:**
- `run_id`: the run identifier or name

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
ok: session agency_<run_id> ready
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
agency resume my-feature               # attach (create if needed)
agency resume my-feature --detached    # ensure session exists
agency resume my-feature --restart     # force fresh runner (prompts)
agency resume my-feature --restart --yes  # non-interactive restart
```

## `agency push`

pushes the run branch to origin and creates/updates a GitHub PR.

**usage:**
```bash
agency push <run_id> [--allow-dirty] [--force]
```

**arguments:**
- `run_id`: the run identifier (exact or unique prefix)

**flags:**
- `--allow-dirty`: proceed even if worktree has uncommitted changes
- `--force`: proceed even if report is incomplete (missing required sections); does NOT bypass missing file error

**preflight checks (in order):**
1. resolve run_id and load metadata
2. verify worktree exists on disk
3. acquire repo lock (mutating command)
4. fail if worktree has uncommitted changes (unless `--allow-dirty`)
5. verify `origin` remote exists
6. verify origin host is exactly `github.com`
7. report gating:
   - fail if report file missing (`E_REPORT_INVALID`, no bypass)
   - fail if report incomplete (`E_REPORT_INCOMPLETE`, bypassed by `--force`)
   - incomplete = missing `## summary` or `## how to test` content
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
   - title: `[agency] <run_name>`
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
- `E_DIRTY_WORKTREE` — worktree has uncommitted changes without `--allow-dirty`
- `E_NO_ORIGIN` — no origin remote configured
- `E_UNSUPPORTED_ORIGIN_HOST` — origin is not github.com
- `E_REPORT_INVALID` — report file missing
- `E_REPORT_INCOMPLETE` — report exists but missing required sections (summary, how to test)
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
- `--force` bypasses `E_REPORT_INCOMPLETE` (incomplete content) but NOT `E_REPORT_INVALID` (missing file)
- `--force` does NOT bypass `E_EMPTY_DIFF` (must have commits)
- `--allow-dirty` prints a warning and dirty context
- `--force-with-lease` uses `git push --force-with-lease` for safe force push after rebase

**non-fast-forward handling:**

when push fails due to non-fast-forward (e.g., after rebasing), agency detects this and prints a helpful hint:

```
error_code: E_GIT_PUSH_FAILED
push rejected (non-fast-forward)

hint: branch was rebased or amended; retry with:
  agency push <ref> --force-with-lease
```

**examples:**
```bash
agency push my-feature                 # push branch + create/update PR
agency push my-feature --force         # push with incomplete report (placeholder body)
agency push my-feature --allow-dirty   # push with dirty worktree
agency push my-feature --force-with-lease  # force push after rebase
```

## `agency verify`

runs the repo's `scripts.verify` for a run and records deterministic verification evidence.

**usage:**
```bash
agency verify <run_id> [--timeout <dur>]
```

**arguments:**
- `run_id`: the run identifier (exact or unique prefix)

**flags:**
- `--timeout`: script timeout override (Go duration format like `10m`, `90s`); defaults to `agency.json` configured timeout

**behavior:**
1. resolve run_id globally (works from anywhere, not just inside a repo)
2. validate workspace exists (not archived)
3. acquire repo lock for the duration of verification
4. run `scripts.verify` with L0 environment variables (timeout from config)
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
ok verify my-feature record=/path/to/verify_record.json log=/path/to/verify.log
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
agency verify my-feature                # run verify with configured timeout
agency verify my-feature --timeout 10m  # custom timeout
```

## `agency merge`

verifies, confirms, merges a GitHub PR, and archives the workspace.
requires cwd to be inside the target repo.
requires an interactive terminal for confirmation.

**usage:**
```bash
agency merge <run_id> [--squash|--merge|--rebase] [--no-delete-branch] [--force]
```

**arguments:**
- `run_id`: the run identifier or name

**flags:**
- `--squash`: use squash merge strategy (default)
- `--merge`: use regular merge strategy
- `--rebase`: use rebase merge strategy
- `--no-delete-branch`: preserve the remote branch after merge (default: delete)
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
5. merges PR via `gh pr merge --delete-branch` (deletes remote branch by default)
6. archives workspace (runs archive script, kills tmux, deletes worktree)

**confirmation prompts:**
```
verify failed. continue anyway? [y/N]
confirm: type 'merge' to proceed:
```

**success output:**
```
merged: my-feature
pr: https://github.com/owner/repo/pull/123
log: /path/to/logs/archive.log
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
- if already merged (idempotent): skips verify/mergeability checks, prompts for confirmation, archives workspace
- PR must exist before merge; agency does NOT call `push` implicitly
- gh merge output is captured to `${AGENCY_DATA_DIR}/repos/<repo_id>/runs/<run_id>/logs/merge.log`
- post-merge confirmation: agency verifies PR reached `MERGED` state with retries (250ms, 750ms, 1500ms backoff)
- by default, the remote branch is deleted after merge; use `--no-delete-branch` to preserve it

**merge conflict handling:**

when merge fails due to conflicts, agency provides an actionable resolution path:

```
error_code: E_PR_NOT_MERGEABLE
PR #93 has conflicts with main and cannot be merged.

pr: https://github.com/owner/repo/pull/93
base: main
branch: agency/feature-x-a3f2
worktree: /path/to/worktree

next:

1. agency open feature-x
2. git fetch origin
3. git rebase origin/main
4. resolve conflicts, then:
   git add -A && git rebase --continue
5. agency push feature-x --force-with-lease
6. agency merge feature-x

alt: cd "/path/to/worktree"
```

**examples:**
```bash
agency merge my-feature                       # squash merge, delete branch (default)
agency merge my-feature --merge               # regular merge, delete branch
agency merge my-feature --rebase              # rebase merge, delete branch
agency merge my-feature --no-delete-branch    # squash merge, preserve branch
agency merge my-feature --force               # skip verify-fail prompt
```

## `agency resolve`

shows conflict resolution guidance for a run.
provides step-by-step instructions to resolve merge conflicts via rebase.
read-only: makes no git changes, does not require repo lock.

**usage:**
```bash
agency resolve <run_id>
```

**arguments:**
- `run_id`: the run identifier (name, exact run_id, or unique prefix)

**behavior:**
- if worktree present: prints action card to stdout, exits 0
- if worktree missing: prints partial guidance to stderr, exits with `E_WORKTREE_MISSING`

**output (worktree present):**
```
pr: https://github.com/owner/repo/pull/93
base: main
branch: agency/feature-x-a3f2
worktree: /path/to/worktree

next:

1. agency open feature-x
2. git fetch origin
3. git rebase origin/main
4. resolve conflicts, then:
   git add -A && git rebase --continue
5. agency push feature-x --force-with-lease
6. agency merge feature-x

alt: cd "/path/to/worktree"
```

**output (worktree missing):**
```
error_code: E_WORKTREE_MISSING
worktree archived or missing

pr: https://github.com/owner/repo/pull/93
base: main
branch: agency/feature-x-a3f2

hint: worktree no longer exists; resolve conflicts via GitHub web UI or restore locally
```

**examples:**
```bash
agency resolve my-feature
```

## `agency clean`

archives a run without merging (abandons the run).
requires cwd to be inside the target repo.
requires an interactive terminal for confirmation.

**usage:**
```bash
agency clean <run_id>
```

**arguments:**
- `run_id`: the run identifier or name

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
cleaned: my-feature
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
agency clean my-feature    # archive without merging
```

## error output

agency uses structured error output with stable error codes.

### default error format

```
error_code: E_...
<one-line message>

<context (key: value pairs)>

hint: <actionable guidance>
```

example (verify failure):

```
error_code: E_SCRIPT_FAILED
verify failed (exit 1)

script: scripts/agency_verify.sh
exit_code: 1
duration: 12.3s
log: /path/to/verify.log
record: /path/to/verify_record.json

output (last 20 lines):
  npm ERR! Test failed
  npm ERR! code ELIFECYCLE
  ...

hint: fix the failing tests and run: agency verify my-feature
```

### `--verbose` mode

use `agency --verbose <command>` to see additional context:

- more context keys displayed
- longer output tails (up to 100 lines)
- extra details section with all remaining metadata

```bash
agency --verbose push my-feature
agency --verbose merge my-feature
```
