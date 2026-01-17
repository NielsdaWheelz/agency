# PR Report: User Config + Open Command

## Summary of Changes
- Added strict user-level config at `$AGENCY_CONFIG_DIR/config.json` with defaults for runner/editor and strict validation (no unknown keys).
- Made `agency.json` scripts-only and hard-rejected non-script keys.
- Implemented `agency open <run-id> [--editor <name>]` to open run worktrees in the configured editor.
- Updated `agency run`/`resume` defaults to come from user config; parent branch defaults to current branch; strict validation enforced.
- Added editor/runner command resolution with PATH lookup and executable checks; added `E_INVALID_USER_CONFIG` and `E_EDITOR_NOT_CONFIGURED`.
- Updated `agency doctor` to require user config, report config path, defaults, and resolved editor/runner.
- Updated `agency init` to create user config if missing and emit its path/state.
- Updated tests and scaffolding templates to align with scripts-only repo config.

## Problems Encountered
- Non-repo runs returned `E_PARENT_BRANCH_NOT_FOUND` instead of `E_NO_REPO` because parent branch resolution happened before repo detection in the deferred check path.
- Resume tests expected a runner command string (`claude`) but resolution now returns full PATH (`/usr/bin/claude`), causing assertion failures.

## Solutions Implemented
- Fixed repo detection by resolving repo root first in the deferred repo check path, restoring `E_NO_REPO` in non-repo contexts.
- Adjusted resume test assertions to accept resolved PATHs by comparing `filepath.Base(...)` to the expected command name.

## Decisions Made
- User config lives at `$AGENCY_CONFIG_DIR/config.json` and is required for `doctor`; built-in defaults are used only when the file is missing for other commands.
- Commands are resolved via PATH lookup unless an explicit path is provided; explicit paths must exist and be executable.
- `agency.json` is strictly scripts-only to keep repo config minimal and avoid user-specific values.

## Deviations from Prompt/Spec/Roadmap
- None. All changes align with the approved spec updates and user requests.

## How to Run New or Changed Commands
- Initialize repo + user config:
  - `agency init`
- Open a run worktree:
  - `agency open <run-id>`
  - `agency open <run-id> --editor <name>`

## How to Use/Check New or Changed Functionality
- Validate setup and config:
  - `agency doctor`
- Verify user config location and contents:
  - Check `user_config_path` in `agency init` or `agency doctor` output.
  - Edit `$AGENCY_CONFIG_DIR/config.json` to set `defaults.runner`, `defaults.editor`, `runners`, `editors`.
- Run tests:
  - `go test ./...`

## Branch Name
- `pr/open-user-config`

## Commit Message (Long, Detailed)
Add strict user config, open command, and scripts-only repo config

Introduce a user-scoped config at $AGENCY_CONFIG_DIR/config.json with strict validation,
defaults for runner/editor, and shared resolution helpers that enforce PATH lookups
or executable paths. Add the agency open command to launch a run worktree in the
configured editor and surface new error codes for invalid user config and missing
editor. Move repo config to scripts-only, reject non-script keys, and update
scaffolding templates accordingly. Wire user config defaults into run/resume and
resolve parent branches from the current branch when not provided. Require user
config for doctor, and extend doctor output to include config path, defaults, and
resolved runner/editor. Update init to create user config if missing and emit its
path/state. Fix deferred repo checks to return E_NO_REPO outside repos and adjust
tests for resolved runner PATHs. Update test fixtures and coverage to match the new
config shape and behavior.
