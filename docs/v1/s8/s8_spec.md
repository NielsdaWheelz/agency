# Agency — Slice 8: Integration Worktrees, Sandbox Isolation, Agents, and Watch

## Goal

Introduce **integration worktrees** (stable branches you open and merge), **sandbox worktrees** (ephemeral per-invocation execution directories), **agent invocations** running safely inside sandboxes, **headed and headless runners**, **per-sandbox checkpointing** via private refs, a **landing workflow** to move sandbox results into integration, and a **live watch interface**.

This slice moves Agency from "run = everything" into a composable orchestration system where multiple agents can work concurrently on the same integration branch without interference.

---

## Non-Goals (Slice 8)

Explicitly out of scope:

- Multi-user or remote orchestration
- Cloud execution
- Autonomous approval or merge
- Planner / council systems
- Persistent conversational state
- Semantic understanding of model "thinking"
- Fine-grained tool telemetry beyond filesystem + stream parsing
- Linear, Jira, or issue tracker integrations
- Branch naming driven by LLM output (auto-rename deferred)
- Additional runners beyond Claude and Codex (droid/opencode/amp deferred)

---

## Migration Note

**Slice 8 changes the on-disk layout.** Existing v1 `runs/<run_id>/` directories remain as legacy records but are not used by new commands.

New commands create:
- Integration worktree records under `worktrees/<worktree_id>/`
- Sandbox artifacts under `sandboxes/<invocation_id>/`
- Invocation records under `invocations/<invocation_id>/`

No automatic migration of v1 runs. Users can continue using v1 commands for existing runs.

---

## Core Concepts

### Integration Worktree

An **integration worktree** is the stable branch you intend to merge, push, or PR. It is what you open in your editor.

**Properties:**

- Named workspace tied to a repository and branch
- Independent of any agent invocation
- **Never modified by a runner directly** — agents execute in sandboxes
- Can have multiple concurrent agent invocations (each in its own sandbox)
- Has a stable identity even if no agent is running
- Created via `git worktree add -b` from a parent branch
- Contains `.agency/INTEGRATION_MARKER` to identify it as an integration tree

---

### Sandbox Worktree

A **sandbox worktree** is an ephemeral per-invocation workspace derived from the integration branch. It is where runners actually execute.

**Properties:**

- One sandbox per invocation (1:1 relationship)
- Created automatically when an invocation starts
- Branched from the integration worktree's branch at invocation time
- Deleted when the invocation is discarded or after landing
- Two invocations never write to the same directory

**Rationale:** Sandboxes eliminate shared-directory concurrency. Each agent gets its own isolated filesystem. The integration worktree stays clean and human-owned.

---

### Agent Invocation

An **agent invocation** is one execution of a runner (Claude, Codex, etc.) inside a sandbox worktree.

**Properties:**

- Exactly one runner
- Either headed (tmux) or headless (subprocess)
- Produces logs, status, checkpoints, and an exit outcome
- Owns its sandbox worktree (sandbox lifetime = invocation lifetime)
- Does **not** own the integration worktree

---

### Relationship Diagram

```
integration worktree (branch: agency/my-feature-a3f2)
├── sandbox 1 (invocation 20260128120500-b7c9, branch: agency/sandbox-20260128120500-b7c9)  [active]
├── sandbox 2 (invocation 20260128130000-d4e5, branch: agency/sandbox-20260128130000-d4e5)  [landed]
└── sandbox 3 (invocation 20260128140000-f1g2, branch: agency/sandbox-20260128140000-f1g2)  [discarded]
```

Multiple agents can work on the same integration branch concurrently. Each gets its own sandbox. Results are "landed" into the integration branch when ready.

---

## Integration Worktree Details

### Lifecycle

```
create → present
rm     → archived (record retained, tree removed)
```

**States:**

| State | Tree Exists | Record Exists | Name Reusable |
|-------|-------------|---------------|---------------|
| `present` | Yes | Yes | No |
| `archived` | No | Yes | Yes |

**Transitions:**

- `worktree create` → creates record + tree → **present**
- `worktree rm` → removes tree, retains record → **archived**
- `worktree rm --purge` → removes tree + record (deferred to future slice)

**When archived:**

- Checkpoint snapshot refs remain valid in the parent repo
- Name becomes available for reuse
- Invocation history preserved in invocation records

---

### Integration Worktree Fields

