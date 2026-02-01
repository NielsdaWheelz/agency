# Slice 8 — PR Roadmap

**Goal:** Land slice 8 incrementally without ever breaking core invariants:

- Runners never touch integration trees
- Sandboxes are always isolated
- Landing is explicit and reversible
- State is always inspectable on disk

PRs 00–09 should be reviewable in ~15–30 minutes each. PRs 10–14 are larger architectural migrations and may require splitting further.

> **Daemon migration rule (slice 8):**
> PR-04 introduces **daemon-as-supervisor for headless only**.
> PR-05 migrates to **daemon-as-control-plane** for headless, moving sandbox/invocation creation behind daemon RPC.
> After PR-05, CLI must not write headless invocation or sandbox store files.
> After PR-06, worktree creation is daemon-owned.
> After PR-10, headed invocation creation is daemon-owned — all invocation + worktree creation flows through daemon.
> After PR-12, CLI must not read v2 store files either (all reads go through daemon).
> After PR-13, watch uses daemon event stream — no filesystem polling.

### Parity Rules

These rules define the **target architecture** for slice 8. Each PR moves closer; they are all satisfied after PR-14.

**Rule A — Daemon is single writer for all v2 objects.** Only daemon writes: worktree meta, invocation meta, sandbox artifacts (creation/deletion), `checkpoints.json`, `events.jsonl`, snapshot refs, logs dir. CLI writes nothing to the store for v2 flows.

**Rule B — Daemon is the read authority.** CLI must not scan directories, parse meta.json files, derive statuses, or reconcile state on read paths. Daemon does all read/derive; CLI only renders.

**Rule C — Watch uses daemon event stream.** Watch subscribes to a daemon event stream (state changes + optional log tail). On reconnect or cursor gaps, watch performs a full resync via daemon read endpoints. No direct filesystem polling. Daemon is the event source.

**Rule D — Stable, versioned IPC contract.** Unix socket. Versioned API. Daemon exposes `api_version` and `build_version`. CLI refuses incompatible daemon versions.

**Rule E — Daemon has repo registry.** Daemon can operate without the client's CWD. This unlocks remote/phone/termux clients.

### What You Still Don't Need (Even for Parity)

- gRPC (HTTP over unix socket is sufficient)
- Containers
- systemd/launchd installation (not required; architecture should not preclude it)
- Remote network listener (keep unix socket; remote via ssh/tailscale tunnel later)

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

## PR-04 — Daemon Supervisor for Headless + Raw Logging

**Purpose:** Enable detached headless invocations with correct lifecycle and streaming logs, without refactoring existing CLI creation paths yet.

**Scope:**

- `agency daemon start|status|stop` (same binary; no `run` — "run" already means something in agency)
- Unix socket `${AGENCY_DATA_DIR}/agencyd.sock` (0600)
- `daemon stop` sends SIGTERM via pidfile (no HTTP shutdown endpoint in v0)
- Daemon API:
  - `POST /invocations/{id}/start_headless`
  - `POST /invocations/{id}/stop`
  - `POST /invocations/{id}/kill`
  - `GET /health` → returns `api_version`, `build_version`, `daemon_pid`, `uptime`
- CLI continues to create:
  - Invocation meta (PR-02 behavior)
  - Sandbox worktree (PR-02 behavior)

**Daemon state directory:**

All daemon state files live under `${AGENCY_DATA_DIR}/`:

- **Pidfile:** `${AGENCY_DATA_DIR}/agencyd.pid` — contains daemon PID as decimal text, newline-terminated. Written on startup, removed on clean shutdown.
- **Socket:** `${AGENCY_DATA_DIR}/agencyd.sock` — unix socket (0600). Removed on clean shutdown.
- **Daemon log:** `${AGENCY_DATA_DIR}/agencyd.log` — optional, daemon stderr/diagnostic output. Not invocation logs. Rotated by size or external tooling.
- **Stale detection:** On startup, if pidfile exists: read PID, check `kill(pid, 0)`. If process is dead, remove stale pidfile + socket and proceed. If process is alive, refuse to start with `E_DAEMON_ALREADY_RUNNING`.

**Writer boundary (PR-04 split-brain contract):**

- CLI creates invocation meta with `status=starting` and sandbox worktree
- Daemon refuses to start if meta or sandbox is missing, or markers are wrong
- Once daemon accepts the invocation, daemon is the **sole writer** of lifecycle fields (`status`, `pid`, `exit_code`, `finished_at`, `last_output_at`, `exit_reason`)
- CLI must not write these fields after handing off to daemon

