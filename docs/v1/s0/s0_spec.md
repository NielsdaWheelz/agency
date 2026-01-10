# agency l1 / slice 00: bootstrap (init + doctor + config validation)

## goal
enable deterministic repo onboarding and prerequisite verification: a user can run `agency init` to scaffold config and `agency doctor` to validate everything required for v1.

## scope
- repo discovery via `git rev-parse --show-toplevel`
- directory resolution (see constitution)
- optionally parse `origin` and report "GitHub flow available: yes/no"
- repo identity parsing:
  - github.com ssh/https -> `github:<owner>/<repo>`
  - otherwise -> `path:<sha256(abs_path)>`
- `agency.json` presence + strict validation (schema v1)
- prerequisite checks: `git`, `tmux`, `gh`, runner command (strict readiness for slice 1+)
- `gh auth status` check
- global repo index + per-repo repo.json persistence (even for non-GitHub repos)
- error codes + actionable messages

## non-scope
- creating worktrees
- tmux sessions
- running scripts
- creating runs (meta.json, events.jsonl)
- any git push/pr/merge behavior

## public surface area
new commands:
- `agency init [--no-gitignore] [--force]`
- `agency doctor`

new files (global):
- `${AGENCY_DATA_DIR}/repo_index.json`
- `${AGENCY_DATA_DIR}/repos/<repo_id>/repo.json`

repo-local modifications:
- `agency init` creates `agency.json` in repo root
- `agency init` creates stub scripts if missing (executable):
  - `scripts/agency_setup.sh`
  - `scripts/agency_verify.sh`
  - `scripts/agency_archive.sh`
- `agency init` appends `.agency/` to `.gitignore` by default (use `--no-gitignore` to skip)

## commands + flags

### `agency init`
flags:
- `--no-gitignore` (bool): do not modify `.gitignore`
- `--force` (bool): overwrite existing `agency.json` if present

behavior:
- finds repo root; errors `E_NO_REPO` if not in a git repo
- if `agency.json` exists and `--force` not set: error `E_AGENCY_JSON_EXISTS` with message “agency.json already exists; use --force to overwrite”
- writes template `agency.json` (version 1) exactly as in l0 (defaults + scripts + runners) via atomic write
- writes stub scripts if missing (executable); never overwrite:
  - `scripts/agency_setup.sh`
  - `scripts/agency_verify.sh`
  - `scripts/agency_archive.sh`
- ensures `scripts/` directory exists
- if scripts exist: leave untouched
- appends `.agency/` to repo `.gitignore` unless `--no-gitignore`
- if `.gitignore` missing: create it
- if `.agency/` already present: no-op
- when appending: ensure file ends with a newline
- if `--no-gitignore`, user must ensure `.agency/` is ignored in parent checkout and worktrees
- does NOT require a clean working tree

stub script contents (v1):
- path normalization: always under repo root (no absolute paths)
- file mode: 0755
- setup/archive:
  - `#!/usr/bin/env bash`
  - `set -euo pipefail`
  - comment indicating it is a stub
  - `exit 0`
- verify:
  - `#!/usr/bin/env bash`
  - `set -euo pipefail`
  - comment indicating it is a stub and must be replaced
  - `echo "replace scripts/agency_verify.sh"`
  - `exit 1`

### `agency doctor`
flags: none (v1)

behavior:
- finds repo root; errors `E_NO_REPO` if not in a git repo
- requires `agency.json` exists; else `E_NO_AGENCY_JSON`
- validates `agency.json`; else `E_INVALID_AGENCY_JSON` (print first validation error)
- strict readiness check for slice 1+ (see constitution for success criteria)
- resolves dirs and prints:
  - repo root
  - `AGENCY_DATA_DIR`
  - config dir
  - cache dir
  - derived repo_key + repo_id
  - github flow available: yes/no (based on `origin` host)
  - origin_present, origin_url, origin_host
- checks required tools exist and run:
  - `git --version` else `E_GIT_NOT_INSTALLED`
  - `tmux -V` else `E_TMUX_NOT_INSTALLED`
  - `gh --version` else `E_GH_NOT_INSTALLED`
