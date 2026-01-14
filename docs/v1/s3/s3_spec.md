# Agency L2: Slice 03 — Push (git push + gh pr create/update + report sync)

## goal
implement `agency push <run_id>`: push the run branch to `origin` on GitHub, create a PR if missing, and sync `.agency/report.md` to the PR body — deterministically and non-interactively.

## scope
- `agency push <id>`:
  - validates prerequisites (run exists, worktree present, github.com origin, gh authenticated)
  - fetches origin refs (no rebase/reset)
  - refuses to push if the run branch has **0 commits ahead** of the configured parent
  - pushes branch using `git push -u origin <branch>`
  - creates PR via `gh pr create` if missing
  - updates PR body via `gh pr edit --body-file` on every push if report is present + non-empty
  - persists PR metadata (`pr_number`, `pr_url`) and timestamps into `meta.json`

## non-scope
- merge (`gh pr merge`) and mergeability checks
- verify script execution
- PR checks enforcement / parsing
- draft PRs
- title updates after initial PR creation
- forks / upstream-base PRs / GitHub Enterprise hosts
- any interactive prompts (no `gh` prompts, no editor)

## public surface area added/changed

### commands
- `agency push <run_id> [--force]`

### flags
- `--force`
  - allows creating/updating PR even if `.agency/report.md` is missing or effectively empty (still logs a warning)
  - does **not** bypass `E_EMPTY_DIFF`
  - does **not** enable force-push

### output
- prints `pr: <url>` on success (single stdout line)
- prints warnings (to stderr) for dirty worktree and missing/empty report (unless `--force` is used to proceed)

## files created/modified

### modified
- `${AGENCY_DATA_DIR}/repos/<repo_id>/runs/<run_id>/meta.json`
  - set/update:
    - `pr_number`
    - `pr_url`
    - `last_push_at`
    - `last_report_sync_at` (only when report synced)
    - `last_report_hash` (sha256 of `.agency/report.md`, only when report synced)

- `${AGENCY_DATA_DIR}/repos/<repo_id>/runs/<run_id>/events.jsonl`
  - append events:
    - `push_started`
    - `git_fetch_finished`
    - `git_push_finished`
    - `pr_created` (if created)
    - `pr_body_synced` (if body updated)
    - `push_finished` (always on success)
    - `push_failed` (on failure)

### no new repo-local files
slice 03 does not create/modify `.agency/report.md` (created in slice 01).

## new error codes
(slice 03 introduces new errors; these become part of the public contract)

- `E_UNSUPPORTED_ORIGIN_HOST` — `origin` exists but is not `github.com`
- `E_NO_ORIGIN` — no `origin` remote configured
- `E_PARENT_NOT_FOUND` — neither `<parent_branch>` nor `origin/<parent_branch>` exists after fetch
- `E_GIT_PUSH_FAILED` — `git push` non-zero exit
- `E_GH_PR_CREATE_FAILED` — `gh pr create` non-zero exit
- `E_GH_PR_EDIT_FAILED` — `gh pr edit` non-zero exit
- `E_GH_PR_VIEW_FAILED` — unable to `gh pr view ... --json ...` after successful `gh pr create` (after retries)
- `E_PR_NOT_OPEN` — PR found for run/branch but `state != OPEN`

(existing errors referenced)
- `E_RUN_NOT_FOUND`
- `E_GH_NOT_AUTHENTICATED`
- `E_GH_NOT_INSTALLED`
- `E_EMPTY_DIFF`
- `E_REPO_LOCKED`

## behaviors (given/when/then)

### non-interactive execution
- all `git` and `gh` subprocesses in `agency push` MUST set:
  - `GH_PROMPT_DISABLED=1` (disable gh prompts)
  - `GIT_TERMINAL_PROMPT=0` (disable git credential prompts)
- stdin remains `/dev/null` for these subprocesses.

### origin / auth gating
- given a run exists
- when `agency push <id>` runs
- then it MUST:
  1) assert remote `origin` exists (else `E_NO_ORIGIN`)
  2) parse `origin` URL host; if not exactly `github.com`, fail `E_UNSUPPORTED_ORIGIN_HOST`
  3) assert `gh auth status` succeeds (else `E_GH_NOT_AUTHENTICATED`)
     - GitHub Enterprise is unsupported in v1

### fetch (non-destructive)
- given prerequisites pass
- when `agency push <id>` runs
- then it MUST run `git fetch origin` in the workspace and MUST NOT rebase/reset/merge any branch.

### parent ref resolution + ahead check
- given `parent_branch` from `meta.json` (derived from `agency.json`)
- when computing ahead count
- then:
  - prefer local ref `<parent_branch>` if it exists
  - else use `origin/<parent_branch>` if it exists after fetch
  - else fail `E_PARENT_NOT_FOUND`
- then compute:
  - `ahead = git rev-list --count <parent_ref>..<workspace_branch>`
  - if `ahead == 0` => fail `E_EMPTY_DIFF`
- note: `--force` MUST NOT bypass `E_EMPTY_DIFF`.

### worktree cleanliness
- given workspace has uncommitted changes
- when `agency push <id>` runs
- then it MUST warn (stderr) but MUST continue (push uses commits, not working tree state).

### report gating + force
- given `.agency/report.md` is missing or effectively empty
- when `agency push <id>` runs
- then it MUST:
  - if `--force` not set: abort before any network side effects (git fetch/push/gh) with a warning + non-zero exit (no new error code; reuse `E_SCRIPT_FAILED` is disallowed; use `E_REPORT_INVALID`)
  - if `--force` set: continue, but:
    - PR creation uses a minimal placeholder body string
    - PR body sync is skipped unless file exists (never invent content)

