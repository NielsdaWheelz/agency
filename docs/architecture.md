# architecture

this document defines the architecture contract and describes daemon internals. it is normative. when in doubt, update this doc before shipping.

for user-facing concepts, see [concepts](concepts.md).

## goals

- keep the system local-first, single-user, and deterministic
- make metadata and events the single source of truth
- keep process supervision correct and testable
- make contracts explicit and enforceable

## non-goals

- multi-user isolation
- windows support
- zero data loss on power loss

## core invariants

- all persisted state is owned by `internal/store` and its contract files.
- all external process execution goes through `internal/exec`.
- the daemon is the sole authority for all lifecycle mutations (invocations, sandboxes, worktrees).
- the CLI never writes invocation or sandbox files — it is a thin RPC client.
- metadata and events are append-only or atomic writes; no partial writes.
- schema_version is required and validated on every read.
- environment merges are deterministic and stable (sorted keys).

## packages

```
cmd/agency/              main entry point
internal/
├── cli/cobra/           Cobra command tree (flag parsing, dispatch)
├── commands/            command implementations (validate → RPC → format)
├── daemon/              daemon server, handlers, process supervision
│   ├── checkpoint/      checkpoint engine (semantic triggers, fsnotify, snapshots, rollback)
│   ├── landing/         landing service (cherry-pick, apply, discard)
│   └── stream/          stream parser (claude/codex output → normalized events)
├── daemonclient/        HTTP-over-Unix-socket RPC client
├── exec/                process execution (pty, streaming, process groups)
├── fs/                  safe filesystem operations (SafeRemoveAll)
├── git/                 git operations (worktree, branch, remote)
├── store/               on-disk persistence (atomic writes, locking)
├── config/              agency.json parsing and validation
└── services             runservice, invocation, worktree, integrationworktree
```

## control plane

```
┌──────────┐     HTTP/Unix Socket     ┌──────────────┐
│  CLI     │ ────────────────────────► │   Daemon     │
│  (thin)  │                          │   (owner)    │
└──────────┘                          └──────┬───────┘
                                             │
                              ┌──────────────┼──────────────┐
                              ▼              ▼              ▼
                         ┌────────┐    ┌──────────┐   ┌───────────┐
                         │ Store  │    │ Process  │   │ Checkpoint│
                         │ (disk) │    │ Supervisor│   │ Engine   │
                         └────────┘    └──────────┘   └───────────┘
```

- CLI commands call the daemon via RPC; the daemon calls store.
- daemon API is the only contract for invocation/worktree control.
- legacy daemon endpoints are deprecated and must not gain features.

## daemon

the daemon runs as a foreground process, listening on a Unix socket at `${AGENCY_DATA_DIR}/agencyd.sock`. it auto-starts when the CLI needs it (first invocation creation).

### responsibilities

- **invocation lifecycle**: create, start, stop, kill for headed and headless modes
- **sandbox management**: atomic creation and cleanup of sandbox worktrees
- **worktree management**: create and remove integration worktrees (single-writer)
- **process supervision**: spawn runners, capture stdout/stderr, handle exit
- **log streaming**: write `raw.jsonl`, `stderr.log`, `stream.jsonl` to disk
- **checkpoint engine**: semantic-trigger and fsnotify-based snapshots with deduplication
- **stream parsing**: real-time parsing of runner output for semantic status
- **headed reconciliation**: detect tmux session exit, update state
- **read API**: serve list/show/diff/logs/timeline queries for the CLI

### API

HTTP/1.1 over Unix socket.

**mutation endpoints:**
- `POST /invocations` — create invocation (headed or headless)
- `POST /invocations/{ref}/stop` — graceful stop
- `POST /invocations/{ref}/kill` — forceful kill
- `POST /invocations/{ref}/chat` — append invocation-scoped follow-up prompt (idempotent by `client_request_id`)
- `POST /invocations/{ref}/land` — land sandbox to integration
- `POST /invocations/{ref}/discard` — discard sandbox
- `POST /invocations/{ref}/checkpoints/{id}/apply` — rollback to checkpoint
- `POST /worktrees` — create integration worktree
- `POST /worktrees/{ref}/remove` — remove integration worktree
- `POST /shutdown` — daemon shutdown

