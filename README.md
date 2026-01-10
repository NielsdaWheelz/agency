# agency

local-first runner manager: creates isolated git workspaces, launches `claude`/`codex` TUIs in tmux, opens GitHub PRs via `gh`.

## status

**v1 in development** — slice 0 (bootstrap) complete, slice 1 complete, slice 2 in progress.

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
- [ ] PR-06: transcript capture + events.jsonl

next: slice 2 PR-06 (transcript capture + events.jsonl)

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
agency merge <id> [--force]       verify, confirm, merge, archive
agency clean <id>                 archive without merging
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
agency show <run_id> [--json] [--path]
```

**arguments:**
- `run_id`: the run identifier (exact) or unique prefix

**flags:**
- `--json`: output as JSON (stable format)
- `--path`: output only resolved filesystem paths

**behavior:**
- resolves run_id globally (works from anywhere, not just inside a repo)
- accepts exact run_id or unique prefix for convenience
- displays rich metadata, derived status, and paths

**id resolution:**
- exact match wins if found
- if no exact match, checks for unique prefix match
- multiple matches: fails with `E_RUN_ID_AMBIGUOUS` and lists candidates
- no matches: fails with `E_RUN_NOT_FOUND`

**human output sections:**
- **run**: core metadata (run_id, title, runner, created_at, repo identity)
- **workspace**: git/workspace info (branches, worktree, tmux session)
- **pr**: PR info if present (pr_number, pr_url, last_push_at)
- **report**: report file info (exists, bytes, path)
- **logs**: script log paths
- **status**: derived status and archived state
- **warnings**: contextual warnings (repo not found, worktree missing)

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

**examples:**
```bash
agency show 20260110120000-a3f2           # show run details
agency show 20260110                       # unique prefix resolution
agency show 20260110120000-a3f2 --json    # machine-readable output
agency show 20260110120000-a3f2 --path    # print paths only
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
- `E_TMUX_SESSION_MISSING` — tmux session does not exist (killed or system restarted)
- `E_TMUX_NOT_INSTALLED` — tmux not found

**when session is missing:**

if the run exists but the tmux session has been killed (e.g., system restarted), attach will fail with `E_TMUX_SESSION_MISSING` and print:
- worktree path
- runner command
- suggested manual command to restart the runner

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
│   ├── cli/              # command dispatcher (stdlib flag)
│   ├── commands/         # command implementations (init, doctor, run, ls, attach)
│   ├── config/           # agency.json loading + validation (LoadAndValidate, ValidateForS1)
│   ├── core/             # run id generation, slugify, branch naming, shell escaping
│   ├── errors/           # stable error codes + AgencyError type
│   ├── exec/             # CommandRunner interface + RunScript with timeout
│   ├── fs/               # FS interface + atomic write + WriteJSONAtomic
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
│   ├── version/          # build version
│   └── worktree/         # git worktree creation + workspace scaffolding
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

## license

MIT
