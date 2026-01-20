# configuration

## agency.json

the `agency.json` file configures agency for a repository. it is created by `agency init`.

### schema

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
  },
  "defaults": {
    "runner": "claude",
    "parent_branch": "main"
  }
}
```

### fields

| field | required | default | description |
|-------|----------|---------|-------------|
| `version` | yes | - | schema version, must be `1` |
| `scripts.setup.path` | yes | - | path to setup script (relative to repo root) |
| `scripts.setup.timeout` | no | `10m` | setup script timeout |
| `scripts.verify.path` | yes | - | path to verify script |
| `scripts.verify.timeout` | no | `30m` | verify script timeout |
| `scripts.archive.path` | yes | - | path to archive script |
| `scripts.archive.timeout` | no | `5m` | archive script timeout |
| `defaults.runner` | no | `claude` | default runner (`claude` or `codex`) |
| `defaults.parent_branch` | no | `main` | default branch to branch from |

### timeout format

timeouts use Go duration format:

| format | meaning |
|--------|---------|
| `10m` | 10 minutes |
| `1h30m` | 1 hour 30 minutes |
| `90s` | 90 seconds |
| `2h` | 2 hours |

constraints:
- minimum: 1 minute
- maximum: 24 hours

### script defaults

| script | default timeout | purpose |
|--------|-----------------|---------|
| `setup` | 10 minutes | install dependencies, copy env files |
| `verify` | 30 minutes | run tests, lint, build |
| `archive` | 5 minutes | cleanup before worktree deletion |

## environment variables

these environment variables are automatically set when agency runs your scripts:

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

### usage in scripts

```bash
#!/usr/bin/env bash
set -euo pipefail

# copy env files from parent repo
if [ -f "$AGENCY_REPO_ROOT/.env" ]; then
  cp "$AGENCY_REPO_ROOT/.env" "$AGENCY_WORKSPACE_ROOT/.env"
fi

# write output for agency to read
echo '{"ok": true}' > "$AGENCY_OUTPUT_DIR/verify.json"
```

## shell completion

agency supports tab completion for bash and zsh.

**homebrew users:** completions are installed automatically. no manual configuration needed.

### manual installation (non-homebrew)

generate completion scripts using:

```bash
agency completion bash
agency completion zsh
```

#### bash

```bash
# option 1: bash-completion directory (if bash-completion is installed)
agency completion bash > ~/.local/share/bash-completion/completions/agency

# option 2: manual sourcing
agency completion bash > ~/.agency-completion.bash
echo 'source ~/.agency-completion.bash' >> ~/.bashrc
```

#### zsh

```bash
# option 1: fpath (recommended)
mkdir -p ~/.zsh/completions
agency completion zsh > ~/.zsh/completions/_agency
```

add to `~/.zshrc` before `compinit`:

```bash
fpath=(~/.zsh/completions $fpath)
autoload -Uz compinit && compinit
```

```bash
# option 2: manual sourcing
agency completion zsh > ~/.agency-completion.zsh
echo 'source ~/.agency-completion.zsh' >> ~/.zshrc
```

restart your shell after configuration.

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

## data directories

agency stores data in platform-appropriate locations:

| platform | data directory |
|----------|----------------|
| macOS | `~/Library/Application Support/agency` |
| Linux | `~/.local/share/agency` (XDG_DATA_HOME) |

### directory structure

```
${AGENCY_DATA_DIR}/
├── repo_index.json              # index of all registered repos
└── repos/
    └── <repo_id>/
        ├── repo.json            # repo metadata
        ├── runs/
        │   └── <run_id>/
        │       ├── meta.json    # run metadata
        │       ├── events.jsonl # event log
        │       ├── verify_record.json
        │       ├── transcript.txt
        │       └── logs/
        │           ├── setup.log
        │           ├── verify.log
        │           └── archive.log
        └── worktrees/
            └── <run_id>/        # git worktree
                ├── .agency/
                │   ├── report.md
                │   ├── INSTRUCTIONS.md
                │   ├── out/
                │   ├── tmp/
                │   └── state/
                └── <repo files>
```