| Field | Description |
|-------|-------------|
| `worktree_id` | `<yyyymmddhhmmss>-<4hex>` (same format as run_id) |
| `name` | Human-readable, unique among non-archived (2-40 chars, lowercase alphanumeric + hyphens) |
| `repo_id` | Associated repository |
| `branch` | `agency/<name>-<shortid>` where shortid is last 4 chars of worktree_id |
| `parent_branch` | Branch this was created from |
| `tree_path` | Filesystem path to the integration git worktree |
| `created_at` | Creation timestamp (RFC3339) |
| `last_used_at` | Last activity timestamp (RFC3339) |
| `state` | `present` or `archived` |

**Branch creation:** The branch is created at worktree creation time via `git worktree add -b`. Multiple worktrees cannot share a branch.

---

### Integration Worktree Storage

```
${AGENCY_DATA_DIR}/repos/<repo_id>/worktrees/<worktree_id>/
├── meta.json           # Integration worktree metadata (record)
└── tree/               # Actual git worktree (the code you open)
    ├── .git            # Worktree git link
    ├── .agency/
    │   └── INTEGRATION_MARKER   # Prevents runners from executing here
    └── <project files>
```

**Key distinction:**

- **Record directory:** `worktrees/<id>/` — contains `meta.json`
- **Tree directory:** `worktrees/<id>/tree/` — the actual git worktree with project files

The `tree_path` field in `meta.json` points to the tree directory.

**Note:** No `checkpoints.json` or `runner_status.json` in the integration worktree. Checkpoints and runner state live in sandboxes.

---

### Integration Worktree meta.json Schema

```json
{
  "schema_version": "1.0",
  "worktree_id": "20260128120000-a3f2",
  "name": "my-feature",
  "repo_id": "abc123def456...",
  "branch": "agency/my-feature-a3f2",
  "parent_branch": "main",
  "tree_path": "/path/to/data/repos/.../worktrees/20260128120000-a3f2/tree",
  "created_at": "2026-01-28T12:00:00Z",
  "last_used_at": "2026-01-28T14:30:00Z",
  "state": "present"
}
```

---

## Sandbox Worktree Details

### Lifecycle

```
agent start → sandbox created (active)
agent land  → sandbox removed (landed)
agent discard → sandbox removed (discarded)
```

A sandbox exists only while the invocation needs it. After landing or discarding, the sandbox tree is deleted.

---

### Sandbox Fields

Sandbox metadata is stored in the **invocation** record (see Invocation Fields below). The sandbox directory contains only operational artifacts: tree, logs, and checkpoints.

There is no `sandboxes/<id>/meta.json`. The invocation meta.json is the single source of truth for sandbox state.

---

### Sandbox Storage (Operational Artifacts Only)

```
${AGENCY_DATA_DIR}/repos/<repo_id>/sandboxes/<invocation_id>/
├── checkpoints.json    # Checkpoint records for this invocation
├── logs/
│   ├── raw.jsonl       # Verbatim runner stdout (JSONL as emitted)
│   ├── stderr.log      # Runner stderr (errors, warnings)
│   └── stream.jsonl    # Normalized events (written by stream parser)
└── tree/               # Actual git worktree (runner executes here)
    ├── .git            # Worktree git link
    ├── .agency/        # Agency state inside sandbox
    │   └── state/
    │       └── runner_status.json
    └── <project files>
```

**Key points:**

- Sandbox `tree/` is the CWD for runner execution
- `runner_status.json` lives in the sandbox, not integration worktree
- Logs are per-sandbox (per-invocation)
- Checkpoints are per-sandbox
- **No meta.json here** — invocation meta.json is canonical

---

## Invocation Details

### Invocation Fields

The invocation record is the **single source of truth** for both invocation lifecycle and sandbox state.

| Field | Description |
|-------|-------------|
| `invocation_id` | `<yyyymmddhhmmss>-<4hex>` (same format as run_id) |
| `integration_worktree_id` | Target integration worktree |
| `sandbox_path` | Filesystem path to sandbox tree (CWD for runner) |
| `sandbox_branch` | `agency/sandbox-<invocation_id>` (full invocation ID) |
| `base_commit` | Integration branch commit at invocation start |
| `runner` | Runner type (`claude`, `codex`) |
| `mode` | `headed` or `headless` |
| `pid` | Process ID (headless only, null for headed) |
| `tmux_session` | Session name (headed only, null for headless) |
| `started_at` | Start timestamp (RFC3339) |
| `finished_at` | Finish timestamp (RFC3339, null if running) |
| `status` | `starting` / `running` / `finished` / `failed` |
| `exit_reason` | `exited` / `killed` / `stopped` / `unknown` (null if running) |
| `exit_code` | Integer exit code (headless only, null for headed or if running) |
| `last_output_at` | Last stdout/stderr activity (RFC3339) |
| `landing_status` | `pending` / `landed` / `discarded` (null if still running) |

