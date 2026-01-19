# Agency L0: Constitution (v1 MVP)

Local-first runner manager: creates isolated git workspaces, launches `claude`/`codex` TUIs in tmux, opens GitHub PRs via `gh`.

---

## 1) Purpose

Agency makes "spin up an AI coding session on a clean branch" trivial, inspectable, and reversible.

Core loop:
1. Create workspace from parent branch (requires clean working tree).
2. Run `claude` or `codex` in tmux session.
3. Push branch + create PR (`agency push`).
4. User reviews via PR or locally.
5. User confirms merge.
6. Agency merges via `gh pr merge --delete-branch` and archives.

---

## 2) Non-goals (v1)

- no planner/council mode
- no headless automation
- no sandboxing/containers
- no cross-host orchestration
- no multi-repo intelligence
- no transcript replay on resume
- no auto-update / self-update

---

## 3) Primitives

**repo**: local git checkout; may be non-GitHub for init/doctor/run. `agency push`/`agency merge` require a GitHub `origin`.

**repo identity**:
- primary: `github:<owner>/<repo>` parsed from `origin` when it is a github.com remote
  - supports `git@github.com:<owner>/<repo>.git`
  - supports `https://github.com/<owner>/<repo>.git`
- fallback: `path:<sha256(abs_path)>` (for non-GitHub remotes or unsupported URL formats)
- GitHub Enterprise hosts are not supported in v1; treat them as non-GitHub and use the fallback
- stored in `${AGENCY_DATA_DIR}/repo_index.json` (schema defined below)

**workspace (run)**: git worktree + branch + tmux session + metadata. survives multiple invocations.

**run_id**: `<timestamp>-<random>` (e.g., `20250109-a3f2`)

**invocation**: one execution of runner in workspace. may exit; workspace persists. relaunch via `resume`.

**runner**: `claude` or `codex` (must be on PATH, or specify command in user config).

---

## 4) Hard constraints

- implementation: **Go**
- github integration: **`gh` CLI** only
- attach/detach: **tmux** only
- isolation: **git worktrees**
- config: **`agency.json`** required at repo root (scripts only)
- scripts: **setup/verify/archive** required
- merge: **human confirmation** required
- cli parsing: **stdlib `flag`** in v1

---

## 5) Packaging and distribution

**supported install methods (v1)**:
- dev install: `go install github.com/NielsdaWheelz/agency@latest`
- releases: github releases with prebuilt binaries (darwin-amd64, darwin-arm64, linux-amd64)
- homebrew: `brew install NielsdaWheelz/tap/agency`

**not supported (v1)**:
- auto-update / self-update
- linux distro packages (apt/yum/pacman)

---

## 6) Prerequisites

Agency requires (checked via `agency doctor`):
- `git`
- `gh` (authenticated: `gh auth status`)
- `tmux`
- configured runner (resolved from user config; must be on PATH or explicit path)
- user config file present (`$AGENCY_CONFIG_DIR/config.json`)
- configured editor (resolved from user config; must be on PATH or explicit path)
- scripts `setup/verify/archive` exist and are executable

`agency doctor` also prints resolved directory paths (data, config, cache).
`agency doctor` exits 0 only when all required tools/scripts are present and `gh auth status` succeeds. origin may be absent; GitHub flow availability does not affect success.

---

## 7) Directories

### Directory resolution

**data directory** (`AGENCY_DATA_DIR`):
1. if `$AGENCY_DATA_DIR` set: use it
2. else if macOS: `~/Library/Application Support/agency`
3. else if `$XDG_DATA_HOME` set: `$XDG_DATA_HOME/agency`
4. else: `~/.local/share/agency`

**config directory** (used for user config):
1. if `$AGENCY_CONFIG_DIR` set: use it
2. else if macOS: `~/Library/Preferences/agency`
3. else if `$XDG_CONFIG_HOME` set: `$XDG_CONFIG_HOME/agency`
4. else: `~/.config/agency`

**user config file**:
- `${AGENCY_CONFIG_DIR}/config.json`

**cache directory** (reserved, unused in v1):
1. if `$AGENCY_CACHE_DIR` set: use it
2. else if macOS: `~/Library/Caches/agency`
3. else if `$XDG_CACHE_HOME` set: `$XDG_CACHE_HOME/agency`
4. else: `~/.cache/agency`

