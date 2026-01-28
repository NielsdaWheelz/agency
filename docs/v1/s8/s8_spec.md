# Agency — Slice 8: Worktrees, Agents, Headless Execution, and Watch

## Goal

Introduce first-class **worktrees** and **agent invocations** as independent primitives, support **headed and headless runners**, enable **frequent reversible checkpoints**, and provide a **live watch interface**, while preserving a simple happy-path wrapper.

This slice modernizes Agency from "run = everything" into a composable orchestration system suitable for managing multiple parallel AI agents safely.

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
- Worktree records under `worktrees/<worktree_id>/`
- Invocation records under `invocations/<invocation_id>/`

No automatic migration of v1 runs. Users can continue using v1 commands for existing runs.

---

## Core Concepts (New / Clarified)

### Worktree

A **worktree** is a named workspace tied to a repository and branch.

**Properties:**

- Independent of any agent invocation
- Can host multiple sequential agent invocations
- Has a stable identity even if no agent is running

---

### Worktree Lifecycle

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

- `path` field points to former tree location (no longer exists)
- Checkpoints remain in record (stash commits still valid in parent repo)
- Name becomes available for reuse
- Invocation history preserved

---

### Worktree Fields

| Field | Description |
|-------|-------------|
| `worktree_id` | `<yyyymmddhhmmss>-<4hex>` (same format as run_id) |
| `name` | Human-readable, unique among non-archived (2-40 chars, lowercase alphanumeric + hyphens) |
| `repo_id` | Associated repository |
| `branch` | `agency/<name>-<shortid>` where shortid is last 4 chars of worktree_id |
| `parent_branch` | Branch this was created from |
| `tree_path` | Filesystem path to the git worktree (the actual code directory) |
| `created_at` | Creation timestamp (RFC3339) |
| `last_used_at` | Last activity timestamp (RFC3339) |
| `state` | `present` or `archived` |

**Branch creation:** The branch is created at worktree creation time via `git worktree add -b`. Multiple worktrees cannot share a branch.

---

### Worktree Storage Layout

```
${AGENCY_DATA_DIR}/repos/<repo_id>/worktrees/<worktree_id>/
├── meta.json           # Worktree metadata (record)
├── checkpoints.json    # Checkpoint records
└── tree/               # Actual git worktree (the code)
    ├── .git            # Worktree git link
    ├── .agency/        # Agency state inside worktree
    │   └── state/
    │       └── runner_status.json
    └── <project files>
```

**Key distinction:**

- **Record directory:** `worktrees/<id>/` — contains `meta.json`, `checkpoints.json`
- **Tree directory:** `worktrees/<id>/tree/` — the actual git worktree with project files

The `tree_path` field in `meta.json` points to the tree directory.

---

