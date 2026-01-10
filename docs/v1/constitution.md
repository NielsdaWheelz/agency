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
6. Agency merges via `gh pr merge` and archives.

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

**runner**: `claude` or `codex` (must be on PATH, or specify command in agency.json).

---

## 4) Hard constraints

- implementation: **Go**
- github integration: **`gh` CLI** only
- attach/detach: **tmux** only
- isolation: **git worktrees**
- config: **`agency.json`** required at repo root
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
- configured runner (`claude` or `codex` on PATH, or custom command)
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

**config directory** (reserved, unused in v1):
1. if `$AGENCY_CONFIG_DIR` set: use it
2. else if macOS: `~/Library/Preferences/agency`
3. else if `$XDG_CONFIG_HOME` set: `$XDG_CONFIG_HOME/agency`
4. else: `~/.config/agency`

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
  "defaults": {
    "parent_branch": "main",
    "runner": "claude"
  },
  "scripts": {
    "setup": "scripts/agency_setup.sh",
    "verify": "scripts/agency_verify.sh",
    "archive": "scripts/agency_archive.sh"
  },
  "runners": {
    "claude": "claude",
    "codex": "codex"
  }
}
```

**required fields**:
- `defaults.parent_branch`
- `defaults.runner`
- `scripts.setup`, `scripts.verify`, `scripts.archive`

**validation (v1)**:
- `version` must be integer `1`
- `defaults.parent_branch` must be non-empty string
- `defaults.runner` must be `claude` or `codex`
- `scripts.setup|verify|archive` must be non-empty strings
- `runners` if present must be object of string -> string (values non-empty)
- unknown top-level keys are ignored
- runner commands must be a single executable name or path with no whitespace (no args); otherwise `E_INVALID_AGENCY_JSON`

**runner resolution**:
- if `runners.<name>` exists: use that command
- else if `defaults.runner` is `claude` or `codex`: assume on PATH
- else: error `E_RUNNER_NOT_CONFIGURED`

Invalid `runners.<name>` values (non-string or empty string) are configuration errors (`E_INVALID_AGENCY_JSON`).

**schema versioning (v1)** (applies to `agency.json`, `meta.json`, `events.jsonl`):
- additive only
- new required fields must bump version
- unknown fields are ignored

---

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
- `AGENCY_TITLE`
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

If present, agency uses `ok` field; if absent, uses exit code only.

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
      logs/               # script stdout/stderr
    worktrees/<run_id>/   # git worktree (deleted on archive)
```

`repo_id` = sha256(repo_key) truncated to 16 hex chars.

### Atomic write behavior (v1)

- JSON files are written via temp file + atomic rename.
- Do not write `repo_index.json` or `repo.json` unless `agency doctor` succeeds.
- Optional: fsync temp file and parent directory before rename (not required in v1).

### repo_index.json (public contract, v1)

```json
{
  "schema_version": "1.0",
  "repos": {
    "github:owner/repo": {
      "repo_id": "abcd1234ef567890",
      "paths": ["/abs/path"],
      "last_seen_at": "2025-01-09T12:34:56Z"
    }
  }
}
```

`repos` is keyed by `repo_key`; `repo_id` is stored inside each entry.

### repo.json (public contract, v1)

- `schema_version: "1.0"`
- `repo_id`
- `repo_key`
- `origin_present` (bool)
- `origin_url` (string or empty if no origin)
- `origin_host` (string or empty if none)
- `repo_root_last_seen` (absolute path)
- `agency_json_path` (absolute path)
- `capabilities`:
  - `github_origin` (bool)
  - `origin_host` (string or empty)
  - `gh_authed` (bool)
- `created_at`
- `updated_at`

### meta.json (public contract, v1)

Required fields:
- `schema_version`
- `run_id`
- `repo_id`
- `title`
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
- `active` — tmux session `agency:<run_id>` exists
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

### Runner detection (v1)

`active` = tmux session exists. no pid inspection in v1.

---

## 12) Git + GitHub

**repo discovery**: `git rev-parse --show-toplevel` from cwd.

**branch naming**: `agency/<slug>-<shortid>`
- slug: sanitized title (lowercase, hyphens, max 30 chars)
- shortid: first 4 chars of run_id

**parent branch**: defaults to `agency.json defaults.parent_branch`. override with `--parent <branch>`.

**clean working tree**: `agency run` requires:
- cwd not inside existing agency worktree
- repo checkout has clean `git status --porcelain`

**remote requirement (v1)**: `origin` must exist and point to `github.com` (ssh or https) for `agency push`/`agency merge`. `repo_key` may still fall back to a path-based key for indexing, but GitHub PR flows require the GitHub origin. if hostname != `github.com`: `E_UNSUPPORTED_ORIGIN_HOST`.

