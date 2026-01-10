# agency

local-first runner manager: creates isolated git workspaces, launches `claude`/`codex` TUIs in tmux, opens GitHub PRs via `gh`.

## status

**v1 in development** — slice 0 (bootstrap) complete, slice 1 in progress.

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

next: slice 1 PR-02 (repo detection + gates)

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
│   ├── commands/         # command implementations (init, doctor, etc.)
│   ├── config/           # agency.json loading + validation
│   ├── core/             # run id generation, slugify, branch naming, shell escaping
│   ├── errors/           # stable error codes + AgencyError type
│   ├── exec/             # CommandRunner interface + RunScript with timeout
│   ├── fs/               # FS interface + atomic write + WriteJSONAtomic
│   ├── git/              # repo discovery + origin info
│   ├── identity/         # repo_key + repo_id derivation
│   ├── paths/            # XDG directory resolution
│   ├── scaffold/         # agency.json template + stub script creation
│   ├── store/            # repo_index.json + repo.json persistence
│   └── version/          # build version
└── docs/                 # specifications
```

## documentation

- [constitution](docs/v1/constitution.md) — full v1 specification
- [slice roadmap](docs/v1/slice_roadmap.md) — implementation plan
- [slice 0 spec](docs/v1/s0/s0_spec.md) — bootstrap slice detailed spec
- [slice 0 PRs](docs/v1/s0/s0_prs.md) — slice 0 PR breakdown
- [slice 1 spec](docs/v1/s1/s1_spec.md) — run workspace slice detailed spec
- [slice 1 PRs](docs/v1/s1/s1_prs.md) — slice 1 PR breakdown

## license

MIT
