# getting started

this guide walks through agency from setup to merge.

## how agency works

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

## step 1: initialize your repo

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

## step 2: configure agency.json

the default `agency.json` works for most repos. see [configuration](configuration.md) for details.

## step 3: configure scripts

the stub scripts created by `agency init` need to be customized for your project.

### example: node.js project

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

### example: python project

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

## step 4: verify setup

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

## step 5: commit agency files

agency creates git worktrees from your current branch. worktrees only contain committed files, so you must commit the agency files before starting a run:

```bash
git add agency.json CLAUDE.md scripts/
git commit -m "add agency configuration"
```

if you skip this step, `agency run` will fail because `agency.json` won't exist in the worktree.

## step 6: start an ai coding session

```bash
agency run --name add-user-auth
```

**what just happened:**
1. agency verified your repo is clean (no uncommitted changes)
2. created a git worktree with a new branch `agency/add-user-auth-a3f2`
3. ran `scripts/agency_setup.sh` (installed deps, copied env files)
4. started a tmux session with claude running inside the worktree
5. attached you to the tmux session (you're now inside it!)

## step 7: work with the ai

you're now in a terminal with claude running. give it instructions:

```
> please implement JWT-based user authentication with login and logout endpoints
```

claude will write code, make commits, etc.

**to leave (but keep claude running):** press `Ctrl+b` then `d`

**other session commands:**
```bash
agency ls                              # list all runs
agency show add-user-auth              # show run details
agency stop add-user-auth              # send Ctrl+C to claude
agency kill add-user-auth              # kill tmux session (keeps files)
agency resume add-user-auth            # reattach (creates session if needed)
agency resume add-user-auth --restart  # restart with fresh claude session
```

## step 8: review the work

```bash
# see what claude did
agency show add-user-auth

# open in your IDE (VS Code)
agency open add-user-auth

# cd into the worktree
cd "$(agency path add-user-auth)"
git log --oneline main..HEAD
git diff main
```

## step 9: push and create PR

```bash
agency push add-user-auth
```

output:
```
pr: https://github.com/owner/repo/pull/123
```

**what just happened:**
1. pushed the branch to origin
2. created a GitHub PR with title `[agency] add-user-auth`
3. synced `.agency/report.md` from worktree to PR body

you can now review the PR on GitHub, request changes, etc.

**if you make more changes and push again**, agency updates the existing PR.

## step 10: merge and cleanup

```bash
agency merge add-user-auth
```

prompts:
```
verify failed. continue anyway? [y/N] y
confirm: type 'merge' to proceed: merge
```

output:
```
merged: add-user-auth
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

## alternative: abandon a run

if the work isn't good and you want to discard it:

```bash
agency clean add-user-auth
```

prompts:
```
confirm: type 'clean' to proceed: clean
```

this deletes the worktree and tmux session but does NOT merge anything.

## command quick reference

all commands that accept `<ref>` support **name-based resolution**: you can use the run name (e.g., `feature-x`) or run_id (e.g., `20260115143022-a3f2`) interchangeably.

```bash
# === SETUP ===
agency init                        # initialize repo for agency
agency doctor                      # check prerequisites

# === LIFECYCLE ===
agency run --name my-feature       # start new AI session
agency attach <ref>                # enter tmux session
agency push <ref>                  # push branch + create/update PR
agency merge <ref>                 # verify + merge + cleanup
agency clean <ref>                 # abandon (no merge)

# === OBSERVABILITY ===
agency ls                          # list runs
agency show <ref>                  # show details
agency path <ref>                  # output worktree path
agency open <ref>                  # open worktree in editor

# === SESSION CONTROL ===
agency resume <ref>                # attach (create session if needed)
agency stop <ref>                  # send Ctrl+C
agency kill <ref>                  # kill session (keeps files)

# === VERIFICATION ===
agency verify <ref>                # run verify script manually
```

see [CLI reference](cli.md) for complete command documentation.