**definition: effectively empty**
- file missing OR trimmed content length `< 20` characters

**new error code**
- `E_REPORT_INVALID` — report missing/empty without `--force`

### git push behavior
- when pushing branch
- then it MUST run:
  - `git push -u origin <workspace_branch>`
- it MUST NOT use `--force` or `--force-with-lease`.
- on non-zero exit => `E_GIT_PUSH_FAILED` with captured stderr.

### PR lookup (idempotency)
- given `meta.json` may or may not contain PR identifiers
- when determining whether a PR already exists
- then:
  1) if `meta.pr_number` exists: attempt `gh pr view <pr_number> --json number,url,state`
     - if it succeeds:
       - if `state != "OPEN"` (including `CLOSED` or `MERGED`), fail `E_PR_NOT_OPEN`
       - else persist the returned `number` + `url` and treat as authoritative
     - if it fails, fall back to step (2)
  2) attempt `gh pr view --head <workspace_branch> --json number,url,state`  [oai_citation:0‡GitHub CLI](https://cli.github.com/manual/gh_pr_view?utm_source=chatgpt.com)
     - if it succeeds:
       - if `state != "OPEN"` (including `CLOSED` or `MERGED`), fail `E_PR_NOT_OPEN`
       - else persist the returned `number` + `url`
  3) if still not found: create PR (below)

### PR creation
- when creating a PR
- then it MUST call `gh pr create` with explicit non-interactive flags:
  - `--base <parent_branch>`  [oai_citation:1‡GitHub CLI](https://cli.github.com/manual/gh_pr_create?utm_source=chatgpt.com)
  - `--head <workspace_branch>` (or run in the branch context and still pass `--head` explicitly)
  - `--title "[agency] <title>"`
  - body source:
    - if report exists and non-empty: `--body-file <worktree>/.agency/report.md`  [oai_citation:2‡GitHub CLI](https://cli.github.com/manual/gh_pr_create?utm_source=chatgpt.com)
    - else (only allowed with `--force`): `--body "agency: report missing/empty; see workspace .agency/report.md"`
- after create, it MUST fetch PR metadata via:
  - `gh pr view --head <workspace_branch> --json number,url,state`
- it SHOULD retry the post-create view (small backoff, no long sleeps); if it still fails: `E_GH_PR_VIEW_FAILED`
- if `state != "OPEN"` (including `CLOSED` or `MERGED`), fail `E_PR_NOT_OPEN`
- then persist:
  - `pr_url` (from `url`)
  - `pr_number` (from `number`)
- on failure => `E_GH_PR_CREATE_FAILED` with captured stderr.

### PR body sync (update)
- given a PR exists and report exists and is non-empty
- when `agency push <id>` runs
- then it MUST update PR body to match the report using:
  - `gh pr edit <pr_number> --body-file <report_path>`  [oai_citation:3‡GitHub CLI](https://cli.github.com/manual/gh_pr_edit?utm_source=chatgpt.com)
- if the computed report hash matches `meta.last_report_hash`, it SHOULD skip the edit (no event, no timestamp update)
- title is NOT updated after creation in v1.
- on failure => `E_GH_PR_EDIT_FAILED`.

### metadata writes (canonical)
on successful push:
- update meta.json:
  - `last_push_at` = now (RFC3339)
  - persist `pr_url` and `pr_number` (if PR exists)
- if report synced:
  - `last_report_sync_at` = now
  - `last_report_hash` = sha256(report contents)

## persistence
slice 03 writes:
- meta.json updates (PR metadata + timestamps + hash)
- append-only events.jsonl entries for observability and debugging
- no run-state deletion or archiving

## tests

### automated (minimum)
- unit tests (table-driven):
  - origin host parsing rejects non-`github.com`
  - parent ref resolution behavior:
    - local parent exists → use it
    - local missing but origin/<parent> exists → use it
    - neither exists → `E_PARENT_NOT_FOUND`
  - ahead==0 → `E_EMPTY_DIFF` even with `--force`
  - report gating:
    - missing/empty without `--force` → `E_REPORT_INVALID`
    - missing/empty with `--force` → proceeds (but no body-file sync)
  - PR idempotency selection:
    - meta.pr_number present and view succeeds → use it
    - meta.pr_number present and view fails → fallback to branch lookup
    - view succeeds but state != OPEN → `E_PR_NOT_OPEN`

- mockable command runner abstraction:
  - all `git` and `gh` invocations go through an interface so tests can simulate outputs without network.

### manual acceptance
1) create a GitHub repo with `origin` on `github.com` and `gh` authenticated.
2) `agency run --title "push demo" --runner claude` (slice 01)
3) in the run, make a commit on the workspace branch.
4) ensure `.agency/report.md` is non-empty.
5) `agency push <id>`:
   - should push branch
   - should create PR
   - should print `pr: <url>`
6) edit `.agency/report.md`, rerun `agency push <id>`:
   - should update PR body (no duplicate PR)
7) revert commits so ahead becomes 0, rerun push:
   - should fail `E_EMPTY_DIFF`.

## guardrails
- do not run `scripts.verify` or any scripts in slice 03
- do not rebase/reset/merge any branch
- do not force push
- do not modify PR title after creation in v1
- do not attempt to support GitHub Enterprise or forks/upstream PR bases
- `agency push` must remain non-interactive (no prompts, no editor)

## rollout notes
- s3 is the first slice that relies on real GitHub network calls.
- keep stderr from `git`/`gh` surfaced verbatim (plus an error code) to make failures actionable.
