# s3_pr04 report — polish + docs sync

## summary

completed slice 03 by polishing output formatting and syncing documentation:
- standardized `agency push` stdout to output exactly `pr: <url>` (no "created"/"updated")
- updated warning messages to use exact spec-required strings
- updated error messages to use exact spec-required templates
- rewrote `agency show` human output to use spec-defined flat key/value format
- added unit tests for output formatting contracts
- updated `constitution.md` with `last_report_sync_at` and `last_report_hash` meta fields
- updated `README.md` with slice 3 completion status and new output formats

## problems encountered

### 1. output format change for show command
- the existing `agency show` output used section headers (`=== run ===`, `=== workspace ===`, etc.)
- the PR-4 spec defines a simpler flat key/value format without sections
- had to decide whether to keep backwards compatibility or follow the spec exactly
- **solution:** followed the spec exactly, rewrote `WriteShowHuman` to produce the new format

### 2. PR display format
- spec requires: `pr: <url|none> (#<number|->)`
- when PR exists: `pr: https://github.com/... (#123)`
- when PR missing: `pr: none (#-)`
- this is different from the previous sectioned format with separate `pr_number:` and `pr_url:` fields
- **solution:** implemented single-line combined format as specified

### 3. test updates
- existing tests for `WriteShowHuman` expected the old sectioned format
- had to update all test assertions to match the new flat format
- **solution:** rewrote test cases with new assertions, added test for blank line between worktree and tmux

## solutions implemented

### push output contract

**stdout (success):** exactly one line
```
pr: <url>
```

**stderr warnings:**
```
warning: worktree has uncommitted changes; pushing commits anyway
warning: report missing or empty; proceeding due to --force
```

**stderr errors:**
```
E_REPORT_INVALID: report missing or empty; use --force to push anyway
E_EMPTY_DIFF: no commits ahead of parent; make at least one commit
E_NO_ORIGIN: git remote 'origin' not configured
E_UNSUPPORTED_ORIGIN_HOST: origin host must be github.com
```

### show output contract

**human output format:**
```
run: <run_id>
title: <title|<untitled>>
repo: <repo_id>
runner: <runner>
parent: <parent_branch>
branch: <branch>
worktree: <worktree_path>

tmux: <session_name|none>
pr: <url|none> (#<number|->)
last_push_at: <rfc3339|none>
last_report_sync_at: <rfc3339|none>
report_hash: <sha256|none>
status: <derived_status>
```

note: blank line between `worktree:` and `tmux:` as required by spec.

## decisions made

### 1. removed "created"/"updated" from push output
the spec says stdout should be exactly `pr: <url>` on success. removed the distinction between "pr created:" and "pr updated:" since the PR URL is the same either way.

### 2. simplified error messages
changed error messages to match spec exactly:
- `E_NO_ORIGIN` now says `git remote 'origin' not configured` (removed suggestion to run `git remote add origin`)
- `E_UNSUPPORTED_ORIGIN_HOST` now says `origin host must be github.com` (removed details about the actual host)
- `E_EMPTY_DIFF` now says `no commits ahead of parent; make at least one commit` (consistent with spec)

### 3. flat show output format
chose to follow the spec exactly rather than maintain backwards compatibility with the sectioned format. the new format is more script-friendly and consistent with other commands like `doctor` and `init`.

### 4. none display for missing fields
- missing PR: `pr: none (#-)`
- missing timestamps: `last_push_at: none`
- missing hash: `report_hash: none`

this matches the spec's pattern of using "none" for missing values.

## deviations from spec

### none
the implementation follows the PR-4 spec exactly:
- output formats match the required templates
- warning messages use exact strings
- error messages use exact templates
- show output includes all required fields in correct order
- blank line between worktree and tmux is present

## how to run new/changed commands

### push a branch and see new output
```bash
# prerequisite: have an agency run with at least one commit
agency run --title "test feature"
# (make commits in the worktree)

# push branch - note the simplified output
agency push <run_id>
# output: pr: https://github.com/owner/repo/pull/123
```