All global state lives under `${AGENCY_DATA_DIR}`.

---

## 8) agency.json

**location**: must exist at repo root (`git rev-parse --show-toplevel`). no subdir or monorepo overrides in v1.

```json
{
  "version": 1,
  "scripts": {
    "setup": "scripts/agency_setup.sh",
    "verify": "scripts/agency_verify.sh",
    "archive": "scripts/agency_archive.sh"
  }
}
```

**required fields**:
- `scripts.setup`, `scripts.verify`, `scripts.archive`

**validation (v1)**:
- `version` must be integer `1`
- `scripts.setup|verify|archive` must be non-empty strings
- any other top-level key is invalid (including `defaults` and `runners`)

**schema versioning (v1)** (applies to `agency.json`, `config.json`, `meta.json`, `events.jsonl`):
- additive only
- new required fields must bump version
- config files may reject unknown top-level keys (strict validation)
- persistence files ignore unknown fields

---

## 8.1) user config (`config.json`)

**location**: `${AGENCY_CONFIG_DIR}/config.json`

```json
{
  "version": 1,
  "defaults": {
    "runner": "claude",
    "editor": "code"
  },
  "runners": {
    "claude": "claude",
    "codex": "codex"
  },
  "editors": {
    "code": "code"
  }
}
```

**required fields**:
- `defaults.runner`
- `defaults.editor`

**validation (v1)**:
- `version` must be integer `1`
- `defaults.runner|editor` must be non-empty strings
- `runners` if present must be object of string -> string (values non-empty, no whitespace)
- `editors` if present must be object of string -> string (values non-empty, no whitespace)
- unknown top-level keys are invalid
 - invalid user config fails all commands (no fallback)

**runner resolution**:
- if `runners.<name>` exists: use that command
- else if `defaults.runner` is `claude` or `codex`: assume on PATH
- else: error `E_RUNNER_NOT_CONFIGURED`

**editor resolution**:
- if `editors.<name>` exists: use that command
- else: use `defaults.editor` as command on PATH
- if the resolved command is missing or not executable: `E_EDITOR_NOT_CONFIGURED`

**missing config file**:
- for most commands: use built-in defaults
- for `agency doctor`: error `E_INVALID_USER_CONFIG`

**invalid config file**:
- `E_INVALID_USER_CONFIG` for all commands (no fallback)

**built-in defaults (when config is missing)**:
- `defaults.runner`: `claude`
- `defaults.editor`: `code`

## 9) Scripts

**requirements**:
- non-interactive (stdin is `/dev/null`)
- idempotent
- run outside tmux (subprocess, not in runner pane)
- timeouts: setup 10m, verify 30m, archive 5m

**semantics**: exit 0 = pass, non-zero = fail. stdout/stderr logged.

**workspace directories** (created by agency before scripts run):
- `<worktree>/.agency/`
- `<worktree>/.agency/out/`
- `<worktree>/.agency/tmp/`

**environment** (injected by agency):
- `AGENCY_RUN_ID`
- `AGENCY_NAME`
- `AGENCY_REPO_ROOT`
- `AGENCY_WORKSPACE_ROOT`
- `AGENCY_BRANCH`
- `AGENCY_PARENT_BRANCH`
- `AGENCY_ORIGIN_NAME` (usually `origin`)
- `AGENCY_ORIGIN_URL`
- `AGENCY_RUNNER` (e.g., `claude`)
- `AGENCY_PR_URL` (empty string if no PR)
- `AGENCY_PR_NUMBER` (empty string if no PR)
- `AGENCY_DOTAGENCY_DIR` — `<worktree>/.agency/`
- `AGENCY_OUTPUT_DIR` — `<worktree>/.agency/out/`
- `AGENCY_LOG_DIR` — `${AGENCY_DATA_DIR}/repos/<repo_id>/runs/<run_id>/logs/`
- `AGENCY_NONINTERACTIVE=1`
- `CI=1`

**structured outputs** (optional in v1):

Scripts may write to `.agency/out/<script>.json` where `<script>` is `setup`, `verify`, or `archive`. if present, must follow schema:

```json
{
  "schema_version": "1.0",
  "ok": true,
  "summary": "one-line description",
  "data": {}
}
```

supported files (v1):
- `.agency/out/setup.json`
- `.agency/out/verify.json`
- `.agency/out/archive.json`

