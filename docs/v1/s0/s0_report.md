# Slice 00: Bootstrap (Init + Doctor + Config Validation)

**Status**: Complete

## Overview

Slice 00 enables deterministic repo onboarding and prerequisite verification. Users can run `agency init` to scaffold configuration and `agency doctor` to validate everything required for subsequent slices.

## Commands

### `agency init [--no-gitignore] [--force]`

Creates `agency.json` template and stub scripts in the current git repository.

**Flags:**
- `--no-gitignore` — do not modify `.gitignore`
- `--force` — overwrite existing `agency.json` (scripts are never overwritten)

**Files created:**
- `agency.json` — configuration file (see [constitution § agency.json](../constitution.md#8-agencyjson))
- `scripts/agency_setup.sh` — stub setup script (exits 0)
- `scripts/agency_verify.sh` — stub verify script (exits 1, must be replaced)
- `scripts/agency_archive.sh` — stub archive script (exits 0)
- `.gitignore` entry for `.agency/` (unless `--no-gitignore`)

**Output (stable key: value format):**
```
repo_root: /path/to/repo
agency_json: created
scripts_created: scripts/agency_setup.sh, scripts/agency_verify.sh, scripts/agency_archive.sh
gitignore: updated
```

**Error codes:**
- `E_NO_REPO` — not inside a git repository
- `E_AGENCY_JSON_EXISTS` — agency.json exists without `--force`

### `agency doctor`

Verifies all prerequisites are met for running agency commands.

**Checks:**
1. Repo root discovery via `git rev-parse --show-toplevel`
2. `agency.json` exists and is valid
3. Required tools installed: `git`, `tmux`, `gh`
4. `gh` is authenticated (`gh auth status`)
5. Runner command exists (e.g., `claude` or `codex` on PATH)
6. Scripts exist and are executable

**On success:**
- Writes/updates `${AGENCY_DATA_DIR}/repo_index.json`
- Writes/updates `${AGENCY_DATA_DIR}/repos/<repo_id>/repo.json`
- Exits 0

**Output (stable key: value format):**
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

**Failure output contract:**
- On any failure: stdout is empty
- stderr prints `error_code: E_...` then a message line
- Exit code is non-zero

**Error codes:**
- `E_NO_REPO` — not inside a git repository
- `E_NO_AGENCY_JSON` — agency.json not found
- `E_INVALID_AGENCY_JSON` — agency.json validation failed
- `E_GIT_NOT_INSTALLED` — git not found
- `E_TMUX_NOT_INSTALLED` — tmux not found
- `E_GH_NOT_INSTALLED` — gh CLI not found
- `E_GH_NOT_AUTHENTICATED` — gh not authenticated
- `E_RUNNER_NOT_CONFIGURED` — runner command not found
- `E_SCRIPT_NOT_FOUND` — required script not found
- `E_SCRIPT_NOT_EXECUTABLE` — script is not executable
- `E_PERSIST_FAILED` — failed to write persistence files

## Persistence

### repo_index.json

Location: `${AGENCY_DATA_DIR}/repo_index.json`

Maps repository keys to their metadata. Written only on successful `agency doctor`.

```json
{
  "schema_version": "1.0",
  "repos": {
    "github:owner/repo": {
      "repo_id": "abcd1234ef567890",
      "paths": ["/abs/path/to/repo"],
      "last_seen_at": "2025-01-09T12:34:56Z"
    },
    "path:a1b2c3d4e5f67890": {
      "repo_id": "a1b2c3d4e5f67890",
      "paths": ["/abs/path/to/local-only-repo"],
      "last_seen_at": "2025-01-09T12:35:00Z"
    }
  }
}
```

### repo.json

Location: `${AGENCY_DATA_DIR}/repos/<repo_id>/repo.json`

Per-repository metadata. Written only on successful `agency doctor`.

```json
{
  "schema_version": "1.0",
  "repo_key": "github:owner/repo",
  "repo_id": "abcd1234ef567890",
  "repo_root_last_seen": "/abs/path/to/repo",
  "agency_json_path": "/abs/path/to/repo/agency.json",
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

## Scope Boundaries

### In scope
- Repo discovery via `git rev-parse --show-toplevel`
- Directory resolution (data, config, cache)
- Origin parsing → `github:<owner>/<repo>` or `path:<sha256>`
- `agency.json` presence + strict validation (schema v1)
- Prerequisite checks: `git`, `tmux`, `gh`, runner command
- `gh auth status` check
- Global repo index + per-repo repo.json persistence

### Out of scope (deferred to later slices)
- Creating worktrees
- tmux sessions
- Running scripts
- Creating runs (meta.json, events.jsonl)
- Any git push/pr/merge behavior

## Demo

```bash
cd myrepo
agency init       # creates agency.json + stub scripts + .gitignore entry
agency doctor     # verifies prerequisites, writes persistence files
```

## References

- [Constitution](../constitution.md) — full v1 specification
- [Slice Roadmap](../slice_roadmap.md) — implementation plan
- [Slice 0 Spec](../s0/s0_spec.md) — detailed spec
- [Slice 0 PRs](../s0/s0_prs.md) — PR breakdown
