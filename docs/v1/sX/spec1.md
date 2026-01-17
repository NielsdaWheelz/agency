# agency l1 / slice X1: user config + open

## goal
add a user-level configuration file for defaults/runners/editors and introduce `agency open` to open a run worktree in the user's editor with global run resolution.

## scope
- user config file: `$AGENCY_CONFIG_DIR/config.json` (versioned, strict validation)
- repo config (`agency.json`) remains required but contains scripts only
- precedence:
  - runner/editor: cli flags -> user config -> built-in defaults (no env overrides in v1)
  - parent branch: `--parent` or current branch at run time (not stored in any config)
- new command: `agency open <run-id> [--editor <name>]`
- `agency doctor` fails if user config is missing or invalid
- new error codes: `E_INVALID_USER_CONFIG`, `E_EDITOR_NOT_CONFIGURED`

## non-scope
- env overrides (AGENCY_* or XDG) beyond existing directory resolution
- editor flags/args (single executable only)
- `agency config set/get` command
- backwards compatibility for old `agency.json` layouts
- windows-specific editor behaviors

## public surface area
new command:
- `agency open <run-id> [--editor <name>]`

new global file:
- `$AGENCY_CONFIG_DIR/config.json` (user config)

updated contracts:
- `agency.json` schema now only allows `version` + `scripts`
- `agency doctor` output includes user config path and defaults (parent/runner/editor)
- new error codes `E_INVALID_USER_CONFIG`, `E_EDITOR_NOT_CONFIGURED`

## definitions
- **user config**: per-user, machine-local settings (defaults, runners, editors).
- **repo config**: per-repo `agency.json` that specifies scripts only.
- **defaults**: baseline values for runner/editor when CLI flags are not provided.
- **editor**: executable used to open a run worktree.
- **runner**: executable used for `agency run`/`resume` sessions.
- **parent branch**: branch used for run creation; resolved at run time.

## config model

### user config (`$AGENCY_CONFIG_DIR/config.json`)
schema (v1):
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
    "code": "code",
    "zed": "zed"
  }
}
```

validation (strict, v1):
- `version` must be integer `1`
- `defaults.runner` and `defaults.editor` must be non-empty strings
- `runners` if present: object of string -> string (non-empty values, no whitespace)
- `editors` if present: object of string -> string (non-empty values, no whitespace)
- unknown top-level keys are invalid

resolution:
- if `runners.<name>` exists: use that command
- else if `defaults.runner` is `claude` or `codex`: assume on PATH
- else: `E_RUNNER_NOT_CONFIGURED`
- if `editors.<name>` exists: use that command
- else: use `defaults.editor` as command on PATH

missing user config:
- for most commands: treat as absent and use built-in defaults
- for `agency doctor`: missing file is an error (`E_INVALID_USER_CONFIG`)

invalid user config:
- `E_INVALID_USER_CONFIG` for all commands (no fallback)

built-in defaults (when user config is missing):
- `defaults.runner = "claude"`
- `defaults.editor = "code"`

### repo config (`agency.json`)
schema (v1):
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

validation (strict, v1):
- `version` must be integer `1`
- `scripts.setup|verify|archive` must be non-empty strings
- any other top-level key is invalid (including `defaults` and `runners`)

## precedence + resolution

user config is loaded and validated at command start.
- missing config: use built-in defaults (except `agency doctor`, which fails)
- invalid config: `E_INVALID_USER_CONFIG` for all commands

runner resolution:
- `agency run`: `--runner` if provided; else `defaults.runner`
- `agency resume`: uses `meta.runner`/`meta.runner_cmd` from run creation
- command resolution:
  - if command contains a path separator or starts with `.`: resolve relative to `$AGENCY_CONFIG_DIR` and require it to exist + be executable
  - otherwise: resolve via PATH
- if resolution fails: `E_RUNNER_NOT_CONFIGURED`

editor resolution:
- `agency open`: `--editor` if provided; else `defaults.editor`
- command resolution rules match runner resolution
- if resolution fails: `E_EDITOR_NOT_CONFIGURED`

parent branch resolution (run creation only):
- if `--parent` provided: use it
- else use current branch (`git branch --show-current`)
- if current branch is empty (detached HEAD): `E_PARENT_BRANCH_NOT_FOUND`

## commands + flags

### `agency open`
usage:
```
agency open <run-id> [--editor <name>]
```

behavior:
- resolves run id globally (exact match or unique prefix)
- fails with `E_RUN_NOT_FOUND` or `E_RUN_ID_AMBIGUOUS` when appropriate
- fails with `E_RUN_BROKEN` if meta.json is unreadable
- fails with `E_WORKTREE_MISSING` if worktree path does not exist
- resolves editor:
  - `--editor <name>` overrides defaults
  - otherwise uses user config defaults.editor
  - uses `editors.<name>` override if present; else uses name directly
- execs editor command with worktree path as a single arg
- inherits stdio; returns editor exit code
- read-only: no repo lock, no meta mutations, no events

## doctor updates
`agency doctor`:
- loads user config from `$AGENCY_CONFIG_DIR/config.json`
- if missing or invalid: `E_INVALID_USER_CONFIG`
- prints new fields (see constitution): `user_config_path`, `defaults_parent_branch`, `defaults_runner`, `defaults_editor`
  - `defaults_parent_branch` is the current branch at doctor time
- verifies resolved editor command exists and is executable; `E_EDITOR_NOT_CONFIGURED` on failure

## error codes
new:
- `E_INVALID_USER_CONFIG` — user config missing or invalid
- `E_EDITOR_NOT_CONFIGURED` — editor command not found or not executable

## files + functions
new/updated files:
- `$AGENCY_CONFIG_DIR/config.json` (user-level settings)
- `agency.json` template updated to scripts-only

new/updated functions (expected):
- config: `LoadUserConfig`, `ValidateUserConfig`, `ResolveDefaults`
- commands: `Open` with global run resolution (similar to `show`/`push`)
- doctor: load user config and include its fields in output

## tests
- config load/validate:
  - missing file handled (built-in defaults vs doctor error)
  - invalid schema errors `E_INVALID_USER_CONFIG`
  - runners/editors whitespace rejection
  - invalid config blocks all commands
- resolution:
  - relative path resolution against `$AGENCY_CONFIG_DIR`
  - PATH lookup for bare commands
  - editor not configured errors `E_EDITOR_NOT_CONFIGURED`
  - parent branch uses `--parent` or current branch; detached HEAD errors
- `agency open`:
  - resolves run id globally (exact/prefix)
  - rejects ambiguous/not found/broken runs
  - rejects missing worktree
  - execs editor with worktree path argument
- doctor:
  - fails if user config missing
  - prints user config path + defaults (parent/runner/editor)
  - errors when editor command is missing or not executable

## acceptance
- `agency open <run-id>` opens the correct worktree globally without requiring repo cwd
- missing user config uses built-in defaults for non-doctor commands
- invalid user config fails all commands
- `agency doctor` fails when user config is missing or invalid
- parent branch defaults to current branch when `--parent` is omitted
- `agency.json` with non-script keys is rejected (`E_INVALID_AGENCY_JSON`)
- user config validation is strict and stable (no unknown keys)