### Worktree meta.json Schema

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
  "state": "present",
  "flags": {
    "checkpoint_degraded": false
  }
}
```

---

### Agent Invocation

An **agent invocation** is one execution of a runner (Claude, Codex, etc.) inside a worktree.

**Properties:**

- Exactly one runner
- Either headed (tmux) or headless (subprocess)
- Produces logs, status, checkpoints, and an exit outcome
- Does **not** own the worktree

---

### Invocation Fields

| Field | Description |
|-------|-------------|
| `invocation_id` | `<yyyymmddhhmmss>-<4hex>` (same format as run_id) |
| `worktree_id` | Associated worktree |
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

### Invocation Storage

```
${AGENCY_DATA_DIR}/repos/<repo_id>/invocations/<invocation_id>/
├── meta.json
├── events.jsonl
├── stdout.log
├── stderr.log
└── stream.jsonl (if runner supports structured output)
```

---

### Invocation meta.json Schema

```json
{
  "schema_version": "1.0",
  "invocation_id": "20260128120500-b7c9",
  "worktree_id": "20260128120000-a3f2",
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

- `create` fails if name already exists (among non-archived worktrees)
- `rm` fails if an active agent invocation exists (unless `--force`)
- `rm --force` executes: stop → wait 5s → kill → remove
- `path` outputs the tree path only (for scripting: `` cd `agency worktree path foo` ``)
- `--open` opens in configured editor
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
```

**Prompt resolution (headless mode):**

1. If `--prompt-file <path>` given: read file contents as prompt
2. If `--prompt <string>` given: use string directly
3. If neither: open `$EDITOR` to compose prompt, then run

**Headed mode:** Prompt is provided interactively after attach.

**Rules:**

- Only one active invocation per worktree at a time
- `stop` is graceful (SIGINT / C-c via tmux)
- `kill` is forceful (SIGKILL / tmux kill-session)
- `attach` only applies to headed invocations
- `--detached` starts but does not attach (headed mode only)
- `--runner-arg` passes additional flags to the runner command (repeatable)
- `--no-include-untracked` disables `-u` flag on checkpoint stashes

---

### Watch

```
agency watch [--repo]
```

**Interactive TUI:**

- Live-refresh list of worktrees + invocations
- Derived status display
- Select an invocation

**Keybindings:**

| Key | Action |
|-----|--------|
| `Enter` | Attach (if headed) |
| `o` | Open worktree |
| `l` | View logs (tail) |
| `s` | Stop |
| `k` | Kill |
| `q` | Quit |

**Implementation:**

- Bubbletea TUI library
- Filesystem polling for status updates (no fsnotify)
- tmux `has-session` for presence detection
- Log viewing via file tailing

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
| Claude Code | `claude --print --output-format stream-json` | Structured JSON to stdout |
| Codex CLI | `codex exec` | Preserves codex config defaults |

### Claude Headless Invocation

```bash
cd <tree_path>
claude --print \
  --output-format stream-json \
  --include-partial-messages \
  < prompt.txt
```

**Flags:**

- `--print` — non-interactive mode, accepts prompt from stdin
- `--output-format stream-json` — structured JSON output
- `--include-partial-messages` — smoother streaming (shows work in progress)

**CWD:** Always set to worktree tree path.

**Policy:** Agency does not set `--permission-mode` or impose safety policy. User's Claude config applies.

### Codex Headless Invocation

```bash
codex exec --cd <tree_path> "<prompt>"
```

**Policy:** Preserve codex config defaults for sandbox and approval settings. Agency does not impose policy in v2.

### Passing Additional Runner Flags

Use `--runner-arg` to pass through extra flags:

```bash
agency agent start --worktree foo --headless \
  --prompt "Fix the bug" \
  --runner-arg "--permission-mode" \
  --runner-arg "allowedTools"
```

---

### Status Model

Two-layer model:

#### 1. Invocation Status (orchestrator-owned)

**Location:** `invocations/<id>/meta.json`

**Includes:**

- Lifecycle state (`starting` / `running` / `finished` / `failed`)
- PID (headless) or tmux session (headed)
- `last_output_at`
- `exit_code` (headless only)
- `exit_reason`

#### 2. Runner Status (optional, improves quality)

**Location:** `<tree_path>/.agency/state/runner_status.json`

Used when present to derive semantic status (`working`, `needs_input`, `blocked`, `ready_for_review`).

**Not required** for lifecycle correctness.

---

### Lifecycle Correctness vs Semantic Status

**Lifecycle correctness** (no runner cooperation required):

- Create/start invocation
- Stop/kill invocation
- Log capture (stdout/stderr)
- Checkpoint creation and rollback
- Cleanup on remove

**Semantic status** (improved by runner cooperation or stream parsing):

- `working` / `needs_input` / `blocked` / `ready_for_review`
- Progress indicators
- Structured tool telemetry

---

### Derived Display Status

Computed from:

- Invocation lifecycle state
- tmux presence (headed)
- Recent stdout/stderr activity (`last_output_at`)
- `runner_status.json` (if present)
- Stall detector (no output + process alive > threshold)

---

## Logging

For every invocation:

```
${DATA_DIR}/repos/<repo_id>/invocations/<invocation_id>/
├── stdout.log      # Raw stdout, always captured
├── stderr.log      # Raw stderr, always captured
├── stream.jsonl    # Parsed structured events (if format known)
└── events.jsonl    # Agency events (start, stop, checkpoint, etc.)
```

**Requirements:**

- **Raw logs always captured** for both headed and headless
- **Best-effort parsing** into `stream.jsonl` when output format is known (e.g., Claude's stream-json)
- Streaming output appended as it arrives
- No reliance on tmux scrollback for headless
- Watch uses tailing of these files

---

## Checkpointing (Critical)

### Primitive

**Git stash snapshots** are the sole checkpoint mechanism.

**Rationale:**

- Captures tracked + untracked changes
- Reversible
- No branch pollution
- Matches industry practice (e.g., Conductor)

**Important:** Stashes are **global per repository**, not per worktree. Multiple concurrent worktrees share the stash list. This is handled by using stable commit hashes and message-based lookup.

### Trigger Policy (Defaults)

**Primary:** fsnotify watcher on worktree tree directory

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
      "stash_commit": "abc123def456789...",
      "head_sha": "789xyz...",
      "created_at": "2026-01-28T12:10:00Z",
      "invocation_id": "20260128120500-b7c9",
      "worktree_id": "20260128120000-a3f2",
      "diffstat": "+42 -15 in 3 files"
    }
  ]
}
```

**Storage:** `worktrees/<worktree_id>/checkpoints.json`

### Mechanics

**Creating a checkpoint:**

```bash
# Must run from inside the tree directory
cd <tree_path>

# Create stash with identifying message
git stash push -u -m "agency checkpoint <worktree_id> <invocation_id> <n>"

# Resolve commit hash by searching stash list for our message
# (Do NOT assume stash@{0} is ours - concurrent stashes may exist)
stash_commit=$(git stash list --format='%H %s' | grep "agency checkpoint <worktree_id> <invocation_id> <n>" | head -1 | cut -d' ' -f1)

# Record stash_commit in checkpoints.json
```

**Why message-based lookup:** Multiple worktrees may checkpoint concurrently. Another stash could land between our `stash push` and hash resolution. Searching by message ensures we find our specific stash.

### Rollback

Rollback restores the worktree to a checkpoint state:

```bash
cd <tree_path>

# 1. Clean tracked files to branch HEAD
git reset --hard

# 2. Remove untracked files (required if checkpoint used -u)
git clean -fd

# 3. Apply the checkpoint by its recorded commit hash
git stash apply <stash_commit>
```

**Post-rollback:**

- Rollback does **not** resume the same invocation
- User starts a new invocation after rollback
- The applied stash remains in stash list (not popped)

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
   - Set `flags.checkpoint_degraded = true` in worktree meta
   - Emit `checkpoint_failed` event with reason
   - Log warning
   - **Invocation continues** (non-fatal)

**Escape hatch:** `--no-include-untracked` flag on `agent start` to disable `-u` on stash (only checkpoint tracked files).

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
       "invocation_id": "...",
       "worktree_id": "..."
     }
   }
   ```

2. Set flag in worktree meta:
   ```json
   {
     "flags": {
       "checkpoint_degraded": true
     }
   }
   ```

3. Continue invocation execution

---

## Storage Layout

```
${AGENCY_DATA_DIR}/repos/<repo_id>/
├── repo.json
├── .lock
├── runs/<run_id>/              # v1 legacy, untouched
├── worktrees/<worktree_id>/
│   ├── meta.json               # Worktree record
│   ├── checkpoints.json        # Checkpoint records
│   └── tree/                   # Actual git worktree
│       ├── .git
│       ├── .agency/
│       │   └── state/
│       │       └── runner_status.json
│       └── <project files>
└── invocations/<invocation_id>/
    ├── meta.json
    ├── events.jsonl
    ├── stdout.log
    ├── stderr.log
    └── stream.jsonl
