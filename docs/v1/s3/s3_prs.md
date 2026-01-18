# Agency L3: Slice 03 — PR Roadmap (push + PR create/update + report sync)

goal: implement `agency push <run_id>` end-to-end with tight scope, high testability, and minimal accidental feature creep.

principle: keep gh/network behaviors behind a mockable command runner. each PR should be reviewable in isolation and should not “half-implement” PR creation.

---

## PR-1 — core plumbing for push (errors, meta fields, gh/git exec abstraction)

### goal
add the internal foundations needed to implement `agency push` without touching git/gh behavior yet.

### scope
- add new error codes used by slice 03:
  - `E_UNSUPPORTED_ORIGIN_HOST`, `E_NO_ORIGIN`, `E_PARENT_NOT_FOUND`, `E_GIT_PUSH_FAILED`,
    `E_GH_PR_CREATE_FAILED`, `E_GH_PR_EDIT_FAILED`, `E_REPORT_INVALID`
- extend `meta.json` schema (additive):
  - `last_push_at`, `last_report_sync_at`, `last_report_hash`
- add append-only `events.jsonl` helper:
  - typed event writer with a minimal schema: event name, timestamp, run_id, repo_id, optional data map
- introduce mockable command execution interface:
  - `CmdRunner` that runs `git`/`gh` with:
    - cwd
    - args
    - env overlay
    - stdout/stderr capture
    - exit code
- repo lock integration point:
  - ensure `push` will take the repo lock (no behavior change yet)

### public surface area
none (internal only). no new commands yet.

### acceptance tests
- unit tests:
  - error code mapping table exists and is stable (no missing codes)
  - meta read/write roundtrip preserves new optional fields
  - events writer appends valid json lines

### guardrails
- do not implement `agency push` behavior yet
- no git fetch/push; no gh calls

---

## PR-2 — preflight + git fetch/ahead/push + report gating (no gh yet)

### goal
implement all deterministic preflight checks for `agency push`, validate/report handling, and complete the git push path.

### scope
- add `agency push <run_id> [--allow-dirty] [--force]` command
- load run metadata and resolve:
  - worktree path
  - parent branch
  - workspace branch
- origin rules:
  - require `origin` exists else `E_NO_ORIGIN`
  - parse origin url host; if not exactly `github.com` => `E_UNSUPPORTED_ORIGIN_HOST`
- report gating:
  - read `<worktree>/.agency/report.md`
  - “effectively empty” = missing OR trimmed length < 20
  - if empty and no `--force` => `E_REPORT_INVALID`
  - if `--force` => continue (but record warning path in events)
- dirty worktree check:
  - if `git status --porcelain --untracked-files=all` non-empty and no `--allow-dirty` => `E_DIRTY_WORKTREE`
  - if `--allow-dirty` => warn, continue
- gh auth gate:
  - `gh auth status` must succeed else `E_GH_NOT_AUTHENTICATED`
- non-interactive enforcement for all git/gh subprocesses invoked by `agency push`:
  - set env `GIT_TERMINAL_PROMPT=0`
  - set env `GH_PROMPT_DISABLED=1`
  - set stdin to `/dev/null` in the code path (not only in runner defaults)
- git push path:
  - `git fetch origin` (non-destructive)
  - resolve `parent_ref`:
    - prefer `<parent_branch>` if exists
    - else use `origin/<parent_branch>` if exists post-fetch
    - else `E_PARENT_NOT_FOUND`
  - compute `ahead = git rev-list --count <parent_ref>..<workspace_branch>`
  - if `ahead == 0` => `E_EMPTY_DIFF` (even with `--force`)
  - `git push -u origin <workspace_branch>` (no force push)
- append events:
  - `push_started`, `git_fetch_finished`, `git_push_finished`, `push_failed` (as applicable)
- persist on success:
  - `last_push_at`

### public surface area
`agency push` now pushes the branch but does not create/update PRs yet.