**Mode-specific behavior:**

| Field | Headed | Headless |
|-------|--------|----------|
| `pid` | null | subprocess PID |
| `tmux_session` | session name | null |
| `exit_code` | null (not capturable without wrapper) | captured from subprocess |
| `exit_reason` | `exited` when session ends, `killed` on kill-session | `exited`/`killed`/`stopped` based on signal |

**"Finished" definition:**

- **Headless:** subprocess exited (exit code captured)
- **Headed:** tmux session no longer exists (exit code unknown)

---

### Invocation Terminal States

| State | Meaning |
|-------|---------|
| `finished` | Runner exited normally |
| `failed` | Runner crashed or was killed |
| `landed` | Sandbox changes applied to integration branch |
| `discarded` | Sandbox deleted without landing |

`landed` and `discarded` are set by the landing workflow after the invocation has finished.

---

### Invocation Storage

```
${AGENCY_DATA_DIR}/repos/<repo_id>/invocations/<invocation_id>/
├── meta.json           # Canonical record (invocation + sandbox state)
└── events.jsonl        # Agency events (start, stop, checkpoint, land, etc.)
```

Logs and checkpoints live under the sandbox directory (same invocation_id key).

---

### Invocation meta.json Schema

```json
{
  "schema_version": "1.0",
  "invocation_id": "20260128120500-b7c9",
  "integration_worktree_id": "20260128120000-a3f2",
  "sandbox_path": "/path/to/data/repos/.../sandboxes/20260128120500-b7c9/tree",
  "sandbox_branch": "agency/sandbox-20260128120500-b7c9",
  "base_commit": "789abc...",
  "runner": "claude",
  "mode": "headless",
  "pid": 12345,
  "tmux_session": null,
  "started_at": "2026-01-28T12:05:00Z",
  "finished_at": null,
  "status": "running",
  "exit_reason": null,
  "exit_code": null,
  "last_output_at": "2026-01-28T12:10:00Z",
  "landing_status": null,
  "prompt_source": "file",
  "prompt_path": "/path/to/prompt.md"
}
```

---

## Command Surface (Slice 8)

### Worktrees

```
agency worktree create --name <name> [--parent <branch>] [--open]
agency worktree ls [--repo] [--all]
agency worktree show <name|id|prefix> [--json]
agency worktree path <name|id|prefix>
agency worktree open <name|id|prefix>
agency worktree shell <name|id|prefix>
agency worktree rm <name|id|prefix> [--force]
```

**Rules:**

- `create` creates integration branch + integration directory (does **not** start runners)
- `create` writes `.agency/INTEGRATION_MARKER` into the tree
- `create` fails if name already exists (among non-archived worktrees)
- `rm` fails if any active agent invocations exist (unless `--force`)
- `rm --force` executes: stop all invocations → wait 5s → kill → discard sandboxes → remove integration tree
- `path` outputs the integration tree path only (for scripting: `` cd `agency worktree path foo` ``)
- `open` opens integration directory in configured editor
- `--open` on create opens in configured editor after creation
- Resolution accepts name, worktree_id, or unique prefix
- `--all` includes archived worktrees in listing

---

### Agents

```
agency agent start --worktree <name|id> [--runner <runner>] [--headless] [--prompt-file <path>] [--prompt <string>] [--detached] [--no-include-untracked] [--runner-arg <arg>]...
agency agent ls [--repo] [--worktree <name|id>]
agency agent show <invocation_id|prefix> [--json]
agency agent attach <invocation_id|prefix>
agency agent stop <invocation_id|prefix>
agency agent kill <invocation_id|prefix>
agency agent diff <invocation_id|prefix>
agency agent land <invocation_id|prefix> [--apply] [--require-base]
agency agent discard <invocation_id|prefix>
agency agent open <invocation_id|prefix>
agency agent logs <invocation_id|prefix> [--follow]
```

**`start` behavior:**

1. Verify target is an integration worktree (`.agency/INTEGRATION_MARKER` exists)
2. Create sandbox worktree branched from integration branch
3. Run runner inside sandbox directory (CWD = sandbox tree path)
4. If `--detached`: return immediately
5. If not `--detached` and headed: attach to tmux session

**`start` must refuse** if sandbox path resolution fails and would fall back to integration path. No code path may execute a runner with CWD = integration tree.

**Prompt resolution (headless mode):**

1. If `--prompt-file <path>` given: read file contents as prompt
2. If `--prompt <string>` given: use string directly
3. If neither: open `$EDITOR` to compose prompt, then run

**Headed mode:** Prompt is provided interactively after attach.

**Landing workflow:**