If present, agency uses `ok` only when the script exits 0; verify.json may downgrade success but never upgrade failure. If absent, uses exit code only.

---

## 10) Storage

### Global (`${AGENCY_DATA_DIR}`)

```
${AGENCY_DATA_DIR}/
  repo_index.json
  repos/<repo_id>/
    repo.json
    runs/<run_id>/
      meta.json           # run metadata (retained on archive)
      events.jsonl        # append-only event log
      logs/               # script stdout/stderr
    worktrees/<run_id>/   # git worktree (deleted on archive)
```

`repo_id` = sha256(repo_key) truncated to 16 hex chars.

### Atomic write behavior (v1)

- JSON files are written via temp file + atomic rename.
- Do not write `repo_index.json` or `repo.json` unless `agency doctor` succeeds.
- Optional: fsync temp file and parent directory before rename (not required in v1).

### repo_index.json (public contract, v1)

Global index mapping repository keys to their metadata. Written only on successful `agency doctor`.

**Schema:**
- `schema_version` (string): `"1.0"`
- `repos` (object): keyed by `repo_key`
  - `repo_id` (string): sha256 hash truncated to 16 hex chars
  - `paths` (array of strings): known absolute paths, most recent first
  - `last_seen_at` (string): ISO 8601 timestamp in UTC

**Example:**
```json
{
  "schema_version": "1.0",
  "repos": {
    "github:owner/repo": {
      "repo_id": "abcd1234ef567890",
      "paths": ["/Users/dev/projects/repo"],
      "last_seen_at": "2025-01-09T12:34:56Z"
    },
    "path:f1e2d3c4b5a69870": {
      "repo_id": "f1e2d3c4b5a69870",
      "paths": ["/Users/dev/local-only-project"],
      "last_seen_at": "2025-01-09T12:35:00Z"
    }
  }
}
```

**Merge behavior:**
- Existing entries: update `last_seen_at`, move current path to front of `paths` list
- New entries: create with single path and current timestamp
- Paths are de-duplicated case-sensitively

### repo.json (public contract, v1)

Per-repository metadata. Written only on successful `agency doctor`.

**Schema:**
- `schema_version` (string): `"1.0"`
- `repo_key` (string): `github:<owner>/<repo>` or `path:<sha256>`
- `repo_id` (string): sha256 hash truncated to 16 hex chars
- `repo_root_last_seen` (string): absolute path to repo root
- `agency_json_path` (string): absolute path to agency.json
- `origin_present` (bool): whether origin remote exists
- `origin_url` (string): remote URL or empty string
- `origin_host` (string): hostname or empty string
- `capabilities` (object):
  - `github_origin` (bool): whether origin is github.com
  - `origin_host` (string): hostname or empty string
  - `gh_authed` (bool): whether gh is authenticated
- `created_at` (string): ISO 8601 timestamp when first created
- `updated_at` (string): ISO 8601 timestamp when last updated

**Example (GitHub repo):**
```json
{
  "schema_version": "1.0",
  "repo_key": "github:owner/repo",
  "repo_id": "abcd1234ef567890",
  "repo_root_last_seen": "/Users/dev/projects/repo",
  "agency_json_path": "/Users/dev/projects/repo/agency.json",
  "origin_present": true,
  "origin_url": "git@github.com:owner/repo.git",
  "origin_host": "github.com",
  "capabilities": {
    "github_origin": true,
    "origin_host": "github.com",
    "gh_authed": true
  },
  "created_at": "2025-01-09T12:34:56Z",
  "updated_at": "2025-01-09T12:34:56Z"
}
```

**Example (local-only repo):**
```json
{
  "schema_version": "1.0",
  "repo_key": "path:f1e2d3c4b5a69870",
  "repo_id": "f1e2d3c4b5a69870",
  "repo_root_last_seen": "/Users/dev/local-only-project",
  "agency_json_path": "/Users/dev/local-only-project/agency.json",
  "origin_present": false,
  "origin_url": "",
  "origin_host": "",
  "capabilities": {
    "github_origin": false,
    "origin_host": "",
    "gh_authed": true
  },
  "created_at": "2025-01-09T12:35:00Z",
  "updated_at": "2025-01-09T12:35:00Z"
}
```

