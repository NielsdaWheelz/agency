# Slice 8 — PR Roadmap

**Goal:** Land slice 8 incrementally without ever breaking core invariants:

- Runners never touch integration trees
- Sandboxes are always isolated
- Landing is explicit and reversible
- State is always inspectable on disk

Each PR should be reviewable in ~15–30 minutes.

---

## PR-00 — Cobra Migration + Command Skeletons

**Purpose:** Unblock clean subcommand structure before adding behavior.

**Scope:**

- Add Cobra dependency
- Replace manual dispatch with Cobra root + subcommands
- Preserve existing commands (`run`, `ls`, etc.) as legacy wrappers
- Regenerate shell completions via Cobra

**Contains:**

- `cmd/agency/main.go` rewritten to Cobra root
- `agency worktree`, `agency agent`, `agency watch` registered (no logic yet)
- Legacy commands routed to old implementations

**Explicit non-goals:**

- No new storage
- No worktree logic

**Blast radius contract:**

- Help text and usage output **will change** (cobra formatting replaces handwritten usage)
- Shell completion scripts **will change** (cobra auto-generation replaces manual `__complete`)
- **Must not change:** command semantics, flag names, flag behavior, exit codes, JSON/structured output
- Validation: run existing test suite; any test asserting on help text may need updating, but tests asserting on command behavior must pass unchanged

**Acceptance:**

- [ ] `agency help` shows new command tree
- [ ] Existing v1 commands still work (same flags, same behavior, same exit codes)
- [ ] JSON output from `show --json`, `ls --json` unchanged
- [ ] Completions generate successfully

---

## PR-01 — V2 Store Layer + Integration Worktree Primitive

**Purpose:** Introduce the v2 store contracts (resolver, index, filtering) and integration worktrees as first-class records + directories.

**Scope:**

- New store paths: `Store.WorktreeDir`, `Store.WorktreeMetaPath`
- Name/ID/prefix resolver logic (shared by worktree, agent, watch)
- Directory scan strategy for worktree discovery
- Active vs archived filtering rules
- `worktree create|ls|show|path|open|shell|rm`
- `.agency/INTEGRATION_MARKER` enforcement

**Contains:**

- `worktrees/<worktree_id>/meta.json`
- Integration worktree creation via `git worktree add -b`
- Removal via existing `git worktree remove`
- Resolver: accepts name, worktree_id, or unique prefix → returns worktree record
- Resolution implementation: scan `worktrees/*/meta.json` (no index file needed at this scale; add `worktree_index.json` if scan becomes slow)
- Filtering: `ls` shows non-archived by default; `ls --all` includes archived
- Resolver tests covering: exact name, exact ID, unique prefix, ambiguous prefix (error), archived exclusion

**Explicit non-goals:**

- No agents
- No sandboxes
- No landing

**Acceptance:**

- [ ] Integration worktree can be created, opened, removed
- [ ] Resolver correctly handles name, ID, prefix, ambiguous prefix
- [ ] `ls` excludes archived; `ls --all` includes archived
- [ ] Name uniqueness enforced among non-archived worktrees
- [ ] INTEGRATION_MARKER written on create

---

## PR-02 — Sandbox Creation + Invocation Record

**Purpose:** Create per-invocation sandbox worktrees and canonical invocation records.

**Scope:**

- New store paths: `Store.InvocationDir`, `Store.SandboxDir`
- Invocation `meta.json` (canonical record for both invocation + sandbox state)
- Sandbox creation from integration branch

**Contains:**

- `agent start` creates:
  - Sandbox branch (`agency/sandbox-<invocation_id>`)
  - Sandbox worktree
  - Invocation `meta.json`
- `base_commit` captured at start
- Invocation resolver (reuses PR-01 resolver pattern)

**Explicit non-goals:**

- No runner execution yet
- No logging
- No checkpoints

**Integration tree protection (hard test gate):**

- [ ] **Test:** if sandbox creation fails, `agent start` aborts — never falls back to integration path
- [ ] **Test:** `agent start` checks for `INTEGRATION_MARKER` and refuses to run if resolved CWD contains it
- [ ] **Test:** sandbox tree does NOT contain `INTEGRATION_MARKER`