- `diff` — show what the sandbox changed:
  - File diff: `git diff <base_commit>..<sandbox_branch>` (two-dot: base vs tip)
  - Commit list: `git log --oneline <base_commit>..<sandbox_branch>`
- `land` — apply sandbox changes into integration branch, then delete sandbox tree
  - Default: cherry-pick sandbox commits onto current integration HEAD
  - `--apply`: if no commits exist, apply working tree diff instead (see Landing Workflow)
  - `--require-base`: fail if integration has diverged from `base_commit` (strict mode)
- `discard` — delete sandbox tree without applying changes
- `open` — open sandbox directory in editor (for manual inspection)
- `logs` — tail sandbox logs; `--follow` for streaming

**Rules:**

- Many active invocations per integration worktree, each in its own sandbox
- One active invocation per sandbox (trivially enforced: 1:1 relationship)
- `stop` is graceful (SIGINT / C-c via tmux)
- `kill` is forceful (SIGKILL / tmux kill-session)
- `attach` only applies to headed invocations
- `--detached` starts but does not attach (headed mode only)
- `--runner-arg` passes additional flags to the runner command (repeatable)
- `--no-include-untracked` excludes untracked files from checkpoint snapshots
- `land` fails if invocation is still running
- `land` cherry-picks onto current integration HEAD by default (allows parallel landing)
- `land` reports conflicts and aborts if cherry-pick fails (sandbox preserved for manual resolution)
- `land` with no commits and dirty tree requires `--apply` (diff-based landing)
- `land` with no commits and clean tree errors with hint
- `land --require-base` fails if integration branch has diverged from `base_commit`
- `discard` can be used on running invocations (stops first, then discards)

---

### Checkpoints

```
agency checkpoint ls --invocation <invocation_id|prefix>
agency checkpoint apply --invocation <invocation_id|prefix> <checkpoint_id>
```

**Rules:**

- `ls` shows checkpoint history for an invocation (id, timestamp, diffstat)
- `apply` restores the sandbox to a checkpoint state (`git reset --hard` + `git checkout <snapshot_commit> -- .`)
- `apply` fails if invocation is still running (stop first)
- `apply` does **not** resume the invocation — user starts a new one after rollback

---

### Watch

```
agency watch [--repo]
```

**Interactive TUI with hierarchical view:**

```
my-feature (agency/my-feature-a3f2) [present]
├── inv-b7c9  claude  headless  running   3m ago  [active]
├── inv-d4e5  codex   headed    finished  10m ago [ready to land]
└── inv-f1g2  claude  headless  finished  1h ago  [landed]

bugfix-auth (agency/bugfix-auth-c8d3) [present]
└── inv-h3i4  claude  headed    running   1m ago  [active]
```

**View model:**

- Top level: integration worktrees
- Nested: invocations (agents) per worktree

**Actions depend on selection:**

| Context | Key | Action |
|---------|-----|--------|
| Invocation | `Enter` | Attach (if headed) |
| Invocation | `d` | Diff sandbox vs integration |
| Invocation | `L` | Land changes |
| Invocation | `D` | Discard sandbox |
| Invocation | `l` | View logs (tail) |
| Invocation | `s` | Stop |
| Invocation | `k` | Kill |
| Worktree | `o` | Open integration worktree in editor |
| Worktree | `S` | Shell into integration worktree |
| Global | `q` | Quit |

**Implementation:**

- Bubbletea TUI library
- Filesystem polling for status updates (no fsnotify for watch)
- tmux `has-session` for presence detection
- Log viewing via file tailing
- For headed invocations: `attach` (Enter key) is the primary viewing mechanism

Note: Watch uses **polling**. Checkpointing uses **fsnotify + periodic fallback**. Do not conflate.

---

### Legacy Wrapper

```
agency run --name <name> [...]
```

**Defined as:**

1. `worktree create --name <name> --open`
2. `agent start --worktree <name> --attach`

This remains the happy-path demo command.

---

## Headless Execution Model

### Supported Runners (Slice 8)

| Runner | Base Command | Notes |
|--------|--------------|-------|
| Claude Code | `claude -p --output-format stream-json --verbose` | JSONL to stdout |
| Codex CLI | `codex exec -C <dir> --json` | JSONL to stdout |

### Claude Headless Invocation

```bash
cd <sandbox_path>
claude -p \
  --output-format stream-json \
  --verbose \
  "<prompt>"
```

**Flags (verified against claude 2.1.x):**

- `-p` / `--print` — non-interactive mode, prompt as positional argument
- `--output-format stream-json` — JSONL streaming output (**requires `--verbose`**)
- `--verbose` — required by stream-json; without it claude exits with an error

**Optional flags:**

