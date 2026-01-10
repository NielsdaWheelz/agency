# Agency Slice 01: PR Roadmap (v1 MVP)

## Goal
Split S1 into small PRs that are one-shot implementable, low blast-radius, and leave the repo always in a shippable state. Keep dependencies explicit; focus on internal capability PRs first, then CLI UX last.

---

## PR-01: Core utilities + error type + subprocess helper

### Goal
Introduce foundational types/utilities used across S1 without touching git worktrees or tmux.

### Scope
- Add run id generation, slugify, branch name builder.
- Reuse the S0 path resolution package for data dir computation (no duplication).
- Add global path helpers for:
  - `${AGENCY_DATA_DIR}/repos/<repo_id>/runs/<run_id>/meta.json`
  - `${AGENCY_DATA_DIR}/repos/<repo_id>/runs/<run_id>/logs/setup.log`
  - `${AGENCY_DATA_DIR}/repos/<repo_id>/worktrees/<run_id>/`
- Add `ShellEscapePosix(s string) string` (for paths).
- Add `BuildRunnerShell(worktreePath, runnerCmd string) string` for the tmux
  pane command.
- Add `AgencyError` type with structured fields (code/message/cause/details).
- Ensure `E_INTERNAL` exists in the l0 error list (add if missing).
- Add subprocess helper:
  - `RunCmd(ctx, name, args, opts) (stdout, stderr string, exitCode int)`
  - Supports cwd, env override/add, stdin null, context timeout.
- Add atomic write helper (temp + rename) for `*.json`.

### Non-scope
- No git commands, no tmux, no scripts execution.
- No CLI behavior beyond internal helpers.

### Acceptance
- Unit tests:
  - `slugify(title)` table tests.
  - Branch name format: `agency/<slug>-<shortid>`.
  - Run id format: `<yyyymmddhhmmss>-<rand>` (or your chosen exact format).
  - Atomic write produces valid JSON and is replace-safe.

### Guardrails
- Do not introduce new persistent schema fields beyond what S1 already
  specified.
- Keep these utilities in a dedicated package (e.g. `internal/core`).

---

## PR-02: Repo detection + gates + repo.json update

### Goal
Implement the "safe to start" checks used by agency run and ensure repo state
is persisted.

### Scope
- Repo root resolution from CWD (`git rev-parse --show-toplevel`).
- Compute `repo_id` using S0 rules (reuse S0 code, no reimplementation).
- Ensure `${AGENCY_DATA_DIR}/repos/<repo_id>/repo.json` exists.
- Update `repo.json`: `last_seen_at` + `origin_url` if present.
- Parent cleanliness check at repo root:
  - `git status --porcelain` empty => OK.
  - Else `E_PARENT_DIRTY`.
- Empty repo check:
  - If `git rev-parse --verify HEAD` fails => `E_EMPTY_REPO`.
- Parent branch local existence check:
  - `git show-ref --verify refs/heads/<parent>` => OK.
  - Else `E_PARENT_BRANCH_NOT_FOUND`.

### Non-scope
- No worktree creation yet.
- No scripts, no tmux.

### Acceptance
- Integration tests:
  - Temp repo helper that creates:
    - Committed clean repo.
    - Dirty repo.
    - Empty init repo.
  - Assert error codes returned.
  - `repo.json` is created/updated with `last_seen_at` and `origin_url`
    (if present).

### Guardrails
- Must not modify git state (no fetch, no checkout).

---

## PR-03: agency.json load + runner resolution (read-only)

### Goal
Load and validate `agency.json` fields used by S1 and resolve the runner/setup strings.

### Scope
- Read `agency.json` at repo root.
- Validate required keys for S1:
  - `version == 1`
  - `defaults.parent_branch`, `defaults.runner`
  - `scripts.setup` is a non-empty string
- Runner resolution:
  - If `runners.<name>` exists => use that string as-is (no parsing).
  - Else if name is `claude` or `codex` => use literal `claude`/`codex`.
  - Else `E_RUNNER_NOT_CONFIGURED`.