**read endpoints:**
- `GET /health` — health check
- `GET /worktrees` — list worktrees (with filtering)
- `GET /worktrees/{ref}` — resolve worktree by name/id/prefix
- `GET /invocations` — list invocations (with filtering)
- `GET /invocations/{ref}` — resolve invocation by name/id/prefix
- `GET /invocations/{ref}/diff` — structured diff with commits
- `GET /invocations/{ref}/logs` — log reads (offset/tail) with strict bound validation (`tail_bytes` in `1..1048576`)
- `GET /invocations/{ref}/timeline` — unified typed timeline (prompt seed/messages/tool-use/follow-up prompts/raw coverage + lifecycle events) with stable cursor pagination
- `GET /invocations/{ref}/checkpoints` — checkpoint list

### invocation flow (headed)

1. CLI sends `POST /invocations` with `repo_root`, `worktree_ref`, `runner`
2. daemon resolves integration worktree, validates request
3. daemon atomically creates sandbox worktree + invocation record
4. daemon creates tmux session `agency_<invocation_id>` with runner in sandbox CWD
5. CLI receives `invocation_id`, `sandbox_path`, `tmux_session`
6. CLI attaches to tmux (unless `--detached`)

### invocation flow (headless)

1. CLI sends `POST /invocations` with `repo_root`, `worktree_ref`, `runner`, `prompt`
2. daemon resolves integration worktree, validates request
3. daemon atomically creates sandbox worktree + invocation record
4. daemon spawns runner process, streams stdout/stderr to log files
5. daemon starts checkpoint engine (semantic triggers from stream parser + fsnotify drift safety net)
6. daemon starts stream parser (normalized events + semantic status + checkpoint trigger notifications)
7. CLI receives `invocation_id` and exits

### headed reconciliation

a background loop runs every 3 seconds:

- `running` invocation with missing tmux session → marked `finished`
- `starting` invocation with missing session for 2+ checks → marked `failed`
- transient tmux errors logged but don't cause state transitions
- on daemon start, all headed invocations are immediately reconciled
- recently-started invocations (< 30s) get grace time

### graceful shutdown

1. reconciliation loop exits first (prevents race conditions)
2. active invocations receive SIGINT
3. 5-second wait
4. remaining invocations receive SIGKILL
5. socket and PID files cleaned up

## stream parsing

for headless invocations, the daemon parses runner output in real-time.

### log files

```
sandboxes/<invocation_id>/logs/
├── raw.jsonl        # verbatim stdout (exactly as emitted by runner)
├── stderr.log       # verbatim stderr
└── stream.jsonl     # normalized events (daemon-generated)
```

### runner commands

- Claude Code: `claude -p --output-format stream-json --verbose --dangerously-skip-permissions`
- Codex CLI: `codex exec --cd <sandbox> --json --full-auto`
- OpenCode: `opencode run --mode auto`

### normalized events

each line in `stream.jsonl` contains a JSON event with a stable schema:

```json
{
  "schema_version": "1.0",
  "seq": 1,
  "timestamp": "2026-02-01T12:00:00Z",
  "invocation_id": "20260201-a1b2",
  "runner": "claude-code|codex|amp|opencode|cursor|droid",
  "kind": "session_start|message|tool_start|tool_end|final|error|usage|parse_error",
  "data": { }
}
```

### content block enrichment

for runners with semantic adapters (claude, codex), `message` events include a `content_blocks` array in `data` that preserves the full structure of each content block. this is additive to the existing `text`, `has_tool_use`, and `tool_names` fields.

block types:
- `text`: `{ "type": "text", "text": "..." }`
- `tool_use`: `{ "type": "tool_use", "name": "...", "id": "...", "input": { ... } }`
- `tool_result`: `{ "type": "tool_result", "tool_use_id": "...", "content": "..." }`

empty blocks (type="") are filtered. tool_use inputs are pre-parsed from `json.RawMessage` to `interface{}` for stable JSON round-trips.

### semantic status

derived from parsed output:
- `working` — any assistant/command activity detected
- `ready_for_review` — runner completed successfully

status is written to `InvocationMeta.semantic_status`, throttled to 500ms updates, always persisted on exit.

### error handling

- parse errors do not crash the daemon or fail the invocation
- malformed lines emit `kind=parse_error` events (throttled)
- stdout ingestion is bounded: lines are read in chunks, parse buffering is capped at 8 MB per logical line, and oversized lines are drained without full-line allocation
- oversized lines are preserved verbatim in `raw.jsonl`, emit `kind=parse_error` with reason `line_too_large`, and do not terminate invocation lifecycle
- `raw.jsonl` always contains verbatim output regardless of parse success

## checkpoint engine

creates automatic snapshots of sandbox state during agent execution.

### creation