### view run details with new format
```bash
agency show <run_id>
# output is now flat key/value format:
# run: 20260110-a3f2
# title: test feature
# repo: abc123
# runner: claude
# parent: main
# branch: agency/test-feature-a3f2
# worktree: /path/to/worktree
#
# tmux: agency_20260110-a3f2
# pr: https://github.com/owner/repo/pull/123 (#123)
# last_push_at: 2026-01-14T12:00:00Z
# last_report_sync_at: 2026-01-14T12:00:00Z
# report_hash: abc123def456...
# status: ready for review
```

### test error messages
```bash
# test E_NO_ORIGIN
# (remove origin from repo)
agency push <run_id>
# stderr: E_NO_ORIGIN: git remote 'origin' not configured

# test E_EMPTY_DIFF
# (reset to parent branch so 0 commits ahead)
agency push <run_id>
# stderr: E_EMPTY_DIFF: no commits ahead of parent; make at least one commit
```

## how to verify functionality

### unit tests
```bash
go test ./internal/commands/... -v -run 'Test(PushOutputFormat|WriteShowHuman)'
```

### manual verification
1. `agency push <id>` prints exactly `pr: <url>` to stdout on success
2. `agency push <id>` with dirty worktree prints `warning: worktree has uncommitted changes; pushing commits anyway` to stderr
3. `agency push <id> --force` with empty report prints `warning: report missing or empty; proceeding due to --force` to stderr
4. `agency show <id>` prints flat key/value format with blank line between worktree and tmux
5. `agency show <id>` prints `pr: none (#-)` when no PR exists

## files changed

### modified
- `internal/commands/push.go` — updated output formatting and error messages
- `internal/commands/push_test.go` — added output formatting tests
- `internal/commands/show_test.go` — updated tests for new show format
- `internal/render/show.go` — rewrote `WriteShowHuman` for flat key/value format
- `docs/v1/constitution.md` — added `last_report_sync_at` and `last_report_hash` to meta.json docs
- `README.md` — updated slice 3 progress, push output example, show output example

### new files
- `docs/v1/s3/s3_prs/s3_pr04_report.md` — this report

## branch name and commit message

**branch:** `pr3/s3-pr04-polish-docs-sync`

**commit message:**
```
feat(push,show): polish output formatting + docs sync

complete slice 03 PR-04 by standardizing CLI output contracts:

push output changes:
- stdout success: exactly `pr: <url>` (removed created/updated distinction)
- stderr warnings: exact spec strings
  - worktree: "warning: worktree has uncommitted changes; pushing commits anyway"
  - report: "warning: report missing or empty; proceeding due to --force"
- stderr errors: exact spec templates
  - E_REPORT_INVALID: report missing or empty; use --force to push anyway
  - E_EMPTY_DIFF: no commits ahead of parent; make at least one commit
  - E_NO_ORIGIN: git remote 'origin' not configured
  - E_UNSUPPORTED_ORIGIN_HOST: origin host must be github.com

show output changes:
- rewrote human output to flat key/value format (no section headers)
- fields in spec order: run, title, repo, runner, parent, branch, worktree
- blank line separator
- fields: tmux, pr (combined url + number), timestamps, report_hash, status
- pr format: `pr: <url|none> (#<number|->)`
- missing values display as "none"

documentation updates:
- constitution.md: added last_report_sync_at and last_report_hash to meta.json docs
- README.md: updated slice 3 progress to complete, updated output examples

tests added:
- TestPushOutputFormat_ErrorMessages: verify error message templates
- TestPushOutputFormat_WarningMessages: verify warning string format
- TestPushOutputFormat_SuccessLine: verify stdout format
- TestWriteShowHuman_NoPR: verify pr: none (#-) format
- updated existing show tests for new format

implements spec: docs/v1/s3/s3_prs/s3_pr4.md
```
