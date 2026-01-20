# agency

local-first runner manager: creates isolated git workspaces, launches `claude`/`codex` TUIs in tmux, opens GitHub PRs via `gh`.

## installation

### from source (development)

```bash
go install github.com/NielsdaWheelz/agency/cmd/agency@latest
```

Ensure your Go bin dir is on PATH (uses `GOBIN` if set, otherwise `GOPATH/bin`):

```bash
export PATH="$(go env GOPATH)/bin:$PATH"
```

For zsh, add this to `~/.zshrc` and restart your shell.


### from releases (coming soon)

prebuilt binaries available on [GitHub releases](https://github.com/NielsdaWheelz/agency/releases) for:
- darwin-amd64
- darwin-arm64
- linux-amd64

### homebrew (coming soon)

```bash
brew install NielsdaWheelz/tap/agency
```

## release (maintainers)

release builds are tag-driven via `.github/workflows/release.yml` and `.goreleaser.yaml`.

1) ensure main is green and committed
2) tag and push:

```bash
git tag -a v0.1.1 -m "v0.1.1"
git push origin v0.1.1
```

notes:
- `go install github.com/NielsdaWheelz/agency/cmd/agency@latest` installs the latest semver tag, not the GitHub release binary.
- `agency --version` will show `dev` for `go install` builds unless you pass ldflags or use release artifacts.

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
agency run --name feature-x   # creates workspace and enters tmux session
# Ctrl+b, d to detach from tmux when done working
agency push feature-x
agency merge feature-x
```

## complete guide

this section walks through agency from setup to merge.

### how agency works

agency creates isolated workspaces for AI coding sessions. each run gets:
- a git worktree (separate directory with its own branch)
- a tmux session running your AI runner (claude/codex)
- metadata tracking the run's lifecycle

```
YOUR REPO                           AGENCY DATA DIR
/projects/myapp/                    ~/Library/Application Support/agency/
├── agency.json                     └── repos/<repo_id>/
├── scripts/                            ├── runs/<run_id>/
│   ├── agency_setup.sh                 │   ├── meta.json
│   ├── agency_verify.sh                │   └── logs/
│   └── agency_archive.sh               └── worktrees/<run_id>/  ◄── ISOLATED
└── src/...                                 ├── .agency/report.md
                                            └── src/... (copy of your code)

LIFECYCLE:

  ┌──────────┐    ┌───────────┐    ┌──────────────┐
  │agency run│───►│agency push│───►│agency merge  │
  └────┬─────┘    └─────┬─────┘    └──────┬───────┘
       │                │                 │
       ▼                ▼                 ▼
   creates          pushes to        runs verify,
   worktree,        GitHub +         merges PR,
   runs setup,      creates PR       cleans up
   enters tmux

  DETACH FROM TMUX: press Ctrl+b, then d (session keeps running)
  RE-ATTACH: agency attach <name> or agency resume <name>
```

### step 1: initialize your repo

```bash
cd /path/to/your/repo
agency init
```

this creates:
```
your-repo/
├── agency.json                    # configuration
├── CLAUDE.md                      # runner protocol (status reporting)
└── scripts/
    ├── agency_setup.sh            # runs BEFORE ai starts (install deps)
    ├── agency_verify.sh           # runs to check work (tests/lint)
    └── agency_archive.sh          # runs on cleanup
```

### step 2: configure agency.json

the default `agency.json` works for most repos:

```json
{
  "version": 1,
  "scripts": {
    "setup": {
      "path": "scripts/agency_setup.sh",
      "timeout": "10m"
    },
    "verify": {
      "path": "scripts/agency_verify.sh",
      "timeout": "30m"
    },
    "archive": {
      "path": "scripts/agency_archive.sh",
      "timeout": "5m"
    }
  }
}
```

**customize timeouts:**
- `timeout` uses Go duration format: `10m`, `1h30m`, `90s`
- minimum: 1 minute, maximum: 24 hours
- increase `setup` timeout for repos with large dependencies
- increase `verify` timeout for extensive test suites

### step 3: configure scripts

the stub scripts created by `agency init` need to be customized for your project.

#### example: node.js project

**scripts/agency_setup.sh:**
```bash
#!/usr/bin/env bash
set -euo pipefail

# copy env files from parent repo to worktree
# AGENCY_REPO_ROOT = your original repo
# AGENCY_WORKSPACE_ROOT = the isolated worktree
if [ -f "$AGENCY_REPO_ROOT/.env" ]; then
  cp "$AGENCY_REPO_ROOT/.env" "$AGENCY_WORKSPACE_ROOT/.env"