```

---

## Concurrency Rules

- One active invocation per worktree
- Repo-level lock applies to:
  - Worktree create/remove
  - Agent start/kill
- Read-only commands are lock-free

---

## Failure Semantics

| Scenario | Behavior |
|----------|----------|
| Runner crash | Invocation marked failed, exit_code captured (headless) or exit_reason=unknown (headed) |
| Stalled output | `stalled` status derived |
| Corrupted worktree | Explicit error, no auto-repair |
| Checkpoint failure | `checkpoint_degraded` flag set, invocation continues |
| Denylisted file in untracked | Checkpoint skipped, warning logged |
| fsnotify miss | Periodic fallback catches dirty state |

---

## Success Criteria

Slice 8 is complete when:

- [ ] Users can create named worktrees independent of agents
- [ ] Users can run both headed and headless agents
- [ ] Frequent checkpoints allow safe rollback
- [ ] `agency watch` provides live visibility
- [ ] Lifecycle correctness without runner cooperation (create, stop, kill, logs, checkpoints)
- [ ] The system remains reversible and inspectable
- [ ] Raw logs are always captured for all invocations

---

## Open Questions (Deferred)

- Append-prompt mid-invocation (continuation)
- Multi-agent per worktree concurrency
- Semantic tool telemetry
- Structured diff visualization
- Cobra migration details (completion, output stability)
- `worktree rm --purge` to delete record entirely