- `--include-partial-messages` — emit partial message chunks as they arrive (finer-grained streaming)

**CWD:** Always set to **sandbox** tree path. Never the integration tree.

**Policy:** Agency does not set `--permission-mode` or impose safety policy. User's Claude config applies.

**JSONL event types (stdout):**

| `type` | Description |
|--------|-------------|
| `system` (subtype `init`) | Session metadata: cwd, session_id, model, tools |
| `assistant` | Model turn: content is `tool_use` or `text` |
| `user` | Tool result feedback |
| `result` (subtype `success`/`error`) | Final outcome: duration, cost, usage |

### Codex Headless Invocation

```bash
codex exec -C <sandbox_path> --json "<prompt>"
```

**Flags (verified against codex 0.x):**

- `exec` — non-interactive subcommand
- `-C` / `--cd` — set working directory
- `--json` — JSONL streaming output to stdout
- Prompt is a positional argument (or stdin if `-` or omitted)

**Policy:** Preserve codex config defaults for sandbox and approval settings. Agency does not impose policy in v2.

**JSONL event types (stdout):**

| `type` | Description |
|--------|-------------|
| `thread.started` | Session metadata: thread_id |
| `turn.started` | Turn boundary |
| `item.completed` (type `reasoning`) | Model reasoning step |
| `item.started` / `item.completed` (type `command_execution`) | Command exec with exit_code |
| `item.completed` (type `agent_message`) | Final text response |
| `turn.completed` | Turn end with token usage |

### Passing Additional Runner Flags

Use `--runner-arg` to pass through extra flags:

```bash
agency agent start --worktree foo --headless \
  --prompt "Fix the bug" \
  --runner-arg "--permission-mode" \
  --runner-arg "allowedTools"
```

---

## Status Model

### Two-Layer Model

#### 1. Invocation Status (orchestrator-owned)

**Location:** `invocations/<id>/meta.json`

**Includes:**

- Lifecycle state (`starting` / `running` / `finished` / `failed`)
- PID (headless) or tmux session (headed)
- `last_output_at`
- `exit_code` (headless only)
- `exit_reason`
- `landing_status` (`pending` / `landed` / `discarded`)

#### 2. Runner Status (optional, improves quality)

**Location:** `<sandbox_path>/.agency/state/runner_status.json`

Used when present to derive semantic status (`working`, `needs_input`, `blocked`, `ready_for_review`).

**Not required** for lifecycle correctness.

---

### Lifecycle Correctness vs Semantic Status

**Lifecycle correctness** (no runner cooperation required):

- Create/start invocation + sandbox
- Stop/kill invocation
- Log capture (stdout/stderr)
- Checkpoint creation and rollback
- Land/discard workflow
- Cleanup on remove

**Semantic status** (improved by runner cooperation or stream parsing):

- `working` / `needs_input` / `blocked` / `ready_for_review`
- Progress indicators
- Structured tool telemetry

---

### Invocation Reaper (Headed Finished Detection)

Headed invocations don't have exit codes. "Finished" = tmux session no longer exists.

A lightweight `reconcileInvocationState()` function handles this:

- Checks `tmux has-session -t <session_name>`
- If session missing AND `status == "running"` → set `status = "finished"`, `finished_at = now`, `exit_reason = "exited"`
- Called lazily on every read path: `agent ls`, `agent show`, `watch` refresh
- No daemon, no background goroutine — reconcile on demand
- Must be idempotent (safe to call multiple times)

This avoids needing a persistent watcher while ensuring state is always correct when observed.

---

### Derived Display Status

Computed from:

- Invocation lifecycle state (after reaper reconciliation)
- Landing status
- tmux presence (headed)
- Recent stdout/stderr activity (`last_output_at`)
- `runner_status.json` (if present)
- Stall detector (no output + process alive > threshold)

**Summary per integration worktree:**

- Number of active invocations
- Number ready to land (finished, not yet landed/discarded)
- Number needing input (from runner status)

---

## Logging

For every invocation, logs are stored in the sandbox:

```
${DATA_DIR}/repos/<repo_id>/sandboxes/<invocation_id>/logs/
├── raw.jsonl       # Verbatim runner stdout (JSONL as emitted by claude/codex)
├── stderr.log      # Runner stderr (errors, warnings)
└── stream.jsonl    # Normalized events (written by stream parser, PR-05)
```

Agency events are stored in the invocation record:

```
${DATA_DIR}/repos/<repo_id>/invocations/<invocation_id>/events.jsonl
```

**Requirements:**