**Timestamp semantics:**
- `created_at`: set once when record is first created, never updated
- `updated_at`: updated on every successful `agency doctor`

### meta.json (public contract, v1)

Required fields:
- `schema_version`
- `run_id`
- `repo_id`
- `name`
- `runner`
- `parent_branch`
- `branch`
- `worktree_path`
- `created_at`
- `tmux_session_name`

Optional fields:
- `pr_number`
- `pr_url`
- `last_push_at`
- `last_report_sync_at` — set when PR body updated from report
- `last_report_hash` — sha256 of report contents when synced
- `last_verify_at`
- `flags.needs_attention`
- `flags.setup_failed`
- `flags.abandoned`
- `archive.archived_at`
- `archive.merged_at`

### events.jsonl (public contract, v1)

Append-only. each line is a JSON object:
- `schema_version`
- `event`
- `timestamp`
- `repo_id`
- `run_id`
- `data` (optional object)

### Workspace-local (`<worktree>/.agency/`)

- `.agency/report.md` — synced to PR body on push
- `.agency/out/` — script outputs
- `.agency/tmp/` — scratch space

### Archived state

`agency clean` or post-merge archive:
- deletes worktree directory
- deletes tmux session (if exists)
- **retains** `${AGENCY_DATA_DIR}/repos/<repo_id>/runs/<run_id>/` (meta.json, logs/)

### Retention

v1: archived metadata retained indefinitely. user can manually delete `${AGENCY_DATA_DIR}/repos/<repo_id>/runs/<run_id>/` to reclaim space. `agency gc` deferred to post-v1.

---

## 11) Status model

Status is **composable**, not a flat enum:

### Terminal outcome (mutually exclusive)
- `open` — not merged or abandoned
- `merged` — PR merged via gh
- `abandoned` — user explicitly abandoned

### Workspace presence (mutually exclusive)
- `present` — worktree exists
- `archived` — worktree deleted (clean/archive)

### Runtime (only if workspace present)
- `active` — tmux session `tmux.SessionName(<run_id>)` exists
- `idle` — no tmux session

### Flags (can be combined)
- `needs_attention` — verify failed OR PR not mergeable OR stop requested
- `setup_failed` — setup script exited non-zero

### Display status

`agency ls` shows a single derived string for UX. derive in layers:
1. base outcome: `merged` | `abandoned` | `open`
2. presence suffix: if `archived` -> append " (archived)"
3. flags (for `open`):
   - if `setup_failed` -> "failed" + presence suffix
   - else if `needs_attention` -> "needs attention" + presence suffix
4. else (for `open`):
   - if PR exists and `last_push_at` recorded and report exists + non-empty -> "ready for review" + presence suffix
   - else if `active` and PR open -> "active (report missing)" + presence suffix
   - else if `active` -> "active" + presence suffix
   - else if PR open -> "idle (pr open)" + presence suffix
   - else -> "idle" + presence suffix

`agency ls` defaults to current repo and excludes archived runs. use `--all` for archived and `--all-repos` for global view.

**empty state output:**
- inside repo without `--all`: `no active runs (use --all to include archived)`
- inside repo with `--all`: `no runs found`
- outside repo / `--all-repos`: `no runs found`

### Runner detection (v1)

`active` = tmux session exists. no pid inspection in v1.

---

## 12) Git + GitHub

**repo discovery**: `git rev-parse --show-toplevel` from cwd.

**branch naming**: `agency/<name>-<shortid>`
- name: run name (validated: lowercase alphanumeric with hyphens, 2-40 chars, starts with letter)
- shortid: first 4 chars of run_id

**parent branch**: use `--parent <branch>` if provided; otherwise use current branch at run time.

**clean working tree**: `agency run` requires:
- cwd not inside existing agency worktree
- repo checkout has clean `git status --porcelain`

**remote requirement (v1)**: `origin` must exist and point to `github.com` (ssh or https) for `agency push`/`agency merge`. `repo_key` may still fall back to a path-based key for indexing, but GitHub PR flows require the GitHub origin. if hostname != `github.com`: `E_UNSUPPORTED_ORIGIN_HOST`.

**command cwd**: all git/gh operations run with `-C <worktree_path>` (or `cwd=worktree`) except:
- the parent working tree cleanliness check (repo root)
- `git worktree remove` (repo root)

### Push behavior

