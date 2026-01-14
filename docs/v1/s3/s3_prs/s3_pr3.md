# agency pr-3 spec — slice 03: gh pr create/update + report sync

## goal
complete slice 03 by adding the **github PR** half of `agency push <run_id>`:
- deterministically find existing PR (idempotent)
- create PR if missing
- sync `.agency/report.md` to PR body when present + non-empty
- persist PR metadata + report sync evidence to `meta.json`

this PR must **not** change the git-side behavior (fetch/ahead/push) implemented earlier.

---

## scope

### in-scope
- implement PR lookup order:
  1) `meta.pr_number` → `gh pr view <number> --json number,url,state`
  2) branch lookup → `gh pr view --head <workspace_branch> --json number,url,state`
  3) create PR → `gh pr create ...`
  4) after create: `gh pr view --head <workspace_branch> --json number,url,state` with small retry
- PR creation (non-interactive, explicit base/head/title/body flags)
- PR body sync via `gh pr edit <number> --body-file <report_path>` when report is present + non-empty
- skip body sync if report hash unchanged vs `meta.last_report_hash`
- write these fields into `${AGENCY_DATA_DIR}/repos/<repo_id>/runs/<run_id>/meta.json`:
  - `pr_number`, `pr_url`
  - `last_report_sync_at` (RFC3339 UTC) **only** when body actually synced (or on create using body-file)
  - `last_report_hash` (sha256 raw bytes) **only** when body actually synced (or on create using body-file)
- append events to `events.jsonl`:
  - `pr_created` (only on create)
  - `pr_body_synced` (only when body updated)
  - `push_finished` (only on full success)
  - `push_failed` (on any failure in this PR’s gh steps)

### explicitly out-of-scope
- any changes to git fetch/ahead/push logic
- merge, verify, PR checks parsing/enforcement
- draft PRs
- updating PR title after creation
- forks/upstream-base PRs, GitHub enterprise hosts
- transcript replay / runner process inspection

---

## public surface area
no new commands/flags. this PR completes the existing:
- `agency push <run_id> [--force]`

success output contract (single line):
- `pr created: <url>` or `pr updated: <url>`

---

## prerequisites / assumptions
- PR-3b already enforces:
  - run exists + worktree present
  - `origin` exists and host is exactly `github.com`
  - `gh auth status` ok
  - report gating (`E_REPORT_INVALID` unless `--force`)
  - git fetch/ahead/push completed successfully and wrote `last_push_at`
  - non-interactive env enforced for subprocesses (`stdin=/dev/null`, `GH_PROMPT_DISABLED=1`, `GIT_TERMINAL_PROMPT=0`)
- PR-3a added:
  - cmd runner abstraction
  - new error codes including `E_GH_PR_CREATE_FAILED`, `E_GH_PR_EDIT_FAILED`
  - meta schema fields `last_report_sync_at`, `last_report_hash`
  - event writer helper

if any of the above is missing, implement the minimum needed locally without widening scope.

---

## behavior (canonical)

### definitions
- report path: `<worktree>/.agency/report.md`
- report is “non-empty” iff `trimmed_len >= 20` (reuse slice definition)
- report hash = sha256 of raw file bytes
- all `gh` calls run with:
  - `cwd = worktree`
  - env overlay includes `GH_PROMPT_DISABLED=1`, `GIT_TERMINAL_PROMPT=0`, `CI=1`, `AGENCY_NONINTERACTIVE=1`

### pr lookup (idempotent)
1) if `meta.pr_number` exists:
   - run: `gh pr view <pr_number> --json number,url,state`
   - if exit 0:
     - if `state == "MERGED"`: fail `E_PR_NOT_OPEN`
     - if `state == "CLOSED"`: fail `E_PR_NOT_OPEN`
     - if `state != "OPEN"`: fail `E_PR_NOT_OPEN` (unexpected state)
     - else this is authoritative
   - if non-zero: fall through to (2)

2) run: `gh pr view --head <workspace_branch> --json number,url,state`
   - if exit 0:
     - if `state == "MERGED"`: fail `E_PR_NOT_OPEN`
     - if `state == "CLOSED"`: fail `E_PR_NOT_OPEN`
     - if `state != "OPEN"`: fail `E_PR_NOT_OPEN` (unexpected state)
     - else use it
   - if non-zero: proceed to create

### pr creation
- create with explicit flags:
  - `gh pr create --base <parent_branch> --head <workspace_branch> --title "<title_string>" <body_flag>`
- title string:
  - if meta.title non-empty: `[agency] <title>`
  - else: `[agency] <workspace_branch>`
- body flag:
  - if report present + non-empty: `--body-file <report_path>`
  - else (only possible with `--force`): `--body "<placeholder>"`
    - placeholder template (stable):
      - `agency: report missing/empty (run_id=<run_id>, branch=<branch>). see workspace .agency/report.md`

after create, do NOT parse stdout. instead lookup via `gh pr view --head <workspace_branch> --json number,url,state` with retry:

#### create follow-up view retry
- attempt `gh pr view --head <workspace_branch> --json number,url,state` up to 3 times:
  - try #1 immediately
  - try #2 after ~500ms (injectable sleeper; no real sleeps in unit tests)
  - try #3 after ~1500ms (injectable sleeper; no real sleeps in unit tests)
- if still failing: `E_GH_PR_VIEW_FAILED` (new error code for this PR)
- if view succeeds but state == MERGED or CLOSED: `E_PR_NOT_OPEN`
- if view succeeds but state is unexpected: `E_PR_NOT_OPEN`

on successful create+view:
- append `pr_created`
- persist `pr_number`, `pr_url`
- if body used `--body-file` (report non-empty):
  - set `last_report_sync_at = now`
  - set `last_report_hash = report_hash`

### pr body sync (update)
if PR exists (from lookup/create) AND report present + non-empty:
- compute report hash
- if `meta.last_report_hash == report_hash`:
  - skip gh edit (no event; no timestamp update)
- else:
  - run: `gh pr edit <pr_number> --body-file <report_path>`
  - on non-zero: `E_GH_PR_EDIT_FAILED`
  - on success:
    - append `pr_body_synced`
    - set `last_report_sync_at = now`
    - set `last_report_hash = report_hash`

### meta persistence + events
- meta writes must be atomic (write temp + rename)
- on full success (git side already succeeded; gh side now succeeded):
  - append `push_finished`
  - print `pr created: <url>` if created this run else `pr updated: <url>`
- on any failure in gh steps:
  - append `push_failed` with minimal data: `{ "error_code": "...", "step": "..." }`
  - return non-zero with the error code and captured stderr
  - if failing with `E_PR_NOT_OPEN`, include guidance:
    - clear `meta.pr_number` for the run (or re-open the PR) and rerun `agency push`

---

## error codes

### new (introduced in this PR)
- `E_GH_PR_VIEW_FAILED` — unable to `gh pr view ... --json ...` after successful `gh pr create` (after retries)
- `E_PR_NOT_OPEN` — a PR was found for the run/branch but `state != OPEN` (expected values: OPEN, CLOSED, MERGED)

### existing (used here)
- `E_GH_PR_CREATE_FAILED`
- `E_GH_PR_EDIT_FAILED`
- `E_REPORT_INVALID` (preflight)
- `E_GH_NOT_AUTHENTICATED` (preflight)
- `E_UNSUPPORTED_ORIGIN_HOST` / `E_NO_ORIGIN` (preflight)

---

## files to modify (expected)

### likely touched
- `cmd/agency/...` or equivalent: `push` command implementation (wire gh steps after git push)
- `internal/gh/*` (or similar): helpers for `pr view/create/edit` using `CmdRunner`
- `${AGENCY_DATA_DIR}` schema structs:
  - `meta.json` struct: ensure fields exist (already additive)
- events writer module: add `pr_created`, `pr_body_synced`, `push_finished`, `push_failed` if missing

### not allowed
- any runner/tmux code
- any git fetch/ahead/push logic
- any slice-04+ behavior (merge/archive/verify)

---

## tests

### unit tests (required)
use mocked `CmdRunner` (no network). table-driven.

1) **meta pr_number happy path**
- meta.pr_number set
- `gh pr view <n> --json ...` returns OPEN + url
- report non-empty
- `gh pr edit` called iff hash differs
- meta updated with pr_number/url + last_report_sync_at/hash

2) **meta pr_number fails → branch lookup succeeds**
- first view non-zero
- branch view OPEN
- proceed

3) **both lookups fail → create → view retry**
- `gh pr create` exit 0
- first `gh pr view --head <branch>` fails
- second succeeds (OPEN)
- ensures retry behavior via injected sleeper (no real sleep in unit tests)

4) **branch lookup finds CLOSED/MERGED**
- `gh pr view --head <branch>` returns state CLOSED (or MERGED)
- fail `E_PR_NOT_OPEN` (no create)

5) **report hash unchanged**
- meta.last_report_hash equals computed
- no `gh pr edit` call
- no timestamp update

6) **create failure**
- `gh pr create` non-zero → `E_GH_PR_CREATE_FAILED`

7) **edit failure**
- `gh pr edit` non-zero → `E_GH_PR_EDIT_FAILED`

### manual acceptance (document only)
- in a github.com repo with gh authenticated:
  1) create run, make commit(s), ensure report non-empty
  2) `agency push <id>` → PR created, url printed
  3) edit report, `agency push <id>` → PR updated, body changed
  4) close PR on github, rerun `agency push <id>` → fails `E_PR_NOT_OPEN` with recovery guidance

---

## guardrails
- never parse raw stdout from `gh pr create` to get PR id; always use `gh pr view --json`
- never create a new PR if a PR is found but not OPEN; fail instead (`E_PR_NOT_OPEN`)
- never update PR title after creation in v1
- keep all gh calls non-interactive (env + stdin null)
- do not change any git operations

---

## runbook (for claude)
commands (adapt to repo conventions):
- `go test ./...`
- if there are focused packages: `go test ./internal/... ./cmd/...`

when done:
- ensure `agency push` prints `pr created:` or `pr updated:` and errors include stderr from gh
- ensure all new error codes are wired into the global error taxonomy and tested