- **primary trigger**: semantic events from the stream parser — a checkpoint is created each time a mutating tool (Edit, Write, MultiEdit, NotebookEdit, Bash) completes. semantic checkpoints are not rate-limited since each tool completion is a distinct user-visible action.
- **drift safety net**: fsnotify watches sandbox tree with 60s debounce (catches changes not covered by semantic triggers). rate-limited to 1 per 10 seconds.
- **polling fallback**: 30s polling check for changes missed by fsnotify.
- **final**: created on invocation exit (only if content changed)
- **deduplication**: tree-SHA comparison skips identical snapshots

### schema

checkpoints.json uses `schema_version: "1.1"` (semantic trigger metadata). version `"1.0"` is accepted on read for backwards compatibility. unknown versions are rejected per binding rule #4.

semantic metadata fields (omitted for legacy checkpoints):
- `trigger`: what caused the checkpoint (`tool_end`, `drift`, `poll`, `shutdown`, `manual`)
- `tool_name`: the tool that completed (when trigger is `tool_end`)
- `stream_seq`: the stream.jsonl sequence number of the triggering event
- `description`: human-readable label (e.g., "After Edit", "Drift checkpoint")

### storage

```
refs/agency/snapshots/<invocation_id>/
├── 1     # snapshot commit for checkpoint 1
├── 2     # snapshot commit for checkpoint 2
└── ...

sandboxes/<invocation_id>/checkpoints.json   # metadata
```

### denylist

files excluded from snapshots to prevent secret capture:
- `.env`, `.env.*`
- `*.key`, `*.pem`
- `credentials.json`, `secrets.json`

denylisted files degrade the checkpoint to tracked-files-only (non-fatal).

### rollback

restores sandbox via `git reset --hard` + `git clean -fd` + `git checkout <snapshot> -- .`. invocation must be stopped first.

### events

emitted to `invocations/<id>/events.jsonl`:
- `agency.followup_prompt` (idempotent by `client_request_id`)
- `agency.checkpoint_created`
- `agency.checkpoint_failed`
- `agency.checkpoint_applied` (rollback)
- `agency.checkpoint_denylist_triggered`
- `agency.land_*`, `agency.discard_*`, `agency.conflict_detected`

## landing service

applies sandbox changes back to the integration worktree.

### modes

- **cherry-pick (default)**: cherry-picks sandbox commits onto integration HEAD
- **apply (`--apply`)**: applies uncommitted changes as a patch

### behavior

- conflicts abort and preserve sandbox
- `.agency/` files excluded from dirty checks, staging, and diffs
- on success, sandbox worktree, branch, and checkpoint refs cleaned up
- invocation record preserved with `landing_status=landed`
- discard stops running invocations, cleans up, sets `landing_status=discarded`

## persistence

### atomic writes

all state files written via temp file + rename. no partial writes.

### locking

repo-level file-based advisory locks acquired before mutations.

### file permissions

- directories: `0700`
- files: `0600`

### events

append-only JSONL at `invocations/<id>/events.jsonl`. contractually required on all mutations. append failure fails the operation. writes are locked and atomic via one daemon-owned invocation event writer (`internal/daemon/invocationevents`), which allocates one monotonic invocation-scoped sequence across follow-up, checkpoint, checkpoint-apply, and landing/discard producers.

### schema versioning

strict. unknown/empty versions rejected. no silent fallbacks. data directory deletion is the reset path.

## data model

```
${AGENCY_DATA_DIR}/
├── repo_index.json
├── agencyd.sock / agencyd.pid / agencyd.log
└── repos/<repo_id>/
    ├── repo.json
    ├── .lock
    ├── integration_worktrees/<worktree_id>/
    │   ├── meta.json
    │   └── tree/
    ├── invocations/<invocation_id>/
    │   ├── meta.json
    │   └── events.jsonl
    ├── sandboxes/<invocation_id>/
    │   ├── tree/
    │   ├── checkpoints.json
    │   └── logs/{raw.jsonl, stderr.log, stream.jsonl}
    └── runs/<run_id>/          (v1 legacy)
        ├── meta.json
        ├── events.jsonl
        └── logs/
```

## status derivation

display status derived from lifecycle + semantic state, in precedence order:

1. `failed`
2. `needs attention`
3. `needs input`
4. `blocked`
5. `ready for review`
6. `working`
7. `running`
8. `finished`
9. `starting`

attention flags: `needs_attention`, `stalled` (>5 min no output), `orphaned` (tmux gone), `landable` (finished, ready to land).

## extension points

- **runners**: add new runner types by extending command resolution and stream parsing
- **contracts**: new fields require schema bump, validation, and tests

## stubs

- observability spec (structured logs, trace ids)
- multi-repo orchestration (if ever needed)