fi
if [ -f "$AGENCY_REPO_ROOT/.env.local" ]; then
  cp "$AGENCY_REPO_ROOT/.env.local" "$AGENCY_WORKSPACE_ROOT/.env.local"
fi

# install dependencies
npm ci
```

**scripts/agency_verify.sh:**
```bash
#!/usr/bin/env bash
set -euo pipefail

# lint
npm run lint

# type check (if using typescript)
npm run typecheck 2>/dev/null || true

# run tests
npm test

# build (catches compile errors)
npm run build
```

**scripts/agency_archive.sh:**
```bash
#!/usr/bin/env bash
set -euo pipefail

# nothing needed for most node projects
# add cleanup if you start background processes in setup
exit 0
```

#### example: python project

**scripts/agency_setup.sh:**
```bash
#!/usr/bin/env bash
set -euo pipefail

# copy env files
if [ -f "$AGENCY_REPO_ROOT/.env" ]; then
  cp "$AGENCY_REPO_ROOT/.env" "$AGENCY_WORKSPACE_ROOT/.env"
fi

# create and activate virtual environment
python3 -m venv .venv
source .venv/bin/activate

# install dependencies
pip install -r requirements.txt
pip install -r requirements-dev.txt 2>/dev/null || true
```

**scripts/agency_verify.sh:**
```bash
#!/usr/bin/env bash
set -euo pipefail

# activate virtual environment
source .venv/bin/activate

# lint
ruff check . || flake8 .

# type check
mypy . 2>/dev/null || true

# run tests
pytest
```

**scripts/agency_archive.sh:**
```bash
#!/usr/bin/env bash
set -euo pipefail
exit 0
```

### step 4: verify setup

```bash
agency doctor
```

expected output:
```
repo_root: /path/to/your/repo
agency_data_dir: ~/Library/Application Support/agency
...
runner_cmd: claude
script_setup: /path/to/your/repo/scripts/agency_setup.sh
script_verify: /path/to/your/repo/scripts/agency_verify.sh
script_archive: /path/to/your/repo/scripts/agency_archive.sh
status: ok
```

if you see errors, fix them before continuing.

### step 5: start an ai coding session

```bash
agency run --name add-user-auth
```

**what just happened:**
1. agency verified your repo is clean (no uncommitted changes)
2. created a git worktree with a new branch `agency/add-user-auth-a3f2`
3. ran `scripts/agency_setup.sh` (installed deps, copied env files)
4. started a tmux session with claude running inside the worktree
5. attached you to the tmux session (you're now inside it!)

### step 6: work with the ai

you're now in a terminal with claude running. give it instructions:

```
> please implement JWT-based user authentication with login and logout endpoints
```

claude will write code, make commits, etc.

**to leave (but keep claude running):** press `Ctrl+b` then `d`

**other session commands:**
```bash
agency ls                              # list all runs
agency show feature-x                  # show run details (name or id)
agency stop feature-x                  # send Ctrl+C to claude
agency kill feature-x                  # kill tmux session (keeps files)
agency resume feature-x                # reattach (creates session if needed)
agency resume feature-x --restart      # restart with fresh claude session
```

### step 7: review the work

```bash
# see what claude did
agency show 20260115143022-a3f2

# open in your IDE (VS Code)
agency open 20260115143022-a3f2

# cd into the worktree
cd "$(agency path 20260115143022-a3f2)"
git log --oneline main..HEAD
git diff main
```

### step 8: push and create PR

```bash
agency push 20260115143022-a3f2
```

output:
```
pr: https://github.com/owner/repo/pull/123
```

**what just happened:**
1. pushed the branch to origin
2. created a GitHub PR with title `[agency] add user authentication`
3. synced `.agency/report.md` from worktree to PR body

you can now review the PR on GitHub, request changes, etc.

**if you make more changes and push again**, agency updates the existing PR.

### step 9: merge and cleanup

```bash
agency merge 20260115143022-a3f2
```

prompts:
```
verify failed. continue anyway? [y/N] y
confirm: type 'merge' to proceed: merge
```

output:
```
merged: 20260115143022-a3f2
pr: https://github.com/owner/repo/pull/123
log: /path/to/logs/archive.log
```

**what just happened:**
1. ran `scripts/agency_verify.sh` (tests, lint)
2. prompted for confirmation
3. merged the PR via `gh pr merge --squash --delete-branch`
4. deleted the remote branch
5. ran `scripts/agency_archive.sh`
6. killed the tmux session
7. deleted the worktree

### alternative: abandon a run

if the work isn't good and you want to discard it:

```bash
agency clean 20260115143022-a3f2
```

prompts:
```
confirm: type 'clean' to proceed: clean
```

this deletes the worktree and tmux session but does NOT merge anything.

### command reference card

All commands that accept `<ref>` support **name-based resolution**: you can use the run name (e.g., `feature-x`) or run_id (e.g., `20260115143022-a3f2`) interchangeably.

**global resolution**: most run-targeted commands now work from any directory. run_id and prefix resolution is always global; name resolution prefers the current repo if you're inside one, otherwise resolves globally.

```bash
# === SETUP ===
agency init                        # initialize repo for agency
agency init --repo /path/to/repo   # initialize a specific repo
agency doctor                      # check prerequisites
agency doctor --repo /path/to/repo # check a specific repo