- Daemon does:
  - Validates sandbox marker + refuses integration marker + prefix-checks sandbox path against store
  - Creates logs dir
  - Writes prompt copy + sha into invocation dir
  - Spawns process group (`Setpgid=true`, signals via `-pgid`)
  - Streams stdout → `raw.jsonl`, stderr → `stderr.log` (append-only)
  - Updates invocation meta (`pid`/`status`/`exit_code`/`finished_at`/`last_output_at`) with throttling (in-memory every chunk, disk at most once per 500ms)
  - Recovery scan on startup (pid dead → mark `failed`/`unknown`; pid alive → mark `needs_attention`)

**Log file contract:**

```
sandboxes/<invocation_id>/logs/
├── raw.jsonl       # Verbatim runner stdout (JSONL as emitted by claude/codex)
├── stderr.log      # Runner stderr (errors, warnings)
└── stream.jsonl    # Reserved for PR-07 (normalized events)
```

**Explicit non-goals:**

- Daemon does NOT create sandboxes or invocation metas (that's PR-05)
- No single-writer-for-everything (not yet)
- No semantic parsing (PR-07)
- No checkpoints
- No watch

**Acceptance:**

- [ ] Headless agents run to completion via daemon
- [ ] Detached headless works after CLI exits
- [ ] `raw.jsonl` contains verbatim runner output
- [ ] `stderr.log` captures stderr separately
- [ ] `last_output_at` updates in-memory on every chunk, persisted at most once per 500ms
- [ ] Invocation marked `finished`/`failed` correctly
- [ ] Daemon refuses to start if sandbox is missing marker or contains integration marker
- [ ] Daemon refuses if sandbox_path is not under store-computed path
- [ ] Recovery scan on daemon restart marks orphaned invocations `failed`/`unknown`
- [ ] Pidfile written on startup, removed on clean shutdown
- [ ] Stale pidfile detected and cleaned up on startup

---

## PR-05 — Daemon Control Plane for Headless Start (Creates Sandbox + Meta)

**Purpose:** Complete the architectural cut: for headless invocations, the daemon becomes the creator and single writer from birth.

**Scope:**

- New daemon endpoint:
  - `POST /invocations/start_headless`
    - Inputs: integration worktree ref (name/id), runner, prompt (string), runner_args, env_overrides
    - Outputs: invocation_id + sandbox_path
- Daemon responsibilities (headless path only):
  - Resolve repo + integration worktree from store
  - Capture `base_commit`
  - Create sandbox worktree + branch
  - Create invocation meta (`status=starting`)
  - Create logs dir + prompt copy
  - Start process + stream logs
  - Update meta lifecycle fields
- CLI changes:
  - `agency agent start --headless` becomes RPC call
  - CLI no longer generates `invocation_id` or `sandbox_branch` name for headless
  - CLI no longer creates sandbox worktree or invocation meta for headless
  - Daemon returns `invocation_id` and `sandbox_path`; CLI prints them and returns
  - stop/kill route through daemon (already in PR-04)
- Compatibility:
  - Headed path remains CLI-owned
  - Existing invocations created pre-PR-05 (by CLI in PR-04 split-brain mode) remain manageable by daemon
  - No migration needed: daemon accepts both daemon-created and CLI-created invocations for stop/kill

**Explicit non-goals:**

- No daemon ownership for headed tmux yet
- No landing/checkpoints/watch

**Acceptance:**

- [ ] Headless start works with only integration worktree id/name + prompt
- [ ] No headless code path writes store files in CLI
- [ ] Daemon rejects any attempt to run in integration tree (marker guard)
- [ ] CLI crash mid-start cannot create half-baked sandbox/meta (daemon handles creation atomically)

---

## PR-06 — Daemon Owns Integration Worktree Mutations *(required for parity — Rule A)*

**Purpose:** `worktree create`/`rm` becomes daemon RPC, achieving single-writer for the top-level store objects early. This minimizes duplication of repo/worktree resolution logic across later PRs.

**Scope:**

- Daemon endpoints:
  - `POST /worktrees/create` — accepts `repo_root` (absolute path); daemon derives `repo_id` and registers the repo if not already known (early foundation for PR-14 repo registry)
  - `POST /worktrees/{id}/rm`
- Daemon does `git worktree add`/`remove`, writes worktree `meta.json`
- CLI becomes thin client for worktree mutations
- Read-only commands (`worktree ls`/`show`/`path`) remain CLI-local (migrated to daemon in PR-12)

**Why here:** Integration trees are the top-level objects. Checkpoints and landing (PR-08/PR-09) need stable daemon ownership of repo/worktree resolution. Moving this before those PRs prevents rewriting resolution logic twice.

**Repo registration:** `POST /worktrees/create` requires `repo_root` and registers it with the daemon's in-memory repo set. This is the foundation for PR-14's full repo registry — no new endpoint needed, just a side effect of worktree creation.

**Acceptance:**

- [ ] `worktree create` and `rm` route through daemon
- [ ] CLI does not write worktree `meta.json` directly
- [ ] Existing worktrees remain accessible
- [ ] Daemon learns `repo_root` on first `worktrees/create` call

---

## PR-07 — Stream Parsing + Semantic Status (Headless, Daemon-written raw.jsonl)

**Purpose:** Derive meaningful status from headless runner output without runner cooperation. Parsing runs inside daemon in-process while streaming.

**Scope:**

- Parse JSONL streams for Claude + Codex (headless only)
- Normalized event records
- Semantic status inference
- Daemon performs parsing in-process while streaming (or immediately after), writing `stream.jsonl`
- CLI only reads `stream.jsonl`

**Contains:**

- Daemon reads stdout, writes `raw.jsonl` verbatim, and simultaneously writes normalized events to `stream.jsonl`
- Normalized internal event representation
- Derived statuses: `working`, `needs_input`, `blocked`, `ready_for_review`

**Scope boundary:** Headed invocations are excluded. Headed status relies on tmux presence + reaper (PR-03). Do not attempt to parse headed output.

**Explicit non-goals:**

- No UI
- No watch
- No checkpoints
- No headed stream parsing
- No CLI parsing of `raw.jsonl` (daemon owns this)

**Acceptance:**

- [ ] Semantic status derived live for headless invocations
- [ ] `stream.jsonl` written by daemon, not CLI
- [ ] Fallback to lifecycle status if parsing fails
- [ ] No crashes on unknown event types
- [ ] No CLI parsing of raw.jsonl

---

## PR-08 — Daemon-owned Checkpoint Engine (Private Refs) + CLI Commands

**Purpose:** Make sandbox work reversible, safe, and user-inspectable. Checkpoint engine runs inside daemon.

**Scope:**

- Daemon runs fsnotify + fallback polling on sandbox tree
- Daemon writes checkpoint refs + `checkpoints.json`
- Private refs under `refs/agency/snapshots/...`
- User-facing CLI checkpoint commands route through daemon

**Contains:**

- Daemon: snapshot creation using temp index + `commit-tree`
- Daemon: denylist handling (degrade to tracked-only, not skip)
- Daemon: fsnotify watcher + periodic fallback polling per active sandbox
- Daemon endpoint:
  - `POST /invocations/{id}/checkpoints/apply` — restore sandbox to checkpoint state (mutation → daemon)
- CLI commands:
  - `agency checkpoint ls --invocation <id|prefix>` — reads `checkpoints.json` directly (read-only, no daemon call needed; migrates to daemon read API in PR-12)
  - `agency checkpoint apply --invocation <id|prefix> <checkpoint_id>` — calls daemon endpoint (mutation)

**Parity note:** After PR-12, `checkpoint ls` must also route through daemon (Rule B). In PR-08, direct file read is acceptable as an interim step.

**Explicit non-goals:**

- No watch integration
- No auto-rollback

**Acceptance:**

- [ ] Snapshots created during sandbox activity by daemon
- [ ] Rollback restores exact state via daemon endpoint
- [ ] No interference across sandboxes
- [ ] E2E test: edit file → snapshot created → modify again → `checkpoint apply` → file content restored
- [ ] `checkpoint ls` shows checkpoint history with timestamps and diffstats
- [ ] Denylisted untracked files degrade checkpoint to tracked-only (not skip)

---

## PR-09 — Landing Workflow via Daemon (diff / land / discard)

**Purpose:** Safely move sandbox results into integration worktree. Landing mutations flow through daemon to centralize git operations.

**Scope:**

- CLI `agent land`/`agent discard` call daemon endpoints (mutations → daemon)
- CLI `agent diff` remains CLI-local (read-only git operations, no daemon call needed; migrates to daemon read API in PR-12)
- Daemon performs: cherry-pick/apply, sandbox cleanup, invocation meta status update

**Daemon endpoints:**

- `POST /invocations/{id}/land` — cherry-pick or apply, cleanup sandbox
- `POST /invocations/{id}/discard` — stop if running, cleanup sandbox

**Contains:**

- Diff between `base_commit` and sandbox tip (two-dot)
- Cherry-pick landing onto current integration HEAD
- Conflict detection + abort (sandbox preserved)
- Sandbox cleanup on success
- Patch file written to sandbox's own `tmp/` directory (not `/tmp`)

**No-commits handling:**

Runners often modify files without committing. `agent land` must handle this:

- If commit range `<base_commit>..<sandbox_branch>` is **non-empty**: cherry-pick as normal
- If commit range is **empty** but working tree is dirty: `agent land` uses `--apply` mode:
  - `git diff <base_commit> -- .` → patch to sandbox `tmp/land.patch`
  - `git apply --index` on integration tree
  - Create a single commit on integration: `"agency: land invocation <invocation_id>"`
- If commit range is empty AND working tree is clean: error — nothing to land

**Merge strategy: deferred.** Cherry-pick is the only landing strategy in slice 8.

**Explicit non-goals:**

- No auto-land
- No rebase support
- No merge strategy (deferred)

**Acceptance:**

- [ ] Landing applies only sandbox changes via daemon
- [ ] Conflicts do not corrupt integration (abort + sandbox preserved)
- [ ] Discarded sandboxes clean up fully
- [ ] Test: land with commits (cherry-pick works)
- [ ] Test: land with no commits but dirty tree (apply mode works)
- [ ] Test: land with no commits and clean tree (error with hint)
- [ ] CLI does not perform git mutations directly for landing

---

## PR-10 — Daemon Starts Headed tmux Sessions *(required for parity — Rule A)*

**Purpose:** Move headed session creation into daemon. CLI only attaches.

**Scope:**

- `agent start` (headed) becomes daemon request
- Daemon creates tmux session in sandbox CWD
- Daemon creates sandbox + invocation meta for headed (mirrors PR-05 pattern)
- CLI only calls `tmux attach` (no session creation)
- Daemon updates meta on start
- stop/kill for headed invocations route through daemon

**Acceptance:**

- [ ] Headed `agent start` routes through daemon
- [ ] CLI never creates tmux sessions directly
- [ ] stop/kill work for headed via daemon

---

## PR-11 — Daemon Reconciliation for Headed Invocations *(required for parity — Rule B)*

**Purpose:** Move tmux finished-state detection from on-read reaper into daemon. After this PR, CLI read paths have zero side effects.

**Scope:**

- Daemon periodically checks `tmux has-session` for active headed invocations
- Daemon updates meta when tmux session disappears (`status=finished`, `finished_at=now`, `exit_reason=exited`)
- Remove on-read `reconcileInvocationState()` from CLI read paths (agent ls/show/watch)
- Reconcile logic lives in daemon only

**Why split:** PR-10 is a straightforward ownership transfer. PR-11 introduces a polling loop inside daemon and removes scattered reconcile calls from CLI — different review concerns.

**Acceptance:**

- [ ] Daemon detects tmux session exit and updates meta
- [ ] On-read reaper removed from CLI
- [ ] `agent show`/`ls` no longer mutate meta on read

---

## PR-12 — Daemon Read API + CLI Read-Through-Daemon *(required for parity — Rule B)*

**Purpose:** Eliminate duplicated read/derive logic in CLI. All reads go through daemon.

**Scope:**

- Daemon endpoints:
  - `GET /worktrees` (filters: repo_id, state)
  - `GET /worktrees/{id}`
  - `GET /invocations` (filters: worktree_id, state, runner, mode)
  - `GET /invocations/{id}`
  - `GET /invocations/{id}/diff` — replaces CLI-local `agent diff`
  - `GET /invocations/{id}/checkpoints` — replaces CLI-local `checkpoint ls`
  - `GET /summary` — cacheable convenience: pre-derived counts for watch (active invocations, ready-to-land, needs-attention). Not required — clients can derive from list endpoints.
- CLI commands migrate:
  - `worktree ls`/`show`/`path` → call daemon read endpoints
  - `agent ls`/`show`/`diff`/`logs` → call daemon read endpoints
  - `checkpoint ls` → call daemon read endpoint
  - CLI stops scanning `${DATA_DIR}` entirely for v2 flows

**Status contract:**

Daemon is the **sole authority** for status derivation after this PR. Every invocation response includes:

- `lifecycle_status` — persisted state: `starting` | `running` | `finished` | `failed`
- `semantic_status` — derived from stream parsing (headless only): `working` | `needs_input` | `blocked` | `ready_for_review` | `null` (headed or unknown)
- `attention_flags` — boolean set: `needs_attention`, `stalled`, `orphaned`, `landable`
- `display_status` — single human-readable string derived with precedence rules:
  1. `failed` (lifecycle) → "failed"
  2. `needs_attention` (flag) → "needs attention"
  3. `needs_input` (semantic) → "needs input"
  4. `blocked` (semantic) → "blocked"
  5. `ready_for_review` (semantic) → "ready for review"
  6. `running` + `working` → "working"
  7. `running` + null semantic → "running"
  8. `finished` → "finished"
  9. `starting` → "starting"

CLI renders `display_status` directly. Watch uses `attention_flags` for sorting/highlighting. No status derivation logic remains in CLI after this PR.

**Explicit non-goals:**

- No streaming (that's PR-13)
- No repo registry (that's PR-14)
- CLI may still read log files directly for `--follow` tail (daemon log stream is PR-13)

**Acceptance:**

- [ ] All CLI read commands go through daemon, not store
- [ ] Statuses match previous CLI-derived outputs
- [ ] No CLI code scans store directories for v2 flows
- [ ] Daemon returns consistent derived status for all invocation states
- [ ] `display_status` precedence produces correct results for all state combinations
- [ ] `/summary` returns correct counts (cacheable, not required for correctness)

---

## PR-13 — Daemon Event Stream + Watch *(required for parity — Rule C)*

**Purpose:** Build watch TUI and eliminate polling. Watch subscribes to daemon event stream for live updates.

**Scope:**

- Daemon event stream endpoint:
  - `GET /events?since=<cursor>` (SSE)
  - Events include: `worktree_created`, `worktree_removed`, `invocation_started`, `invocation_finished`, `log_activity`, `checkpoint_created`, `land_succeeded`, `land_failed`, `status_changed`, etc.
  - Cursor-based resumption (client reconnects with last seen cursor)
- Daemon log stream (optional, recommended):
  - `GET /invocations/{id}/logs?stream=stdout|stderr|raw` (SSE)
  - Replaces file tail for `agent logs --follow` and watch log pane
- Watch TUI:
  - Bubbletea TUI
  - Hierarchical view: worktrees → invocations
  - Subscribes to `/events` for live updates
  - On reconnect or cursor gap: full resync via `GET /invocations` + `GET /worktrees` (then resumes streaming)
  - Can attach to log stream for selected invocation (headless)
  - Keyboard actions: attach, logs, land, discard, stop/kill, open worktree
  - Actions route through daemon mutation endpoints (not direct git/store writes)

**Architectural constraint:** Watch must not implement git/store mutations itself. It calls existing daemon endpoints for all actions.

**Explicit non-goals:**

- No WebSocket (SSE is sufficient for unidirectional events)
- No guaranteed delivery (cursor-based best-effort is fine for local daemon)
- No editing or config UI

**Acceptance:**

- [ ] `agency watch` shows live updates with **no filesystem polling**
- [ ] Event stream includes all state-changing operations
- [ ] Watch resync works on reconnect or missed cursor
- [ ] Log stream replaces file tailing for headless invocations
- [ ] Actions route to daemon-backed commands (no reimplemented logic)
- [ ] No crashes on rapid updates

---

## PR-14 — Daemon Repo Registry + CWD-less Operation *(required for parity — Rule E)*

**Purpose:** Enable remote clients and "operate without being inside a repo."

**Scope:**

- Daemon formalizes the repo registry (foundation laid in PR-06 via `repo_root` on worktree create):
  - `repo_id` → `repo_root`(s) + origin info + `last_seen`
  - Repos already registered via `POST /worktrees/create` since PR-06
- Daemon endpoints:
  - `POST /repos/register` (client provides repo_root; daemon derives repo_id)
  - `GET /repos`
  - `GET /repos/{repo_id}`
- Update mutation endpoints to accept `repo_id`/`worktree_id` without relying on client CWD
- CLI behavior:
  - If user runs `agency ...` outside a repo, CLI can still operate on known worktrees by id/name (daemon resolves)
  - `agency repo add <path>` to explicitly register a repo
  - `agency worktree ls --all-repos` works from anywhere

**Explicit non-goals:**

- No remote network listener (unix socket only; remote via ssh/tailscale tunnel)
- No multi-user auth

**Acceptance:**

- [ ] `agency worktree ls --all-repos` works from any directory
- [ ] Repos auto-registered via worktree creation since PR-06
- [ ] Explicit `agency repo add <path>` works
- [ ] Daemon resolves worktree/invocation refs without client CWD
- [ ] Phone/termux client over ssh becomes possible without CWD hacks

---

## Parity Completion Checklist

After PR-14, the following must be true:

- [ ] **No CLI writes** to v2 store files (worktrees, invocations, sandboxes, checkpoints, events)
- [ ] **No CLI reads** of v2 store files (all reads go through daemon)
- [ ] **No filesystem polling** in watch (replaced by `/events` stream)
- [ ] **No CLI-derived status logic** (daemon is sole authority)
- [ ] **No CLI scanning** of store directories for v2 flows
- [ ] **No CLI reconciliation** of tmux/pid state on read paths
- [ ] Daemon exposes `api_version` and `build_version`; CLI refuses incompatible versions
- [ ] Daemon operates without client CWD (repo registry)

---

## Minimal API Surface (Full Parity)

### Mutation Endpoints

| Endpoint | PR | Description |
|----------|-----|-------------|
| `POST /worktrees/create` | PR-06 | Create integration worktree |
| `POST /worktrees/{id}/rm` | PR-06 | Remove integration worktree |
| `POST /invocations/start_headless` | PR-05 | Start headless invocation (daemon creates sandbox+meta) |
| `POST /invocations/start_headed` | PR-10 | Start headed invocation (daemon creates tmux session) |
| `POST /invocations/{id}/stop` | PR-04 | Graceful stop |
| `POST /invocations/{id}/kill` | PR-04 | Forceful kill |
| `POST /invocations/{id}/land` | PR-09 | Land sandbox changes into integration |
| `POST /invocations/{id}/discard` | PR-09 | Discard sandbox |
| `POST /invocations/{id}/checkpoints/apply` | PR-08 | Restore sandbox to checkpoint |
| `POST /repos/register` | PR-14 | Register a repo with daemon |

### Read Endpoints

| Endpoint | PR | Description |
|----------|-----|-------------|
| `GET /health` | PR-04 | `api_version`, `build_version`, `daemon_pid`, `uptime` |
| `GET /worktrees` | PR-12 | List worktrees (filters: repo_id, state) |
| `GET /worktrees/{id}` | PR-12 | Worktree details |
| `GET /invocations` | PR-12 | List invocations (filters: worktree_id, state, runner, mode) |
| `GET /invocations/{id}` | PR-12 | Invocation details + derived status |
| `GET /invocations/{id}/diff` | PR-12 | Sandbox diff |
| `GET /invocations/{id}/checkpoints` | PR-12 | Checkpoint list |
| `GET /summary` | PR-12 | Cacheable convenience: pre-derived counts for watch |
| `GET /repos` | PR-14 | List registered repos |
| `GET /repos/{repo_id}` | PR-14 | Repo details |

### Stream Endpoints

| Endpoint | PR | Description |
|----------|-----|-------------|
| `GET /events?since=<cursor>` | PR-13 | SSE event stream (all state changes) |
| `GET /invocations/{id}/logs?stream=raw\|stderr` | PR-13 | SSE log stream |

### Response Contract

Every JSON response includes:

- `api_version` — daemon API version (integer, incremented on breaking changes)
- `build_version` — semver if tagged release, otherwise git commit SHA; response also includes `git_sha` unconditionally
- `request_id` — daemon-generated UUID per request; echoed in response, daemon logs, and emitted events for tracing
- `ok` — boolean success indicator

On error (`ok: false`):

- `error_code` — structured error code (reuses existing agency error codes, e.g., `E_INVOCATION_NOT_FOUND`)
- `message` — human-readable error description
- `hint` — optional actionable suggestion (e.g., "use 'agent kill' to finalize")

---

## Optional Follow-ups (Post-Slice)

- `agency run` convenience wrapper (`worktree create` + `agent start`)
- Auto-rename branches
- Auto-land policies
- Sandbox GC
- Richer diff UI
- `--strategy merge` for landing (if compelling use case)