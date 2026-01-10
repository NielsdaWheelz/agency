# Agency L1: Slice Roadmap (v1 MVP)

## Slice 0: Bootstrap (init + doctor + config validation)

- Outcome: user can initialize a repo and verify prerequisites deterministically.
- Demo:

```bash
cd myrepo
agency init
agency doctor
```

- Scope:
  - Find repo root (`git rev-parse --show-toplevel`).
  - Require `agency.json` at repo root; validate schema + required fields.
  - Define schema versioning rules (`agency.json`, `meta.json`, `events.jsonl`): additive only in v1; new required fields bump version; ignore unknown fields.
  - Resolve dirs (`AGENCY_DATA_DIR`, config, cache) with macOS Library defaults, XDG fallbacks, and env overrides; print them.
  - Verify `git`, `tmux`, `gh`, runner command exists; `gh auth status` OK.
  - Optionally parse `origin` and report "GitHub flow available: yes/no".
  - Write repo linkage files even for non-GitHub repos:
    - resolve `repo_id` from `origin` when parseable; fallback to path-based key
    - create `repo.json` under `${AGENCY_DATA_DIR}/repos/<repo_id>/`
    - update `${AGENCY_DATA_DIR}/repo_index.json` entries with `repo_id`, `paths`, `last_seen_at`
- Non-scope: worktrees, tmux sessions, PRs, scripts execution.
- Dependencies: none.
- Acceptance: `agency doctor` exits 0 only when slice-1 readiness is met (tools/scripts present + gh authenticated), regardless of GitHub flow availability; otherwise returns a specific error code + actionable message.
- Failure modes: missing gh auth; missing tmux; invalid `agency.json` -> exits non-zero with concrete fix.
- Risks/spikes: robust parsing of GitHub origin into `repo_key` (fallback to path-key).

## Slice 1: Run (create workspace + setup + tmux runner session)

- Outcome: `agency run` creates an isolated worktree and launches a real runner TUI in tmux (detach/attach works).
- Demo:

```bash
agency run --title "test run" --runner claude
agency ls
agency attach <id> # then detach with ctrl-b d
```

- Scope:
  - Require clean parent working tree (fail `E_PARENT_DIRTY`).
  - Create worktree under `${AGENCY_DATA_DIR}/repos/<repo_id>/worktrees/<run_id>`.
  - Create `.agency/` dirs in worktree; run setup script outside tmux (timeout 10m).
  - Create `.agency/report.md` on run with template (prefill title if provided).
  - Create tmux session `agency:<run_id>` with `cwd=worktree`, run runner command.
  - Persist `meta.json` for the run (title, runner, branch, worktree path, created_at).
  - Spike checklist (folded into implementation):
    - Create worktree.
    - Create tmux session with cwd in worktree.
    - Detach/attach.
    - Delete worktree and session safely.
- Non-scope: push/PR, merge, verify, transcript capture beyond basic tmux scrollback.
- Dependencies: Slice 0.
- Acceptance: after detach, `agency attach` shows the same live runner TUI; run metadata survives new shell invocation.
- Failure modes: setup fails -> run marked `setup_failed` and tmux not started; worktree retained for inspection.
- Risks/spikes: tmux session creation reliability + cross-shell behavior; safe "not inside a worktree" detection.

## Slice 2: Observability (ls/show + repo index + minimal transcript capture)

- Outcome: user can list runs, inspect a run, and capture transcripts/logs for debugging.
- Demo:

```bash
agency ls
agency show <id>
agency show <id> --path
```

- Scope:
  - Global run registry under `${AGENCY_DATA_DIR}` keyed by `repo_id`.
  - `ls` shows: id, title, runner, created_at, derived status, PR url (if any).
  - `show` prints full metadata + resolved paths + last script results.
  - Append-only `events.jsonl` for agency actions (run/setup/attach/etc.).
  - Capture script logs to `${AGENCY_DATA_DIR}/.../runs/<id>/logs/*.log`.
  - Implement repo-level lock behavior + stale detection; read-only commands bypass the lock.
- Non-scope: PR body sync, verify/merge, stop/kill/resume semantics.
- Dependencies: Slice 1.
- Acceptance: runs remain listable after terminal restarts; logs exist for setup failures.
- Failure modes: corrupted `meta.json` -> flagged as "broken run" with recovery instructions.
- Risks/spikes: repo move handling (`repo_index` maps `repo_key` -> seen paths; pick best existing path).

