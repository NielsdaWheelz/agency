# agency slice 00: pr roadmap (bootstrap)

goal: implement slice 00 (`agency init`, `agency doctor`, config validation, repo identity + persistence) in small, reviewable, one-shot-able PRs.

rules for every PR:
- no worktrees, no tmux session creation, no script execution, no run meta/events
- no network calls beyond `gh auth status`
- all external commands go through a stub-friendly `CommandRunner` interface
- all filesystem writes go through a stub-friendly `FS` interface (or thin wrappers) and use atomic write for json
- keep stdout stable + parseable (key: value lines), stderr for errors
- update docs only in the final PR (unless the PR introduces a new public contract that must be documented immediately)

---

## pr-00: project skeleton + shared contracts

**goal**
- create the go project scaffolding and the shared “public contract” primitives needed by later PRs.

**scope**
- command dispatcher skeleton for subcommands (stdlib `flag`, no cobra)
- central error type + error codes (including any slice-00-only codes)
- interfaces:
  - `CommandRunner` (Run(ctx, name, args, opts) → stdout/stderr/exit)
  - `FS` (ReadFile/WriteFile/MkdirAll/Stat/Chmod/Rename/etc.)
- JSON atomic write helper (`writeFileAtomic(path, bytes, perm)`)

**files**
- `cmd/agency/main.go`
- `internal/cli/dispatch.go`
- `internal/errors/errors.go`
- `internal/exec/runner.go` (interface + real impl)
- `internal/fs/fs.go` (interface + real impl)
- `internal/fs/atomic.go`

**tests**
- unit: atomic write writes full file (no partial) using temp dir
- unit: error code formatting is stable

**acceptance**
- `agency --help` works
- `agency init --help` and `agency doctor --help` exist (may be stubbed returning not-implemented error)

---

## pr-01: directory resolution + repo discovery + origin parsing (pure logic)

**goal**
- implement deterministic path resolution + repo identity parsing with table-driven tests.

**scope**
- directory resolution per constitution (macOS defaults + XDG fallbacks + env overrides)
- repo root discovery:
  - `git rev-parse --show-toplevel` via `CommandRunner`
  - normalize/realpath repo root for hashing
- origin url parsing (pure function; no gh usage):
  - support ssh + https github.com formats → `repo_key = github:<owner>/<repo>`
  - else `repo_key = path:<sha256(abs_repo_root)>`
  - compute `repo_id = sha256(repo_key)` truncated to 16 hex chars

**files**
- `internal/paths/xdg.go`
- `internal/git/repo.go` (repo root discovery + origin url getter)
- `internal/identity/parse_origin.go`
- `internal/identity/repo_id.go`

**tests**
- unit: dir resolution precedence matrix
- unit: origin parsing (ssh/https/with and without .git/non-github/missing)
- unit: hashing determinism (repo_key → repo_id)

**acceptance**
- a small debug dev command (or temporary) can print resolved dirs + derived repo_key/repo_id (can be removed later)

---

## pr-02: agency.json schema + strict validation

**goal**
- implement loading + validating `agency.json` (schema v1) with crisp errors.

**scope**
- parse JSON strictly:
  - version must be integer `1`
  - required fields: `defaults.parent_branch`, `defaults.runner`, `scripts.setup/verify/archive`
  - `runners` values must be non-empty strings if present
  - runner commands must be single executables (no args)
  - reject empty strings for script paths / runner commands
- runner resolution logic:
  - if `runners.<name>` exists → use it
  - else if defaults.runner is `claude|codex` → PATH fallback
  - else `E_RUNNER_NOT_CONFIGURED`
- produce one “first validation error” message for `doctor`

**files**
- `internal/config/agencyjson.go`
- `internal/config/validate.go`

**tests**
- fixtures in `internal/config/testdata/`:
  - valid minimal
  - missing required
  - wrong types
  - wrong version
  - empty string fields
- unit: validation error message is actionable and stable

**acceptance**
- `agency doctor` (still stubbed) can load + validate and report a single validation error string

---

## pr-03: persistence schemas + repo store (repo_index.json + repo.json)

**goal**
- define and implement the on-disk schemas + atomic persistence; write nothing unless doctor succeeds.

**scope**
- schema definitions (public contract) for:
  - `${AGENCY_DATA_DIR}/repo_index.json`
  - `${AGENCY_DATA_DIR}/repos/<repo_id>/repo.json`
- implement read/merge/write with atomic rename:
  - repo_index keyed by `repo_key`
  - store seen paths + last_seen_at
  - repo.json stores:
    - schema_version
    - repo_key, repo_id
    - repo_root_last_seen, agency_json_path
    - origin_present, origin_url, origin_host
    - capabilities (github_origin, origin_host, gh_authed)
    - created_at, updated_at
- no run state, no events