`agency push <id>`:
0. require clean worktree unless `--allow-dirty` (if dirty: `E_DIRTY_WORKTREE`)
1. `git fetch <origin>` — ensures remote refs exist; does NOT rebase, reset, or modify local branches
2. check commits ahead: `git rev-list --count <parent_branch>..<workspace_branch> > 0`
3. `git push -u origin <workspace_branch>`
4. if no PR exists and commits ahead > 0: create PR via `gh pr create`
5. PR identity: repo + head branch in origin (`gh pr view --head <workspace_branch> --json number,url`)
6. on update, prefer stored PR number; fallback to branch lookup
7. if `.agency/report.md` exists and non-empty: sync to PR body
8. store PR url/number in metadata

### Merge behavior

1. require existing PR; if missing: `E_NO_PR` with guidance to run `agency push <id>`
2. check `gh pr view --json mergeable`
3. run `scripts.verify`, record result
4. if verify failed: prompt "continue anyway?" (skip with `--force`)
5. prompt for human confirmation (accept `strings.TrimSpace(input) == "merge"`)
6. `gh pr merge --delete-branch` with exactly one strategy flag; if none provided default to `--squash` (more than one -> `E_USAGE`)
   - by default, the remote branch is deleted after merge
   - use `--no-delete-branch` to preserve the branch
7. archive workspace

if not mergeable: `E_PR_NOT_MERGEABLE`. no auto-rebase.

---

## 13) Report

Lives at `<worktree>/.agency/report.md`.

Created on `agency run` with a template; name used as heading.

Template:

```markdown
# <name>

## summary
- what changed (high level)
- why (intent)

## scope
- completed
- explicitly not done / deferred

## decisions
- important choices + rationale
- tradeoffs

## deviations
- where it diverged from spec + why

## problems encountered
- failing tests, tricky bugs, constraints

## how to test
- exact commands
- expected output

## review notes
- files deserving scrutiny
- potential risks

## follow-ups
- blockers or questions
```

**push validation**:
- requires a clean worktree unless `--allow-dirty`
- warns if `.agency/report.md` is missing or effectively empty; use `--force` to push anyway.

---

## 14) Commands

```
agency init                       create agency.json template
agency run --name <name> [--runner] [--parent]
                                  create workspace, setup, start tmux
agency ls                         list runs + statuses
agency show <ref> [--path]        show run details
agency open <ref> [--editor]      open run worktree in editor
agency attach <ref>               attach to tmux session
agency resume <ref> [--detached] [--restart]
                                  attach to tmux session (create if missing)
agency stop <ref>                 send C-c to runner (best-effort)
agency kill <ref>                 kill tmux session
agency push <ref> [--allow-dirty] [--force]
                                  push + create/update PR
agency merge <id> [--squash|--merge|--rebase] [--no-delete-branch] [--allow-dirty] [--force]
                                  verify, confirm, merge, delete branch, archive
agency clean <id> [--allow-dirty] archive without merging
agency doctor                     check prerequisites + show paths
```

### Run reference resolution

`<ref>` can be:
- **name** — exact match against run name (active runs only; archived runs excluded)
- **run_id** — exact match against full run_id
- **prefix** — unique prefix of run_id

Resolution priority: exact name → exact run_id → unique run_id prefix.

For repo-scoped commands (`attach`, `resume`, `stop`, `kill`, `clean`), resolution is limited to the current repository. For global commands (`show`, `open`, `push`, `merge`), resolution spans all repositories.

Archived runs are excluded from name matching but can still be resolved by run_id or prefix.

### Init semantics

`agency init` writes the template and appends `.agency/` to the repo `.gitignore` by default. use `--no-gitignore` for a non-invasive mode.
template includes `version` + `scripts` only; defaults/runners live in user config.
`agency init` also creates stub scripts if missing:
- `scripts/agency_setup.sh` (exit 0)
- `scripts/agency_verify.sh` (print "replace scripts/agency_verify.sh" and exit 1)
- `scripts/agency_archive.sh` (exit 0)

Scripts are never overwritten by init.
`agency init` writes `agency.json` via atomic write (temp file + rename).
Stub scripts:
- path normalization: always under repo root (no absolute paths)
- file mode: 0755
- contents (setup/archive):
  - `#!/usr/bin/env bash`
  - `set -euo pipefail`
  - comment indicating it is a stub
  - `exit 0`