## Slice 3: Push (git push + gh pr create/update + report sync)

- Outcome: user can create/update a GitHub PR from a run with `agency push`, and sync `.agency/report.md` to PR body.
- Demo:
  - (In attached runner) create commits on the run branch.
  - `agency push <id>`.
  - Rerun `agency push <id>` (idempotent update).
  - `agency ls` (shows PR url).
- Scope:
  - All git/gh operations run with `-C <worktree_path>` (or `cwd=worktree`).
  - Require `origin` to exist and be a `github.com` remote (ssh or https).
  - `git fetch origin` (no rebases/resets).
  - Check `ahead_by_commits = rev-list --count parent..head > 0` else refuse `E_EMPTY_DIFF`.
  - `git push -u origin <branch>`.
  - Create PR via `gh pr create` if missing; else update body/title as needed.
  - PR identity: repo + head branch in origin (`gh pr view --head <branch>`).
  - Store PR url/number in run metadata; on update, prefer stored PR number, fallback to `--head`.
  - PR body source: `<worktree>/.agency/report.md` (warn if missing/empty; `--force` bypass).
- Non-scope: merge, verify gating, PR checks parsing, auto-close, auto-rebase.
- Dependencies: Slice 2.
- Acceptance: repeated pushes do not create duplicate PRs; report changes propagate to PR body.
- Failure modes: gh not authenticated; origin missing or not github.com -> error; push rejected -> surface stderr, keep run intact.
- Risks/spikes: robust mapping from branch -> existing PR (`gh pr view --head <branch>`).

## Slice 4: Lifecycle control (stop/kill/resume + flags)

- Outcome: user can manage runner lifecycle safely without merging or archiving.
- Demo:

```bash
agency stop <id> # runner interrupted; session remains
agency resume <id> # attach; ensures session exists
agency kill <id> # kill tmux only
```

- Scope:
  - Stop: tmux `send-keys C-c` best-effort; set `needs_attention`.
  - Kill: `tmux kill-session`; workspace persists.
  - Resume: if session missing -> create session + run runner; then attach unless `--detached`.
  - Resume `--restart`: kill session and recreate (explicit destructive action).
- Non-scope: verify, merge, archive/clean.
- Dependencies: Slice 3.
- Acceptance: stop/kill/resume behave deterministically; `needs_attention` set on stop.
- Failure modes: tmux missing -> fail with `E_TMUX_NOT_INSTALLED`.
- Risks/spikes: resume is intentionally conservative; no idle detection in v1.

## Slice 5: Merge + archive

- Outcome: user can finish the loop by merging and archiving.
- Scope:
  - Merge: ensure PR exists; check mergeable; run `scripts.verify` (timeout 30m) and record result; require human confirmation; `gh pr merge` with strategy flags.
  - Archive: run archive script (timeout 5m); delete worktree; delete tmux session; retain metadata/logs under `${AGENCY_DATA_DIR}/.../runs/<id>`.
  - Clean: archive without merging; mark abandoned.
- Non-scope: interactive TUI, auto-resolving conflicts, PR checks enforcement, retention GC.
- Dependencies: Slice 4.
- Acceptance: merge performs verify + explicit prompt + merges PR + deletes worktree + removes tmux session; run remains in history as merged/archived.
- Failure modes: verify fails -> user can abort or `--force`; merge conflicts/not mergeable -> fail with `E_PR_NOT_MERGEABLE`; archive script fails -> warn and continue best-effort cleanup.

## Gold-standard decisions (v1)

- `meta.json` public contract:
  - `run_id`, `repo_id`, `title`, `runner`, `parent_branch`, `branch`, `worktree_path`, `created_at`, `tmux_session_name`,
    `pr_number?`, `pr_url?`, `last_push_at?`, `last_verify_at?`, `flags(needs_attention, setup_failed, abandoned)`,
    `archive(archived_at, merged_at)`.
- Error code taxonomy: see constitution.
- Branch naming + slug rules: title empty OK; always include shortid to avoid collisions.
- Origin policy: `agency push`/`agency merge` require `github.com` origin; non-GitHub origins are rejected.
- Push behavior: always `git push -u origin <branch>` for agency-managed branches.
- `agency ls` scope:
  - default: runs for repo containing cwd, excluding archived
  - `--all`: include archived for current repo
  - `--all-repos`: show across repos (still exclude archived unless `--all`)