These tests are the highest-value invariant in the slice and must land in this PR, not later.

**Acceptance:**

- [ ] Starting an agent creates sandbox + invocation record
- [ ] Multiple sandboxes can exist per integration worktree
- [ ] Integration tree remains untouched
- [ ] All integration-tree-refusal tests pass

---

## PR-03 — Headed Runner Execution (tmux)

**Purpose:** Restore interactive runner functionality inside sandboxes.

**Scope:**

- Headed `agent start`
- tmux session creation
- Attach/stop/kill for headed invocations
- Invocation reaper for finished detection

**Contains:**

- tmux session named by `invocation_id`
- Runner launched with CWD = sandbox tree
- Invocation lifecycle updates (`starting` → `running` → `finished`)

**Invocation reaper (headed finished detection):**

Headed invocations don't have exit codes. "Finished" = tmux session no longer exists.

- Implement a lightweight `reconcileInvocationState()` function that:
  - Checks `tmux has-session -t <session_name>`
  - If session missing AND `status == "running"` → set `status = "finished"`, `finished_at = now`, `exit_reason = "exited"`
- Called lazily on every read path: `agent ls`, `agent show`, `watch` refresh
- No daemon, no background goroutine — reconcile on demand
- Must be idempotent (safe to call multiple times)

**Explicit non-goals:**

- No headless mode
- No log parsing
- No checkpoints

**Acceptance:**

- [ ] `agent attach` works
- [ ] Stopping/killing affects only sandbox
- [ ] tmux never runs in integration tree
- [ ] After tmux session exits, next `agent show`/`ls` correctly shows `finished`
- [ ] `finished_at` persisted on first reconciliation

---

## PR-04 — Headless Runner Execution + Raw Logging

**Purpose:** Enable non-interactive agents with full stdout/stderr capture.

**Scope:**

- Headless execution for:
  - Claude (`-p --output-format stream-json --verbose`)
  - Codex (`exec -C --json`)
- Structured log file contract
- Process lifecycle tracking

**Contains:**

- Subprocess runner execution
- PID tracking
- `exit_code` + `exit_reason` capture

**Log file contract:**

```
sandboxes/<invocation_id>/logs/
├── raw.jsonl       # Verbatim runner stdout (JSONL as emitted by claude/codex)
├── stderr.log      # Runner stderr (errors, warnings)
└── stream.jsonl    # Reserved for PR-05 (normalized events)
```

- `raw.jsonl` = verbatim copy of runner stdout, appended as chunks arrive
- `stderr.log` = runner stderr, appended as chunks arrive
- `last_output_at` updated on **every chunk** written to `raw.jsonl` (not batched)
- `stream.jsonl` is NOT written in this PR — reserved for PR-05 parsing

**Note:** Both Claude and Codex emit structured JSONL on stdout. `raw.jsonl` captures this verbatim. `stderr.log` captures process-level errors that are separate from the JSONL stream.

**Explicit non-goals:**