- **Headless logs:** Complete. `raw.jsonl` = verbatim copy of runner stdout, appended as chunks arrive. `stderr.log` = runner stderr, appended as chunks arrive.
- **Headed logs:** Best-effort. Periodic `tmux capture-pane` sampling. TUI escape codes make raw capture unreliable; `attach` (via watch or CLI) is the primary viewing mechanism for headed invocations.
- `last_output_at` updated on **every chunk** written to `raw.jsonl` (not batched)
- `stream.jsonl` is NOT written by the logging layer — reserved for the stream parser (PR-05)
- Watch uses tailing of `raw.jsonl` for headless; attach for headed

---

## Checkpointing

### Scope

Checkpoints are **per-sandbox** (per-invocation). Each sandbox has its own checkpoint history.

### Primitive

**Private ref snapshots** are the checkpoint mechanism. Each checkpoint is a commit stored under a private ref namespace.

**Why not git stash:** Stash is repo-global (`refs/stash` + reflog). All worktrees share the same stash list. Concurrent sandboxes would race on `stash@{0}` and require message-based lookup hacks. Private refs avoid this entirely.

**Ref namespace:** `refs/agency/snapshots/<invocation_id>/<n>`

Each checkpoint commit captures the full working tree state (tracked changes + untracked files) without polluting the branch history.

### Trigger Policy (Defaults)

**Primary:** fsnotify watcher on sandbox tree directory

- Debounce: **3 seconds of inactivity**
- Rate limit: **max one checkpoint every 10 seconds**

**Fallback:** Periodic dirty check (handles fsnotify misses on macOS)

- Timer: every **30 seconds**
- Check: `git status --porcelain`
- If dirty AND last checkpoint > 10 seconds ago → checkpoint

**Additional triggers:**

- Invocation exit
- Verify completion (if applicable)

**Ignore patterns:**

- `.agency/` directory (prevent self-trigger loops)
- `.git/` directory
- Lock files (`*.lock`, `*.lck`)
- Large binary patterns (configurable)

### Checkpoint Record Schema

```json
{
  "checkpoints": [
    {
      "id": 1,
      "snapshot_ref": "refs/agency/snapshots/20260128120500-b7c9/1",
      "snapshot_commit": "abc123def456789...",
      "head_sha": "789xyz...",
      "created_at": "2026-01-28T12:10:00Z",
      "includes_untracked": true,
      "diffstat": "+42 -15 in 3 files"
    }
  ]
}
```

**Storage:** `sandboxes/<invocation_id>/checkpoints.json`

### Mechanics

**Creating a checkpoint:**

```bash
cd <sandbox_path>

# 1. Create a temporary index capturing current working tree state
export TEMP_INDEX=$(mktemp)
cp "$(git rev-parse --git-dir)/index" "$TEMP_INDEX"

# 2. Run denylist check BEFORE staging untracked files
#    (see Untracked Files Policy — if denylisted files found, abort checkpoint)
denylisted=$(git ls-files -o --exclude-standard | grep -E '\.env|\.key|\.pem|credentials\.json|secrets\.json')
if [ -n "$denylisted" ]; then
  # emit checkpoint_failed event, skip this checkpoint, return
  exit 0
fi

# 3. Stage all changes (tracked + untracked) into the temp index
GIT_INDEX_FILE="$TEMP_INDEX" git add -A

# 4. Write the tree object from the temp index
tree_hash=$(GIT_INDEX_FILE="$TEMP_INDEX" git write-tree)
rm "$TEMP_INDEX"

# 5. Create a snapshot commit (not on any branch)
snapshot_commit=$(git commit-tree "$tree_hash" \
  -p HEAD \
  -m "agency snapshot <invocation_id> <n>")

# 6. Store under private ref
git update-ref "refs/agency/snapshots/<invocation_id>/<n>" "$snapshot_commit"

# 7. Record in checkpoints.json
```

**If `--no-include-untracked`:** Skip step 2 (denylist check) and replace step 3 with `GIT_INDEX_FILE="$TEMP_INDEX" git add -u` (only tracked files).

**Ordering matters:** The denylist check (step 2) runs **before** anything is staged into the temp index. This ensures denylisted files are never written into a snapshot tree object, even transiently.

**Why this works:**