- record tool versions in doctor output
- checks `gh auth status` succeeds; else `E_GH_NOT_AUTHENTICATED`
- checks runner command resolution:
  - resolve command for defaults.runner using `agency.json.runners` if present, else fallback to `claude|codex` on PATH
  - if not found: `E_RUNNER_NOT_CONFIGURED`
- checks scripts exist + executable:
  - resolve `scripts.setup|verify|archive` relative to repo root if not absolute
  - if missing: `E_SCRIPT_NOT_FOUND`
  - if not executable: `E_SCRIPT_NOT_EXECUTABLE`
    - message suggests `chmod +x <script>`
- writes/updates repo_index.json and repo.json only on success
- on success: exit 0

origin handling:
- if remote `origin` missing OR `remote.origin.url` is empty: treat as `origin_present: false`, `origin_url: ""`
- repo_key falls back to `path:<sha256(abs_path)>` in these cases
- do NOT require github.com origin in slice 0 (doctor still works with fallback repo_key). missing origin is not an error; github.com origin is required later for push/merge.

output format (doctor): see constitution
error output (doctor): see constitution
directory resolution: see constitution
repo_id derivation and collision handling: see constitution

## files created/modified

Schemas and templates are defined in the constitution:
- `agency.json` template + validation rules
- `${AGENCY_DATA_DIR}/repo_index.json`
- `${AGENCY_DATA_DIR}/repos/<repo_id>/repo.json`

## error codes
See constitution; slice 0 uses the shared taxonomy.


## behaviors (given/when/then)

1) init writes config
- given: inside a git repo with no agency.json
- when: `agency init`
- then: repo root contains `agency.json`, stub scripts exist, and `.agency/` is ignored via `.gitignore`

2) init refuses overwrite
- given: agency.json exists
- when: `agency init`
- then: exits non-zero with `E_AGENCY_JSON_EXISTS` and suggests `--force`

3) doctor fails missing gh auth
- given: gh installed but not authenticated
- when: `agency doctor`
- then: exits non-zero with `E_GH_NOT_AUTHENTICATED` and instructs `gh auth login`

4) doctor fails invalid agency.json
- given: agency.json missing required field
- when: `agency doctor`
- then: exits non-zero with `E_INVALID_AGENCY_JSON` and prints the missing field

5) doctor succeeds
- given: git/tmux/gh present, gh authenticated, runner command resolves, scripts exist + executable
- when: `agency doctor`
- then: exits 0 and prints resolved paths + repo identity

6) doctor succeeds without github origin
- given: origin missing or non-github host, but tools/auth/scripts are OK
- when: `agency doctor`
- then: exits 0, reports `github_flow_available: no`, and uses path-based repo_key

## persistence
- repo_index.json and repo.json are written only on successful doctor.
- directory creation: `${AGENCY_DATA_DIR}` may be created even on failure; JSON files are not written unless success.
- init writes `agency.json` and stub scripts (if missing) in repo root.
- no run data is created.

atomic write behavior:
- write JSON via temp file + atomic rename
- do not leave partial files on failure
- optional fsync of temp file and parent dir (not required in v1)

## tests

manual:
- run `agency init` in a fresh repo, verify files created
- run `agency doctor` while unauthenticated to gh
- run `agency doctor` with missing/non-executable scripts

automated (go):
- unit tests for:
  - directory resolution precedence (env override, macOS defaults, XDG fallback)
  - origin url parsing → repo_key (ssh, https, non-github)
  - repo_id derivation from repo_key
  - agency.json validation (missing fields, wrong types, wrong version)
  - doctor checks via stubbed command runner (tool presence, version checks, gh auth)

## guardrails
- do not implement worktrees, tmux session creation, scripts execution, or any run metadata files
- do not introduce planner/council/headless features
- do not add network calls beyond `gh auth status`
- keep output stable and parseable (no emoji, no ascii art)
- all external command invocations go through a small runner interface so tests can stub them

## rollout notes
none (local tool)