- No semantic parsing yet (that's PR-05)
- No checkpoints
- No watch

**Acceptance:**

- [ ] Headless agents run to completion
- [ ] `raw.jsonl` contains verbatim runner output
- [ ] `stderr.log` captures stderr separately
- [ ] `last_output_at` updates on every chunk
- [ ] Invocation marked `finished`/`failed` correctly

---

## PR-05 — Stream Parsing + Semantic Status (Headless Only)

**Purpose:** Derive meaningful status from headless runner output without runner cooperation.

**Scope:**

- Parse JSONL streams for Claude + Codex (headless only)
- Normalized event records
- Semantic status inference

**Contains:**

- Read `raw.jsonl`, write normalized events to `stream.jsonl`
- Normalized internal event representation
- Derived statuses: `working`, `needs_input`, `blocked`, `ready_for_review`

**Scope boundary:** Headed invocations are excluded. Headed status relies on tmux presence + reaper (PR-03). Do not attempt to parse headed output.

**Explicit non-goals:**

- No UI
- No watch
- No checkpoints
- No headed stream parsing

**Acceptance:**

- [ ] Semantic status updates during headless runs
- [ ] Fallback to lifecycle status if parsing fails
- [ ] No crashes on unknown event types

---

## PR-06 — Checkpointing via Private Refs + User Commands

**Purpose:** Make sandbox work reversible, safe, and user-inspectable.

**Scope:**

- Per-sandbox checkpoint engine
- Private refs under `refs/agency/snapshots/...`
- fsnotify + fallback polling
- User-facing checkpoint commands

**Contains:**

- Snapshot creation using temp index + `commit-tree`
- Denylist handling
- Rollback helper
- Checkpoint records in `checkpoints.json`
- `agency checkpoint ls --invocation <id|prefix>` — list checkpoints for an invocation
- `agency checkpoint apply --invocation <id|prefix> <checkpoint_id>` — restore sandbox to checkpoint state

**Explicit non-goals:**

- No watch integration
- No auto-rollback

**Acceptance:**

- [ ] Snapshots created during sandbox activity
- [ ] Rollback restores exact state
- [ ] No interference across sandboxes
- [ ] E2E test: edit file → snapshot created → modify again → `checkpoint apply` → file content restored
- [ ] `checkpoint ls` shows checkpoint history with timestamps and diffstats

---

## PR-07 — Landing Workflow (diff / land / discard)

**Purpose:** Safely move sandbox results into integration worktree.

**Scope:**

- `agent diff`
- `agent land` (cherry-pick only)
- `agent discard`

**Contains:**

- Diff between `base_commit` and sandbox tip (two-dot)
- Cherry-pick landing onto current integration HEAD
- Conflict detection + abort (sandbox preserved)
- Sandbox cleanup on success

**No-commits handling:**

Runners often modify files without committing. `agent land` must handle this:

- If commit range `<base_commit>..<sandbox_branch>` is **non-empty**: cherry-pick as normal
- If commit range is **empty** but working tree is dirty: `agent land` uses `--apply` mode:
  - `git diff <base_commit> -- . | git apply` on integration tree
  - Create a single commit on integration: `"agency: land invocation <invocation_id>"`
- If commit range is empty AND working tree is clean: error — nothing to land

**Merge strategy: deferred.** Cherry-pick is the only landing strategy in slice 8. Merge pulls sandbox branch history into integration, which is a footgun for this model. Defer `--strategy merge` unless a compelling use case emerges.

**Explicit non-goals:**

- No auto-land
- No rebase support
- No merge strategy (deferred)

**Acceptance:**

- [ ] Landing applies only sandbox changes
- [ ] Conflicts do not corrupt integration (abort + sandbox preserved)
- [ ] Discarded sandboxes clean up fully
- [ ] Test: land with commits (cherry-pick works)
- [ ] Test: land with no commits but dirty tree (apply mode works)
- [ ] Test: land with no commits and clean tree (error with hint)

---

## PR-08 — Watch (TUI)

**Purpose:** Give global visibility and control.

**Scope:**

- `agency watch`
- Hierarchical view: worktrees → invocations
- Keyboard actions

**Contains:**

- Bubbletea TUI
- Polling-based refresh
- Actions: attach, logs, land, discard, stop/kill, open worktree

**Architectural constraint:** Watch is a **read-only client** of the store and a **caller** of existing commands. It must not:

- Perform direct git operations
- Write to meta.json files directly
- Implement business logic that duplicates command implementations

All mutations go through the same code paths as the CLI commands (`agent land`, `agent stop`, etc.). Watch is a UI shell over existing APIs.

**Explicit non-goals:**

- No editing
- No config UI
- No direct git mutation

**Acceptance:**

- [ ] Watch reflects live state
- [ ] Actions route to existing commands (no reimplemented logic)
- [ ] No crashes on rapid updates

---

## PR-09 — Legacy Wrapper + Polish

**Purpose:** Restore "happy path" and stabilize UX.

**Scope:**

- Redefine `agency run` as wrapper:
  1. `worktree create`
  2. `agent start` (headed)
- Help text cleanup
- Error message polish
- Docs updates

**Acceptance:**

- [ ] `agency run` works end-to-end
- [ ] New mental model documented
- [ ] No dangling TODOs in code

---

## Optional Follow-ups (Post-Slice)

- Auto-rename branches
- Auto-land policies
- Sandbox GC
- Richer diff UI
- `--strategy merge` for landing (if compelling use case)