- Private refs are namespaced per invocation — no collisions
- No branch pollution (refs don't appear in `git log` or `git branch`)
- Commit objects are immutable and GC-safe while refs point to them
- No interference with the working index (temp index used)

### Rollback

Rollback restores the sandbox to a checkpoint state:

```bash
cd <sandbox_path>

# 1. Clean working tree to branch HEAD
git reset --hard
git clean -fd

# 2. Restore full tree state from the snapshot commit
git checkout <snapshot_commit> -- .
```

**How step 2 works:** The snapshot commit's tree contains all files (tracked + formerly untracked). `git checkout <sha> -- .` overwrites the working tree with every file in that tree. After step 1 cleaned everything, this restores the exact snapshot state.

**Post-rollback:**

- Rollback does **not** resume the same invocation
- User starts a new invocation after rollback
- Snapshot refs remain valid for future rollback

### Cleanup

When a sandbox is deleted (via `land` or `discard`):

```bash
# Remove all snapshot refs for this invocation
git for-each-ref --format='%(refname)' "refs/agency/snapshots/<invocation_id>/" | \
  xargs -I{} git update-ref -d {}
```

This allows git GC to eventually clean up the snapshot commit objects.

---

## Untracked Files Policy

### Denylist

The following patterns are denied from checkpoints:

- `.env`, `.env.*`
- `*.key`, `*.pem`
- `credentials.json`, `secrets.json`

### Checkpoint Behavior

**Before creating checkpoint:**

1. Scan untracked files: `git ls-files -o --exclude-standard`
   (This already excludes `.gitignore` patterns)
2. Check results against denylist
3. If any denylisted files found:
   - Skip checkpoint creation
   - Emit `checkpoint_failed` event with reason
   - Log warning
   - **Invocation continues** (non-fatal)

**Escape hatch:** `--no-include-untracked` flag on `agent start` to exclude untracked files from snapshots entirely.

---

## Checkpoint Failure Handling

Checkpoint failures are **best-effort; do not abort invocation**.

On failure:

1. Emit event to `events.jsonl`:
   ```json
   {
     "event": "checkpoint_failed",
     "data": {
       "reason": "denylisted_file",
       "files": [".env"],
       "invocation_id": "..."
     }
   }
   ```

2. Continue invocation execution

---

## Landing Workflow

Landing is the process of applying sandbox changes back to the integration branch.

### `agency agent diff <invocation>`

Shows what the sandbox changed:

```bash
# File diff: base_commit vs sandbox tip (two-dot = direct comparison)
git diff <base_commit>..<sandbox_branch>

# Commit list
git log --oneline <base_commit>..<sandbox_branch>
```

Both outputs are shown to the user.

### `agency agent land <invocation> [--apply] [--require-base]`

**Preconditions:**

- Invocation must be finished (not running)

**Default behavior (no `--require-base`):**

Landing cherry-picks onto the **current** integration HEAD, regardless of whether it has moved since `base_commit`. This is essential for parallel sandbox workflows where landing one sandbox moves the integration HEAD before others land.

**With `--require-base` (strict mode):**

Fail if integration branch HEAD != `base_commit`. User must rebase or resolve manually.

**Case 1: Commits exist** (default)

```bash
cd <integration_tree_path>
git cherry-pick <base_commit>..<sandbox_branch>
```

If cherry-pick conflicts: abort the cherry-pick, report conflicting files, leave sandbox intact for manual resolution. The user can inspect with `agent open` and retry.

**Case 2: No commits, dirty tree** (`--apply` required)

Runners often modify files without committing. When `<base_commit>..<sandbox_branch>` is empty but the sandbox working tree is dirty:

```bash
cd <sandbox_path>
git diff <base_commit> -- . > /tmp/patch

cd <integration_tree_path>
git apply /tmp/patch
git commit -m "agency: land invocation <invocation_id>"
```

`agent land` without `--apply` in this case errors with a hint: "no commits to cherry-pick; use --apply to land working tree changes."

**Case 3: No commits, clean tree**

Error: "nothing to land — sandbox has no commits and no uncommitted changes."

**Merge strategy: deferred.** Cherry-pick is the only landing strategy in slice 8. Merge pulls sandbox branch history into integration, which is a footgun for this model. Defer `--strategy merge` unless a compelling use case emerges.

**Post-land (success only):**

1. Set `landing_status = "landed"` in invocation meta
2. Delete sandbox tree (keep invocation record + checkpoint refs)
3. Update `last_used_at` on integration worktree

### `agency agent discard <invocation>`

**Behavior:**

- If invocation is running: stop → wait 5s → kill
- Set `landing_status = "discarded"` in invocation meta
- Delete sandbox tree (keep invocation record)
- Clean up snapshot refs for this invocation

---

## Isolation Invariants (Slice 8)

These invariants **must** hold and must not be "optimized" away:

1. **Each invocation gets its own sandbox worktree directory.** No sharing.
2. **Two invocations never write to the same directory.** Enforced by 1:1 sandbox-to-invocation mapping.
3. **Integration worktree is never modified by a runner directly.** Only the landing workflow writes to the integration tree.
4. **Landing is human-triggered.** No automatic merging of sandbox results (v2: may add auto-land with approval gates).
5. **Deleting an invocation deletes only its sandbox.** Integration worktree and other sandboxes are unaffected.
6. **Sandbox tree is the CWD for all runner execution.** Runners never execute in the integration tree.
7. **No code path may execute a runner with CWD = integration tree.** Enforced by:
   - Integration trees contain `.agency/INTEGRATION_MARKER`
   - `agent start` checks that the resolved sandbox path does not contain `INTEGRATION_MARKER`
   - If sandbox creation fails, `agent start` aborts rather than falling back to the integration tree

These invariants prevent race conditions by design, rather than by locking.

---

## Full Storage Layout

```
${AGENCY_DATA_DIR}/repos/<repo_id>/
├── repo.json
├── .lock
├── runs/<run_id>/                          # v1 legacy, untouched
├── worktrees/<worktree_id>/
│   ├── meta.json                           # Integration worktree record
│   └── tree/                               # Integration git worktree
│       ├── .git
│       ├── .agency/
│       │   └── INTEGRATION_MARKER
│       └── <project files>
├── sandboxes/<invocation_id>/              # Operational artifacts only
│   ├── checkpoints.json                    # Checkpoint records
│   ├── logs/
│   │   ├── raw.jsonl                       # Verbatim runner stdout
│   │   ├── stderr.log                      # Runner stderr
│   │   └── stream.jsonl                    # Normalized events (stream parser)
│   └── tree/                               # Sandbox git worktree (runner CWD)
│       ├── .git
│       ├── .agency/
│       │   └── state/
│       │       └── runner_status.json
│       └── <project files>
└── invocations/<invocation_id>/
    ├── meta.json                           # Canonical record (invocation + sandbox state)
    └── events.jsonl                        # Agency events
```

**Git refs (in main repo):**

```
refs/agency/snapshots/<invocation_id>/<n>   # Checkpoint snapshot commits
```

---

## Concurrency Rules

- Many active invocations per integration worktree (each in its own sandbox)
- One active invocation per sandbox (trivially enforced: 1:1 mapping)
- Repo-level lock is held **only** for:
  - `git worktree add` / `git worktree remove` operations
  - Atomic writes to meta.json files
  - Landing into integration branch (cherry-pick / merge)
- Lock is **never** held while a runner is executing
- Read-only commands (`ls`, `show`, `diff`, `logs`) are lock-free

---

## Failure Semantics

| Scenario | Behavior |
|----------|----------|
| Runner crash | Invocation marked failed, exit_code captured (headless) or exit_reason=unknown (headed) |
| Stalled output | `stalled` status derived |
| Corrupted sandbox | Explicit error, no auto-repair |
| Checkpoint failure | Event logged, invocation continues |
| Denylisted file in untracked | Checkpoint skipped, warning logged |
| fsnotify miss | Periodic fallback catches dirty state |
| Land conflict (cherry-pick/merge fails) | Abort operation, report conflicts, sandbox preserved |
| Sandbox creation fails | `agent start` aborts, no fallback to integration tree |

---

## Success Criteria

Slice 8 is complete when:

- [ ] Users can create named integration worktrees independent of agents
- [ ] Users can run multiple agents concurrently on the same integration worktree (each in sandbox)
- [ ] Both headed and headless agents are supported
- [ ] Per-sandbox checkpoints (via private refs) allow safe rollback
- [ ] Landing workflow (diff/land/discard) moves sandbox results to integration
- [ ] Parallel landing works: cherry-pick onto moved integration HEAD by default
- [ ] `agency watch` provides hierarchical live visibility (worktrees → invocations)
- [ ] Lifecycle correctness without runner cooperation (create, stop, kill, logs, checkpoints, land)
- [ ] Integration worktrees are never modified by runners directly (enforced by INTEGRATION_MARKER)
- [ ] The system remains reversible and inspectable
- [ ] Headless logs are complete (stdout/stderr captured directly)
- [ ] Headed logs are best-effort (periodic capture-pane; attach is the primary viewing mechanism)

---

## Pre-Implementation Tasks

- **Pin minimum CLI versions:** Record the minimum claude/codex versions tested against so future breakage is detectable.

---

## Open Questions (Deferred)

- Append-prompt mid-invocation (continuation)
- Semantic tool telemetry
- Structured diff visualization
- Cobra migration details (completion, output stability)
- `worktree rm --purge` to delete record entirely
- Auto-land with approval gates
- Rebase workflow when integration branch diverges during sandbox execution
- Sandbox branch cleanup (prune old branches)
- Snapshot ref GC strategy for long-lived repos