- Expose:
  - Resolved `setup_script` string (as configured).
  - Resolved `runner_cmd` string (verbatim).
  - Defaults `parent_branch`, `runner`.

### Non-scope
- Do not run scripts yet.
- Do not check existence of the runner binary yet (that's S0/doctor).

### Acceptance
- Unit tests with fixtures:
  - Valid config resolves runner cmd.
  - Invalid version fails.
  - Missing required keys fails.
  - Weird runner types rejected (`runners` values must be strings).

### Guardrails
- Additive-only schema handling: ignore unknown fields.

---

## PR-04: Run pipeline orchestration (internal API)

### Goal
Establish the integration seam early so later PRs plug into a stable flow without adding "not implemented" production behavior.

### Scope
- Define `RunPipeline(ctx, opts) (runID string, err error)`.
- Opts include: `title`, `runner`, `parent`, `attach`.
- Define a `RunService` interface with step methods and inject it into the
  pipeline (real implementation added in later PRs).
- Pipeline order:
  - `CheckRepoSafe`
  - `LoadConfig`
  - `CreateWorktree`
  - `WriteMeta`
  - `RunSetup`
  - `StartTmux`
- Short-circuit on first error and propagate it as `AgencyError`.

### Non-scope
- No CLI wiring.
- No tmux, git, or script execution implementations yet.

### Acceptance
- Unit tests with a stubbed `RunService`:
  - Success path returns runID.
  - Failure in a step short-circuits later steps.
  - Error codes are preserved.

### Guardrails
- Pipeline must preserve ordering and short-circuit on failure.
- No "not implemented" runtime errors in production code paths.

---

## PR-05: Worktree + scaffolding + collision handling

### Goal
Create the workspace deterministically and handle common collision cases.

### Scope
- Compute branch name + worktree path.
- Create worktree + branch in one step:
  - `git worktree add -b <branch> <worktree_path> <parent_branch>`
- Collision handling (single error code for MVP):
  - Branch name already exists.
  - Worktree path already exists.
  - "Already checked out" errors from `git worktree add`.
  - Map all to `E_WORKTREE_CREATE_FAILED` and include stderr + exact command.
- Create workspace-local dirs:
  - `<worktree>/.agency/`
  - `<worktree>/.agency/out/`
  - `<worktree>/.agency/tmp/`
- Create `<worktree>/.agency/report.md` if missing (template, title prefilled).
- Best-effort `.agency/` ignore check:
  - `git -C <worktree> check-ignore -q .agency/`
  - Warn if not ignored; do not mutate ignore config.

### Non-scope
- No meta write yet.
- No setup execution yet.
- No tmux yet.

### Acceptance
- Integration tests (no CLI demo):
  - Temp repo with commit + `agency.json`.
  - Run worktree creation function; assert dirs + report exist.
  - Collision scenarios return `E_WORKTREE_CREATE_FAILED` with stderr.

### Guardrails
- Do not overwrite `<worktree>/.agency/report.md` if it already exists.
- Do not delete anything on failure.

---

## PR-06: meta.json writer + run dir creation

### Goal
Persist run metadata immediately after worktree creation and prevent run-id collisions.

### Scope
- Create `${AGENCY_DATA_DIR}/repos/<repo_id>/runs/<run_id>/` using O_EXCL
  semantics (fail if it already exists).
- Create `logs/` under the run dir.
- Write initial `meta.json` right after worktree creation:
  - Required fields (`schema_version` 1.0, ids, title, runner, parent, branch, `worktree_path`, `created_at`, `runner_cmd`).

### Non-scope
- Do not execute setup yet.
- No tmux.

### Acceptance
- Unit tests:
  - Meta required fields present.
  - Atomic write behavior.
- Integration tests:
  - Run dir O_EXCL creation fails when the dir already exists.
  - Meta exists even on failure paths that happen after worktree creation.

### Guardrails
- JSON writes must be atomic (temp + rename).
- If run dir exists, return an error (no retries).

---

## PR-07: Setup script execution + logging

### Goal
Run `scripts.setup` outside tmux with injected env and capture logs deterministically.

### Scope
- Execute setup script as shell command string via `sh -lc <script>`.
- CWD = worktree root.
- Stdin = `/dev/null`.
- Inject env exactly per L0 (`AGENCY_*` + `CI=1` etc.).
- Timeout = 10 minutes (hardcoded v1).
- Capture stdout/stderr to `logs/setup.log` (truncate on each run attempt).
- On failure or timeout:
  - Update meta flags and setup fields.
  - Ensure no tmux session creation occurs (defer anyway).

### Non-scope
- No tmux.
- No attach semantics.

### Acceptance
- Integration tests:
  - Setup script that sleeps > timeout triggers `E_SCRIPT_TIMEOUT`.
  - Setup script that exits non-zero triggers `E_SCRIPT_FAILED`.
  - Log file contains script output.
  - Setup script writes a sentinel file into `.agency/tmp/` and exits 0.

### Guardrails
- Scripts must be non-interactive; enforce stdin null.
- Do not run setup inside tmux.

---

## PR-08: tmux session creation + attach command

### Goal
Create the real runner TUI session and allow attaching.

### Scope
- On successful setup:
  - Create tmux session `agency:<run_id>` detached.
  - Pane command uses shell:
    - `sh -lc 'cd <escaped_worktree_path> && exec <runner_cmd>'`
  - Store `tmux_session_name` in `meta.json`.
- Implement `agency attach <run_id>`:
  - Resolve repo root from CWD.
  - Compute `repo_id` for this repo.
  - Load meta from
    `${AGENCY_DATA_DIR}/repos/<repo_id>/runs/<run_id>/meta.json`.
  - Attach with `tmux attach -t agency:<run_id>` (inside or outside tmux).
  - If run unknown: `E_RUN_NOT_FOUND`.
  - If run_id exists under a different repo_id: `E_RUN_REPO_MISMATCH`.
  - If run known but session missing: `E_TMUX_SESSION_MISSING` and print:
    - `worktree_path`.
    - `runner_cmd`.
    - Suggested manual command.

### Non-scope
- No resume/stop/kill.
- No ls.
- No pid inspection.

### Acceptance
- Manual:
  - Run creates tmux session; `tmux ls | grep agency:<id>` shows it.
  - `agency attach <id>` lands in the runner pane.
  - Forced tmux failure sets `flags.tmux_failed=true` and leaves `tmux_session_name` absent.
- Automated (optional/integration):
  - `tmux has-session -t agency:<id>` succeeds after run.

### Guardrails
- If session name collision: `E_TMUX_SESSION_EXISTS`.
- If tmux creation fails: `E_TMUX_FAILED` and set `flags.tmux_failed=true` in
  meta; do not set `tmux_session_name`.

---

## PR-09: Wire agency run end-to-end + --attach UX + output

### Goal
Expose the full S1 user flow with the exact CLI UX promised by the spec.

### Scope
- Implement `agency run` command:
  - Parse flags `--title`, `--runner`, `--parent`, `--attach`.
  - Default title `untitled-<shortid>`.
  - Default runner/parent from `agency.json`.
  - Call `RunPipeline` and surface errors.
  - Print success summary and next steps.
- Implement `--attach` semantics:
  - Always `tmux attach -t agency:<run_id>` (blocking until detach).

### Acceptance
- Manual smoke from spec, including dirty parent failure.
- Ensure warnings for `.agency/` ignore are printed (best-effort).

### Guardrails
- Still no ls, no GitHub ops, no resume/stop/kill.

---

## Roadmap notes
- Ordering is linear; PR-04 establishes the integration seam early.
- Prefer tests and internal harnesses over partial CLI demos until PR-09.
- Every PR should update/maintain the slice's promised behavior and keep the binary usable (earlier PRs must not break init/doctor from S0).
