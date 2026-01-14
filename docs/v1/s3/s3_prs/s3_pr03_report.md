# s3_pr03 report — gh PR idempotency + create + body sync + metadata persistence

## summary

implemented the github PR half of `agency push <run_id>`:
- deterministic PR lookup (by number, then by branch) for idempotency
- PR creation via `gh pr create` with explicit non-interactive flags
- PR body sync via `gh pr edit --body-file` when report is present + non-empty
- metadata persistence (`pr_number`, `pr_url`, `last_report_sync_at`, `last_report_hash`)
- event logging (`pr_created`, `pr_body_synced`, `push_finished`, `push_failed`)
- injectable `Sleeper` interface for testable retry logic
- updated `agency show` to display new PR sync fields

## problems encountered

### 1. json parsing in tests
- initially wrote a custom json parser for tests to avoid importing `encoding/json` in the test helper
- the custom parser had bugs and failed to extract fields correctly
- solution: simplified the test helper to use string parsing directly for the specific struct

### 2. retry timing for post-create view
- `gh pr create` returns the PR URL in stdout, but the spec says NOT to parse stdout
- instead, we use `gh pr view --head <branch>` to look up the PR after creation
- this can fail immediately after creation due to GitHub API eventual consistency
- solution: implemented retry logic with delays of 0ms, 500ms, 1500ms (3 attempts total)
- used injectable `Sleeper` interface to avoid real sleeps in unit tests

### 3. report hash change detection
- needed to avoid unnecessary `gh pr edit` calls when report unchanged
- solution: compute sha256 hash of report and compare with `meta.last_report_hash`
- only call `gh pr edit` and update timestamps when hash differs

## solutions implemented

### pr lookup order (idempotent)
1. if `meta.pr_number` exists: `gh pr view <number> --json number,url,state`
2. fallback: `gh pr view --head <branch> --json number,url,state`
3. if still not found: create PR

### pr creation
- `gh pr create --base <parent> --head <branch> --title "[agency] <title>" --body-file <report>`
- placeholder body with `--force` flag when report is empty
- post-create lookup via `gh pr view --head <branch>` with retry

### pr body sync
- compute sha256 hash of `.agency/report.md`
- compare with `meta.last_report_hash`
- if different: `gh pr edit <number> --body-file <report>`
- update `last_report_sync_at` and `last_report_hash`

### state validation
- fail `E_PR_NOT_OPEN` if PR exists but state is CLOSED or MERGED
- include recovery guidance in error message

## decisions made

### 1. never parse gh pr create stdout
per spec, we always use `gh pr view --head <branch>` after creation instead of parsing the URL from stdout. this is more robust and follows the convention established in the spec.

### 2. retry delays
chose 0ms, 500ms, 1500ms for retry delays. this gives ~2 seconds total wait time, which is usually enough for GitHub API consistency. the delays are injectable via `Sleeper` interface for testing.

### 3. title format
PR title is `[agency] <run_title>` or `[agency] <branch>` if title is empty. the `[agency]` prefix makes it easy to identify agency-created PRs.

### 4. placeholder body with --force
when report is missing/empty and `--force` is used, the PR body is:
```
agency: report missing/empty (run_id=<id>, branch=<branch>). see workspace .agency/report.md
```

### 5. event data structure
`push_finished` event now includes:
- `pr_number`
- `pr_url`
- `pr_action` ("created" or "updated")

## deviations from spec

### none significant
the implementation follows the spec closely. minor clarifications:
- the spec mentions `E_GH_PR_VIEW_FAILED` for post-create view failures; implemented as specified
- the spec says to use `--body-file` for PR creation; implemented as specified
- retry delays match the spec (try immediately, then after 500ms, then after 1500ms)

## how to run new/changed commands

### create/update a PR
```bash
# prerequisite: have an agency run with at least one commit
agency run --title "test feature"
# (make commits in the worktree)

# push branch and create PR
agency push <run_id>

# edit .agency/report.md in the worktree
# push again to update PR body
agency push <run_id>
```

### view PR info
```bash
# human-readable
agency show <run_id>
# look for === pr === section with:
#   pr_number: 123
#   pr_url: https://github.com/...
#   last_push_at: ...
#   last_report_sync_at: ...
#   last_report_hash: ...

# json output
agency show <run_id> --json | jq '.data.meta.pr_number'
```

### force push with empty report
```bash
agency push <run_id> --force
# creates PR with placeholder body
```

## how to verify functionality

### unit tests
```bash
go test ./internal/commands/... -v -run 'Test(PR|Push|Report)'
```

### manual acceptance testing
1. create a github repo with `gh` authenticated
2. `agency run --title "pr test"`
3. make a commit in the worktree
4. ensure `.agency/report.md` has content (>= 20 chars)
5. `agency push <id>` → should print `pr created: <url>`
6. edit `.agency/report.md`, run `agency push <id>` again → should print `pr updated: <url>`
7. verify PR body on GitHub matches report content
8. close the PR on GitHub, run `agency push <id>` → should fail `E_PR_NOT_OPEN`

## files changed

### modified
- `internal/commands/push.go` — added PR lookup, create, body sync logic
- `internal/commands/push_test.go` — added unit tests for PR functionality
- `internal/commands/show.go` — added `LastReportSyncAt`, `LastReportHash` to human output
- `internal/render/show.go` — updated `ShowHumanData` struct and `WriteShowHuman` function
- `README.md` — updated push command documentation with PR behavior

### no new files created
all functionality integrated into existing files per spec.

## branch name and commit message

**branch:** `pr3/s3-pr03-gh-pr-create-update-sync`

**commit message:**
```
feat(push): implement gh PR create/update + body sync

complete slice 03 by implementing the GitHub PR half of `agency push`:

- PR lookup order: meta.pr_number → branch lookup → create
- PR creation: gh pr create --base --head --title --body-file
  - title format: [agency] <run_title> or [agency] <branch>
  - placeholder body with --force when report empty
- PR body sync: gh pr edit --body-file when report hash differs
- state validation: fail E_PR_NOT_OPEN for CLOSED/MERGED PRs
- retry logic: post-create view with 0/500/1500ms delays
- injectable Sleeper interface for testable retry timing

metadata persistence:
- pr_number, pr_url persisted to meta.json
- last_report_sync_at, last_report_hash track body sync state
- push_finished event includes pr_number, pr_url, pr_action

events (append-only to events.jsonl):
- pr_created: when new PR created
- pr_body_synced: when PR body updated
- push_finished: includes pr metadata
- push_failed: includes error_code and step

agency show updated to display:
- last_report_sync_at in === pr === section
- last_report_hash in === pr === section

error codes added/used:
- E_GH_PR_CREATE_FAILED
- E_GH_PR_EDIT_FAILED
- E_GH_PR_VIEW_FAILED
- E_PR_NOT_OPEN

all git/gh subprocesses run with non-interactive env:
- GIT_TERMINAL_PROMPT=0
- GH_PROMPT_DISABLED=1
- CI=1

implements spec: docs/v1/s3/s3_prs/s3_pr3.md
```