**command cwd**: all git/gh operations run with `-C <worktree_path>` (or `cwd=worktree`) except the parent working tree cleanliness check, which runs in the repo root.

### Push behavior

`agency push <id>`:
1. `git fetch <origin>` — ensures remote refs exist; does NOT rebase, reset, or modify local branches
2. check commits ahead: `git rev-list --count <parent_branch>..<workspace_branch> > 0`
3. `git push -u origin <workspace_branch>`
4. if no PR exists and commits ahead > 0: create PR via `gh pr create`
5. PR identity: repo + head branch in origin (`gh pr view --head <workspace_branch>`)
6. on update, prefer stored PR number; fallback to `--head`
7. if `.agency/report.md` exists and non-empty: sync to PR body
8. store PR url/number in metadata

### Merge behavior

1. require existing PR; if missing: `E_NO_PR` with guidance to run `agency push <id>`
2. check `gh pr view --json mergeable`
3. run `scripts.verify`, record result
4. if verify failed: prompt "continue anyway?" (skip with `--force`)
5. prompt for human confirmation
6. `gh pr merge`
7. archive workspace

if not mergeable: `E_PR_NOT_MERGEABLE`. no auto-rebase.

---

## 13) Report

Lives at `<worktree>/.agency/report.md`.

Created on `agency run` with a template; title prefilled if provided.

Template:

```markdown
# <title>

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

**push validation**: `agency push` warns if `.agency/report.md` is missing or effectively empty. use `--force` to push anyway.

---

## 14) Commands

```
agency init                       create agency.json template
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

### Init semantics

`agency init` writes the template and appends `.agency/` to the repo `.gitignore` by default. use `--no-gitignore` for a non-invasive mode.
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

`agency resume <id>`:
1. if tmux session exists: attach unless `--detached`
2. if tmux session missing: create `agency:<run_id>` with `cwd=worktree`, run runner, then attach unless `--detached`

`agency resume <id> --restart`:
1. kill session (if exists)
2. recreate session and run runner

no idle detection in v1; tmux session existence is the only signal.

### Stop semantics

`agency stop <id>`:
1. `tmux send-keys -t agency:<run_id> C-c` (best-effort interrupt)
2. sets `needs_attention` flag regardless of whether interrupt succeeded
3. tmux session stays alive; use `agency resume --restart` to guarantee a fresh runner

stop is best-effort: C-c may cancel an in-tool operation, exit the tool, or do nothing. it may not interrupt model work and can leave the tool in an inconsistent state; v1 accepts this risk.

`agency kill <id>`:
- `tmux kill-session -t agency:<run_id>`
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
- read-only commands (`ls`, `show`, `attach`, `resume` without `--restart`, `doctor`) do not take the lock

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

- `E_NO_REPO`
- `E_NO_AGENCY_JSON`
- `E_INVALID_AGENCY_JSON`
- `E_AGENCY_JSON_EXISTS`
- `E_GIT_NOT_INSTALLED`
- `E_RUNNER_NOT_CONFIGURED`
- `E_GH_NOT_AUTHENTICATED`
- `E_GH_NOT_INSTALLED`
- `E_TMUX_NOT_INSTALLED`
- `E_PARENT_DIRTY`
- `E_EMPTY_DIFF`
- `E_PR_NOT_MERGEABLE`
- `E_UNSUPPORTED_ORIGIN_HOST`
- `E_REPO_LOCKED`
- `E_RUN_NOT_FOUND`
- `E_NO_PR`
- `E_SCRIPT_NOT_FOUND`
- `E_SCRIPT_NOT_EXECUTABLE`
- `E_SCRIPT_TIMEOUT`
- `E_SCRIPT_FAILED`
- `E_REPO_ID_COLLISION`
- `E_PERSIST_FAILED`
- `E_INTERNAL`

error output (v1):
- on non-zero exit, print `error_code: E_...` as the first line on stderr
- follow with a human-readable message on stderr

doctor output (v1):
- stdout is `key: value` lines, no color
- includes (in this order): `repo_root`, `agency_data_dir`, `agency_config_dir`, `agency_cache_dir`,
  `repo_key`, `repo_id`, `origin_present`, `origin_url`, `origin_host`, `github_flow_available`,
  `git_version`, `tmux_version`, `gh_version`, `gh_authenticated`,
  `defaults_parent_branch`, `defaults_runner`, `runner_cmd`,
  `script_setup`, `script_verify`, `script_archive`,
  `status`
- on success: `status: ok`

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