- contents (verify):
  - `#!/usr/bin/env bash`
  - `set -euo pipefail`
  - comment indicating it is a stub and must be replaced
  - `echo "replace scripts/agency_verify.sh"`
  - `exit 1`

### Resume semantics

`agency resume <ref>`:
1. resolve run within current repo by name, run_id, or prefix (see "Run reference resolution")
2. if tmux session exists: attach unless `--detached`
3. if tmux session missing: create `tmux.SessionName(<run_id>)` with `cwd=worktree`, run runner, then attach unless `--detached`

`agency resume <ref> --restart`:
1. kill session (if exists)
2. recreate session and run runner

no idle detection in v1; tmux session existence is the only signal.

### Open semantics

`agency open <ref>`:
1. resolve run globally by name, run_id, or prefix (see "Run reference resolution")
2. resolve editor from `config.json` defaults (override with `--editor`)
3. execute editor command with worktree path as a single argument
4. read-only: no repo lock, no meta mutations, no events

### Stop semantics

`agency stop <ref>`:
1. `tmux send-keys -t tmux.SessionName(<run_id>) C-c` (best-effort interrupt)
2. sets `needs_attention` flag regardless of whether interrupt succeeded
3. tmux session stays alive; use `agency resume --restart` to guarantee a fresh runner

stop is best-effort: C-c may cancel an in-tool operation, exit the tool, or do nothing. it may not interrupt model work and can leave the tool in an inconsistent state; v1 accepts this risk.

`agency kill <ref>`:
- `tmux kill-session -t tmux.SessionName(<run_id>)`
- workspace persists

---

## 15) Concurrency

v1: **single-writer model**. agency refuses concurrent mutation on the same run.

implementation: coarse repo-level lock file (`${AGENCY_DATA_DIR}/repos/<repo_id>/.lock`). if lock held, error `E_REPO_LOCKED`.
- lock file contains pid + timestamp
- stale detection required (pid not alive -> treat lock as stale)
- `agency doctor` reports how to clear stale locks
- lock only for mutating commands: `run`, `push`, `merge`, `clean`, `resume --restart`
- `stop` and `kill` are best-effort and bypass the lock
- read-only commands (`ls`, `show`, `open`, `attach`, `resume` without `--restart`, `doctor`) do not take the lock

---

## 16) Invariants

- never modify parent working tree silently
- never merge without human confirmation
- never create workspace without agency.json
- never create PR for empty diff (0 commits ahead)
- never start run if parent dirty or inside worktree
- never run scripts inside runner tmux pane

---

## 16.5) Error codes (public contract, v1)

### Core/CLI errors
- `E_USAGE` — invalid CLI usage (flags, arguments)
- `E_NOT_IMPLEMENTED` — command not yet implemented
- `E_INTERNAL` — unexpected internal error

### Repository errors
- `E_NO_REPO` — not inside a git repository
- `E_NO_AGENCY_JSON` — agency.json not found at repo root
- `E_INVALID_AGENCY_JSON` — agency.json validation failed
- `E_INVALID_USER_CONFIG` — user config missing or invalid
- `E_EDITOR_NOT_CONFIGURED` — editor command not found or not executable
- `E_AGENCY_JSON_EXISTS` — agency.json already exists (init without --force)

### Tool/prerequisite errors
- `E_GIT_NOT_INSTALLED` — git not found on PATH
- `E_TMUX_NOT_INSTALLED` — tmux not found on PATH
- `E_GH_NOT_INSTALLED` — gh CLI not found on PATH
- `E_GH_NOT_AUTHENTICATED` — gh not authenticated (run `gh auth login`)
- `E_RUNNER_NOT_CONFIGURED` — runner command not found

### Script errors
- `E_SCRIPT_NOT_FOUND` — required script not found
- `E_SCRIPT_NOT_EXECUTABLE` — script is not executable (suggests `chmod +x`)
- `E_SCRIPT_TIMEOUT` — script exceeded timeout
- `E_SCRIPT_FAILED` — script exited non-zero

### Persistence errors
- `E_PERSIST_FAILED` — failed to write persistence files
- `E_STORE_CORRUPT` — store file is corrupted or has invalid schema
- `E_REPO_ID_COLLISION` — repo_id hash collision (extremely rare)