# === LIFECYCLE ===
agency run --name my-feature       # start new AI session (attaches by default)
agency run --name feat --repo /p   # start in specific repo
agency attach <ref>                # enter tmux session (works from anywhere)
# Ctrl+b, d                        # detach from tmux
agency push <ref>                  # push branch + create/update PR
agency push <ref> --force-with-lease  # force push after rebase
agency merge <ref>                 # verify + merge + cleanup
agency clean <ref>                 # abandon (no merge)
agency resolve <ref>               # show conflict resolution guidance

# === OBSERVABILITY ===
agency ls                          # list runs (current repo or all)
agency ls --repo /path/to/repo     # list runs for specific repo
agency ls --all                    # include archived
agency show <ref>                  # show details (global)
agency show <ref> --path           # show paths only
agency path <ref>                  # output worktree path (for scripting)
agency open <ref>                  # open worktree in editor

# === SESSION CONTROL (global resolution) ===
agency resume <ref>                # attach (create session if needed)
agency resume <ref> --restart      # restart session (loses chat history)
agency stop <ref>                  # send Ctrl+C
agency kill <ref>                  # kill session (keeps files)

# === VERIFICATION ===
agency verify <ref>                # run verify script manually

# === DISAMBIGUATION ===
# use --repo when names conflict across repos:
agency attach my-feature --repo /path/to/repo
agency resume my-feature --repo /path/to/repo
```

### environment variables available in scripts

these are automatically set when agency runs your scripts:

| variable | description | example |
|----------|-------------|---------|
| `AGENCY_RUN_ID` | run identifier | `20260115143022-a3f2` |
| `AGENCY_NAME` | run name | `add-user-auth` |
| `AGENCY_REPO_ROOT` | original repo path | `/Users/you/myapp` |
| `AGENCY_WORKSPACE_ROOT` | worktree path | `/path/to/worktree` |
| `AGENCY_BRANCH` | worktree branch | `agency/add-user-auth-a3f2` |
| `AGENCY_PARENT_BRANCH` | parent branch | `main` |
| `AGENCY_ORIGIN_URL` | git remote URL | `git@github.com:you/myapp.git` |
| `AGENCY_RUNNER` | runner name | `claude` |
| `AGENCY_DOTAGENCY_DIR` | `.agency/` path | `/path/to/worktree/.agency` |
| `AGENCY_OUTPUT_DIR` | script output dir | `/path/to/worktree/.agency/out` |
| `AGENCY_LOG_DIR` | log directory | `/path/to/logs` |
| `CI` | always `1` | `1` |

### script timeouts

each script's timeout is configurable in `agency.json`. defaults:

| script | default timeout | purpose |
|--------|-----------------|---------|
| `setup` | 10 minutes | install deps, copy env files |
| `verify` | 30 minutes | run tests, lint, build |
| `archive` | 5 minutes | cleanup before deletion |

the `agency verify` command also accepts a `--timeout` flag to override the configured timeout.

## shell completion

agency supports tab completion for bash and zsh.

### bash completion

**option 1: with bash-completion package (recommended)**

```bash
agency completion bash > ~/.local/share/bash-completion/completions/agency
```

requires the `bash-completion` package to be installed. the directory is auto-sourced.

**option 2: manual sourcing**

```bash
agency completion bash > ~/.agency-completion.bash
echo 'source ~/.agency-completion.bash' >> ~/.bashrc
```

### zsh completion

**option 1: using fpath (recommended)**

```bash
mkdir -p ~/.zsh/completions
agency completion zsh > ~/.zsh/completions/_agency
```

ensure `~/.zsh/completions` is in your fpath (add to `.zshrc` before `compinit`):

```bash
fpath=(~/.zsh/completions $fpath)
autoload -Uz compinit && compinit
```

**option 2: manual sourcing**

```bash
agency completion zsh > ~/.agency-completion.zsh
echo 'source ~/.agency-completion.zsh' >> ~/.zshrc
```

### homebrew (macos)

if you installed via homebrew, completion scripts are installed automatically:
- zsh: `$(brew --prefix)/share/zsh/site-functions/_agency`
- bash: `$(brew --prefix)/share/bash-completion/completions/agency`

restart your shell after installation.

### what gets completed

- **commands**: `agency <TAB>` shows all subcommands
- **run references**: `agency show <TAB>` completes run names and ids
- **runners**: `agency run --runner <TAB>` completes runner names (claude, codex)
- **merge strategies**: `agency merge x --<TAB>` completes `--squash`, `--merge`, `--rebase`

## shell integration

add these functions to your `~/.bashrc` or `~/.zshrc` for fast worktree navigation:

```bash
# cd into a run's worktree
acd() { cd "$(agency path "$1")" || return 1; }