**files**
- `internal/store/repo_index.go`
- `internal/store/repo_record.go`
- `internal/store/store.go`

**tests**
- unit: repo_index roundtrip + merge (add path, update last_seen_at)
- unit: repo.json roundtrip
- unit: “write only on success” behavior is enforced by caller contract (covered in pr-05)

**acceptance**
- can create `${AGENCY_DATA_DIR}` tree and write both JSON files successfully in temp env

---

## pr-04: `agency init` command (scaffold + gitignore)

**goal**
- implement `agency init [--no-gitignore] [--force]` exactly per slice spec.

**scope**
- inside git repo required (`E_NO_REPO` otherwise)
- refuse overwrite unless `--force` (`E_AGENCY_JSON_EXISTS`)
- write template `agency.json` matching l0 exactly (no drift)
- create `scripts/` dir if missing
- create stub scripts if missing (never overwrite), chmod 0755:
  - `scripts/agency_setup.sh`
  - `scripts/agency_verify.sh`
  - `scripts/agency_archive.sh`
- ensure template uses relative script paths under repo root (no absolute paths)
- `.gitignore` behavior:
  - default append `.agency/` (create file if missing)
  - no duplicate entries
  - ensure newline at end
  - `--no-gitignore` skips modification

**files**
- `internal/commands/init.go`
- `internal/scaffold/template.go`
- `internal/scaffold/stubs.go`
- `internal/scaffold/gitignore.go`

**tests**
- integration-style (temp git repo):
  - init creates files
  - init refuses overwrite
  - init with --force overwrites agency.json but not scripts
  - gitignore append idempotent + newline

**acceptance**
- manual: run `agency init` in a repo; inspect created files + executable bit

---

## pr-05: `agency doctor` command (checks + output + persistence-on-success)

**goal**
- implement strict prerequisite verification + stable output + persistence updates.

**scope**
- require repo + agency.json exists + validates
- compute repo identity + origin status:
  - origin missing/non-github allowed; report github_flow_available: no
- prerequisite checks via `CommandRunner`:
  - `git --version` else `E_GIT_NOT_INSTALLED`
  - `tmux -V` else `E_TMUX_NOT_INSTALLED`
  - `gh --version` else `E_GH_NOT_INSTALLED`
  - `gh auth status` else `E_GH_NOT_AUTHENTICATED`
- resolve runner command and verify it exists (PATH lookup or `exec.LookPath`) else `E_RUNNER_NOT_CONFIGURED`
  - runner command is a single executable (no args)
  - stdout empty on failure
- script checks:
  - resolve script paths relative to repo root
  - missing → `E_SCRIPT_NOT_FOUND`
  - not executable → `E_SCRIPT_NOT_EXECUTABLE` (suggest chmod +x)
- output contract (stdout):
  - stable `key: value` lines (define the exact keys in code)
  - include tool versions captured from stdout
- persistence:
  - only on full success: write/update repo_index.json + repo.json

**files**
- `internal/commands/doctor.go`
- `internal/doctor/checks.go`
- `internal/doctor/output.go`

**tests**
- unit: doctor output formatting (golden file test)
- unit: checks with stubbed CommandRunner (simulate missing tools/auth failure)
- integration-style: temp repo + real git; stub gh/tmux/git via CommandRunner to avoid machine dependency; assert no json written on failure and written on success

**acceptance**
- manual:
  - unauthenticated gh → doctor fails with `E_GH_NOT_AUTHENTICATED` and instructs `gh auth login`
  - missing script exec bit → doctor fails with chmod suggestion
  - success → prints derived values + writes json files

---

## pr-06: tighten public contracts + docs sync (slice 00 completion)

**goal**
- ensure slice 00 is “closed”: contracts are documented, error codes aligned, and UX is consistent.

**scope**
- update `docs/agency/l0.md` and add `docs/agency/slices/slice-00_bootstrap.md` (if you keep slice docs in-repo now)
- ensure error code list in l0 includes all codes used in slice 00
- ensure repo_index/repo.json schemas are explicitly documented (fields + examples)
- polish help text + exit codes
- add `make test` / `go test ./...` instructions

**files**
- docs only + minor CLI help polish

**tests**
- none new beyond keeping existing tests green

**acceptance**
- `go test ./...` passes
- slice 00 demo script works end-to-end

---

## dependency graph (high level)
- pr-00 → pr-01 → pr-02 → pr-03 → pr-04 → pr-05 → pr-06

---

## “one-shot” constraints per PR (for claude)
- pr-00: no command semantics beyond stubs
- pr-01: pure logic + tests only; no file writes
- pr-02: config validation only; no command wiring
- pr-03: persistence module only; no command wiring
- pr-04: init only; must not add doctor logic
- pr-05: doctor only; must not add run/worktree/tmux-session features
- pr-06: docs + cleanup only