### Run/workflow errors (slice 1+)
- `E_PARENT_DIRTY` — parent working tree is not clean
- `E_EMPTY_DIFF` — no commits ahead of parent branch
- `E_PARENT_NOT_FOUND` — parent branch ref not found locally or on origin
- `E_PR_NOT_MERGEABLE` — PR cannot be merged
- `E_GIT_PUSH_FAILED` — git push failed
- `E_GH_PR_CREATE_FAILED` — gh pr create failed
- `E_GH_PR_EDIT_FAILED` — gh pr edit failed
- `E_GH_PR_VIEW_FAILED` — gh pr view failed or returned invalid/unsupported schema
- `E_PR_NOT_OPEN` — PR exists but is not open (CLOSED or MERGED)
- `E_UNSUPPORTED_ORIGIN_HOST` — origin is not github.com
- `E_NO_ORIGIN` — origin remote not configured
- `E_DIRTY_WORKTREE` — run worktree has uncommitted changes
- `E_REPO_LOCKED` — another agency process holds the lock
- `E_RUN_NOT_FOUND` — specified run does not exist
- `E_RUN_ID_AMBIGUOUS` — run reference matches multiple runs
- `E_WORKTREE_MISSING` — run worktree path is missing on disk
- `E_NO_PR` — no PR exists for the run
- `E_REPORT_INVALID` — report missing/empty without `--force`
- `E_GIT_FETCH_FAILED` — git fetch failed
- `E_PR_MISMATCH` — resolved PR does not match expected branch
- `E_GH_REPO_PARSE_FAILED` — failed to parse owner/repo from origin
- `E_PR_MERGEABILITY_UNKNOWN` — gh reports mergeable as UNKNOWN after retries
- `E_GH_PR_MERGE_FAILED` — gh merge failed or merge state could not be confirmed
- `E_ARCHIVE_FAILED` — archive step failed
- `E_ABORTED` — user declined confirmation / wrong token
- `E_NOT_INTERACTIVE` — command requires an interactive TTY

### Error output format (v1)
- on non-zero exit, print `error_code: E_...` as the first line on stderr
- follow with a human-readable message on stderr
- optional: `hint:` line with actionable guidance

### Doctor output format (v1)
- stdout is `key: value` lines, no color
- includes keys in this exact order:
  1. `repo_root` — absolute path to repo root
  2. `agency_data_dir` — resolved data directory
  3. `agency_config_dir` — resolved config directory
  4. `user_config_path` — `${AGENCY_CONFIG_DIR}/config.json`
  5. `agency_cache_dir` — resolved cache directory
  6. `repo_key` — `github:<owner>/<repo>` or `path:<sha256>`
  7. `repo_id` — truncated sha256 of repo_key (16 hex chars)
  8. `origin_present` — `true` or `false`
  9. `origin_url` — remote URL or empty string
  10. `origin_host` — hostname or empty string
  11. `github_flow_available` — `true` or `false`
  12. `git_version` — output of `git --version`
  13. `tmux_version` — output of `tmux -V`
  14. `gh_version` — first line of `gh --version`
  15. `gh_authenticated` — `true` or `false`
  16. `defaults_parent_branch` — current branch at doctor time
  17. `defaults_runner` — from config.json (or built-in)
  18. `defaults_editor` — from config.json (or built-in)
  19. `runner_cmd` — resolved runner command
  20. `script_setup` — absolute path to setup script
  21. `script_verify` — absolute path to verify script
  22. `script_archive` — absolute path to archive script
  23. `status` — `ok` on success
- booleans are `true` or `false` (lowercase)
- on success: exits 0
- on failure: stdout is empty, error printed to stderr, exits non-zero

### Init output format (v1)
- stdout is `key: value` lines, no color
- includes keys:
  1. `repo_root` — absolute path to repo root
  2. `agency_json` — `created` or `overwritten`
  3. `scripts_created` — comma-separated list or `none`
  4. `gitignore` — `updated`, `already_present`, `created`, or `skipped`
- on `--no-gitignore`: prints `warning: gitignore_skipped`

---

## 17) Deferred (post-v1)

- interactive tui
- report_mode: repo_file (committed reports)
- manual status override (agency mark)
- parent-behind-origin gate
- runner pid inspection
- agency gc (automated cleanup)
- auto-update / self-update
- linux distro packages