### acceptance tests
- unit tests (table-driven) with mocked `CmdRunner`:
  - origin host parsing
  - report gating + `--force`
  - gh auth failure -> correct error
  - dirty worktree without `--allow-dirty` fails with `E_DIRTY_WORKTREE`
  - dirty worktree with `--allow-dirty` produces warning (assert warning text)
  - parent-ref selection logic
  - ahead==0 => `E_EMPTY_DIFF`
  - push command invoked with `-u`
  - push failure surfaces stderr + `E_GIT_PUSH_FAILED`
- integration test (local-only):
  - create bare remote as `origin`
  - create commits in workspace branch
  - `agency push` results in branch present in bare remote

### guardrails
- still no `gh pr create/edit/view`
- no rebase/reset/merge
- no title/body formatting decisions beyond warnings

---

## PR-3 — gh PR idempotency + create + body sync + metadata persistence + show output

### goal
finish slice 03: create/update PR and sync report to PR body, idempotently.

### scope
- PR lookup order:
  1) if `meta.pr_number` present: `gh pr view <number> --json number,url,state`; if ok use it; else fallback
  2) `gh pr view --head <workspace_branch> --json number,url,state` (branch lookup)
  3) if none: create PR
- PR creation:
  - `gh pr create --base <parent_branch> --head <workspace_branch> --title "[agency] <title>"`
  - body:
    - if report exists and non-empty: `--body-file <report>`
    - else (only with `--force`): minimal placeholder `--body ...`
  - do not parse output from `gh pr create`
  - after create, fetch metadata via `gh pr view --head <workspace_branch> --json number,url,state`
  - if `state != "OPEN"` (including `CLOSED` or `MERGED`), fail `E_PR_NOT_OPEN`
- PR update:
  - if PR exists and report non-empty: `gh pr edit <pr_number> --body-file <report>`
  - do not update title after creation (v1)
  - if report hash unchanged vs `meta.last_report_hash`, skip edit (no event, no timestamp update)
- persist:
  - `pr_number`, `pr_url`
  - `last_report_sync_at`, `last_report_hash` (only when body synced)
- append events:
  - `pr_created`, `pr_body_synced`, `push_finished`
- `agency show` prints:
  - PR url/number
  - last push timestamp
  - last report sync timestamp

### public surface area
`agency push` now fully conforms to slice 03.

### acceptance tests
- unit tests with mocked `CmdRunner`:
  - meta pr_number success path
  - meta pr_number fails -> branch fallback
  - branch fallback none -> create path
  - edit called when report present
  - correct error mapping:
    - `E_GH_PR_CREATE_FAILED`, `E_GH_PR_EDIT_FAILED`
    - `E_GH_PR_VIEW_FAILED`, `E_PR_NOT_OPEN`
- manual acceptance (documented steps):
  - github repo, auth ok
  - create commit, push => PR created, url printed
  - edit report, push => PR body updated
  - rerun push => no duplicate PR

### guardrails
- do not implement merge, verify, PR checks parsing, drafts, forks/upstream PRs

---

## PR-4 — polish + docs sync

### goal
make the feature legible and maintainable: better output and docs updated.

### scope
- update documentation:
  - `docs/v1/constitution.md` (if it references old error list / fields)
  - add slice 03 notes if you keep a slices index
- improve user messages:
  - on `E_NO_PR` isn’t relevant here; but add:
    - “report missing; use --force to push anyway”
    - “empty diff; make at least one commit”
- finalize `agency show` formatting for PR fields, report hash, and status display

### acceptance tests
- snapshot-ish test of `agency show` formatting (keep loose)
- ensure docs build/lint passes if you have it

### guardrails
- no behavior changes to git/gh logic; only UX + docs + display
- guardrails are behavioral (not path-based):
  - do not change git/gh command argument construction
  - do not change when meta fields are written
  - moving output formatting into a shared helper and small callsite edits are allowed
