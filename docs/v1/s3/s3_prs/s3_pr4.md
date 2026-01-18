# Agency L4: Slice 03 — PR-4 Spec (polish + docs sync)

## goal
make slice 03 legible and stable: align docs with implemented behavior and provide consistent, script-friendly CLI output for `agency push` and `agency show` **without changing** any git/gh execution behavior.

## scope
- documentation updates to reflect new error codes + meta fields introduced/used by slice 03
- consistent output formatting for:
  - `agency push <run_id>` success / warnings / errors
  - `agency show <run_id>` display (including PR + timestamps + report hash)
- minimal tests that assert output structure using substring/regex checks (no snapshots)

## non-scope
- any changes to git/gh invocation logic, argument building, or command execution
- any changes to meta persistence semantics (when/how fields are set)
- any changes to PR creation/update policy (title/body)
- adding new flags (`--json`, `--no-report-sync`, etc.)
- adding new docs files (only modify existing docs)

## public surface area changed
- user-visible stdout/stderr formatting only (no new commands/flags)

## files allowed to change (hard allowlist)
- docs:
  - `docs/v1/constitution.md`
  - (optional) existing slice docs index if present (only update if already exists; do not create new docs)
- cli formatting / rendering (choose the actual paths used in repo; examples):
  - `cmd/agency/*.go` (only formatting paths)
  - `internal/cli/*` or `internal/ui/*` (only formatting paths)
- tests for formatting:
  - `**/*_test.go` in the same packages as output formatting

## files explicitly disallowed (hard denylist)
(do not touch these even “a little”)
- `internal/git/**`
- `internal/gh/**`
- `internal/push/**`
- any code that changes:
  - meta read/write logic beyond display-only accessors
  - `CmdRunner` / command execution semantics
  - repo locking semantics

## required behavior: `agency push` output contract

### stdout (success)
on success, print exactly one line to stdout:
- `pr: <url>`

where `<url>` is the GitHub PR URL resolved via `gh pr view --json url`.

no other stdout output on success.
if any informational output currently goes to stdout, move it to stderr in this PR to satisfy the contract.

### stderr warnings (prefix)
all warnings MUST be printed to stderr and MUST begin with:
- `warning: `

required warning messages (exact strings):

1) dirty worktree (only when `--allow-dirty` is provided):
- `warning: worktree has uncommitted changes; proceeding due to --allow-dirty`

2) report missing/empty but continuing (only when `--force` is provided):
- `warning: report missing or empty; proceeding due to --force`

### stderr errors (format)
on failure, stderr MUST begin with:
- `<ERROR_CODE>: <short message>`

optionally followed by a second line:
- `hint: <actionable command>`

required error message templates (exact strings):

- `E_REPORT_INVALID: report missing or empty; use --force to push anyway`
- `E_EMPTY_DIFF: no commits ahead of parent; make at least one commit`
- `E_NO_ORIGIN: git remote 'origin' not configured`
- `E_UNSUPPORTED_ORIGIN_HOST: origin host must be github.com`
- `E_DIRTY_WORKTREE: worktree has uncommitted changes; use --allow-dirty to proceed`

other error codes may follow the same format; do not reword the above.
all other errors must still use the same `<ERROR_CODE>: <message>` first-line format and must not print stack traces by default.
when `git`/`gh` fail, surface their stderr verbatim after the first line (do not alter it).

## required behavior: `agency show` output contract

`agency show <id>` prints plain key/value lines to stdout in the exact order below.
values are rendered as:
- when PR url missing AND number missing: `pr: none (#-)`
- when PR url present AND number missing: `pr: <url> (#-)`
- when both present: `pr: <url> (#<number>)`

format (exact labels and order):

run: <run_id>
title: 
repo: <repo_id>
runner: 
parent: <parent_branch>
branch: 
worktree: <worktree_path>

tmux: <tmux_session_name|none>
pr: <pr_url|none> (#<pr_number|->)
last_push_at: <rfc3339|none>
last_report_sync_at: <rfc3339|none>
report_hash: <sha256|none>
status: <derived_status>

notes:
- there MUST be a blank line between `worktree:` and `tmux:`.
- `status:` uses whatever the current derived status logic returns; do not change derivation in this PR.
- tests should only assert that the `status:` label exists, not its exact value.

## docs updates

### `docs/v1/constitution.md`
ensure `constitution.md` matches the implemented public contract:

1) error codes list includes slice-03 errors:
- `E_UNSUPPORTED_ORIGIN_HOST`
- `E_NO_ORIGIN`
- `E_PARENT_NOT_FOUND`
- `E_GIT_PUSH_FAILED`
- `E_GH_PR_CREATE_FAILED`
- `E_GH_PR_EDIT_FAILED`
- `E_REPORT_INVALID`

2) `meta.json` optional fields include (with terse definitions):
- `last_push_at` — set on successful `agency push`
- `last_report_sync_at` — set when PR body updated from report
- `last_report_hash` — sha256 of report contents when synced

3) confirm `agency ls` scope defaults (if present) remain:
- current repo only, excluding archived
- `--all` includes archived
- `--all-repos` shows across repos

do not add new sections; update existing ones.

## exit codes (do not change behavior)
documented expectation (do not modify implementation if already present):
- exit code `0` on success
- exit code `1` on operational errors (typed error codes)
- exit code `2` on usage errors (bad args)

## tests

### unit tests (required)
add/adjust tests to validate output structure without snapshots.

1) `agency show` formatting:
- when PR fields are missing -> output contains `pr: none (#-)`
- when PR fields exist -> output contains `pr: https://` and `(#<number>)`
- output contains the blank line between `worktree:` and `tmux:`
- output includes all labels in correct order (use index comparisons on substrings)
- output includes the `status:` label (do not assert its exact value)

2) `agency push` formatting:
- success prints exactly one stdout line starting with `pr: `
- dirty worktree warning uses exact string (stderr contains it, with `--allow-dirty`)
- report force warning uses exact string (stderr contains it)
- error messages for the four specified templates match exactly
- other errors still follow `<ERROR_CODE>: <message>` on the first stderr line

tests MUST:
- avoid golden files
- use substring/regex and ordering checks
- not require network access

## guardrails
- no behavior changes to git/gh logic or meta persistence
- do not change git/gh command argument construction
- do not change when meta fields are written
- moving output formatting into a shared helper and small callsite edits are allowed
- no new flags beyond `--allow-dirty`
- no new docs files
- keep PR small; if you find a real behavior bug, write it down and stop—do not fix it here

## how to verify manually
1) run a slice-03 workspace with a PR already created
2) run `agency push <id>` and confirm stdout is only `pr: <url>`
3) make worktree dirty (edit a file without committing), run push, confirm dirty warning
4) empty or delete `.agency/report.md`, run:
   - `agency push <id>` -> `E_REPORT_INVALID` template
   - `agency push <id> --force` -> force warning, succeeds
5) run `agency show <id>` and confirm fields render as specified