# pushd into a run's worktree (use popd to return)
apushd() { pushd "$(agency path "$1")" || return 1; }
```

usage:
```bash
acd my-feature          # cd into the worktree
git status              # run commands there
apushd my-feature       # pushd for stack-based navigation
popd                    # return to previous directory
```

## commands

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

### `agency init`

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

### `agency completion`

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

see [shell completion](#shell-completion) for full installation instructions.

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
agency push 20260110120000-a3f2           # push branch + create/update PR
agency push 20260110120000-a3f2 --force   # push with empty report (placeholder body)
agency push 20260110120000-a3f2 --allow-dirty # push with dirty worktree
agency push 20260110                       # unique prefix resolution
agency push my-feature --force-with-lease  # force push after rebase
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
agency merge <run_id> [--squash|--merge|--rebase] [--no-delete-branch] [--force]
```

**arguments:**
- `run_id`: the run identifier (e.g., `20260110120000-a3f2`)

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
merged: 20260110120000-a3f2
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
agency merge 20260110120000-a3f2                       # squash merge, delete branch (default)
agency merge 20260110120000-a3f2 --merge               # regular merge, delete branch
agency merge 20260110120000-a3f2 --rebase              # rebase merge, delete branch
agency merge 20260110120000-a3f2 --no-delete-branch    # squash merge, preserve branch
agency merge 20260110120000-a3f2 --force               # skip verify-fail prompt
```

### `agency resolve`

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
agency resolve 20260110120000-a3f2
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

## development

### build

```bash
go build -o agency ./cmd/agency
```

### format

```bash
make fmt
make fmt-check
```

### lint

```bash
make lint
```

### test

```bash
go test ./...
```

### gh e2e (optional)

```bash
make e2e
```

this creates, pushes, merges, and deletes a PR branch in the target repo.

### lefthook (optional)

```bash
go install github.com/evilmartians/lefthook@latest
lefthook install
```

pre-commit runs gofmt on staged Go files (re-stages) and `golangci-lint` on
changes vs `HEAD`. pre-push runs `scripts/agency_verify.sh`. e2e runs on pre-push
when `AGENCY_RUN_E2E=1` (requires `GH_TOKEN`).

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
│   ├── runnerstatus/     # runner status file parsing (S7) - .agency/state/runner_status.json
│   ├── runservice/       # concrete RunService implementation (wires all steps, setup execution)
│   ├── scaffold/         # agency.json template + stub script creation + CLAUDE.md
│   ├── status/           # pure status derivation from meta + runner status + stall detection
│   ├── store/            # repo_index.json + repo.json + run meta.json + run scanning
│   ├── tmux/             # tmux Client interface, exec-backed impl, session detection, scrollback capture, ANSI stripping
│   ├── tty/              # TTY detection helpers for interactive prompts
│   ├── verify/           # verify script execution engine + evidence recording
│   ├── verifyservice/    # verify pipeline entrypoint (S5) + meta/events integration
│   ├── version/          # build version
│   ├── watchdog/         # stall detection (S7) - detects inactive runners
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
- [slice 7 spec](docs/v1/s7/s7_spec.md) — runner status contract + watchdog slice detailed spec
- [slice 7 PRs](docs/v1/s7/s7_prs.md) — slice 7 PR breakdown

## license

MIT
