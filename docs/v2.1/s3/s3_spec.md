# Slice S3: Chat Control Plane + Restart-From-Checkpoint — Spec

## Goal

Enable detached conversational continuation for headless invocations with CLI-first parity for history, restart, turn-aware diff context, and checks-first readiness.

## Acceptance Criteria

### detached transcript timeline is available
- **given**: a headless invocation has prompt input, runner output, and normalized stream events
- **when**: the user reads invocation transcript/history in human or `--json` mode
- **then**: the response shows one ordered timeline that includes prompts, assistant/user messages, tool-use activity, and raw-log coverage, with stable cursor-based pagination

### follow-up prompt continues the same invocation
- **given**: a headless invocation is active
- **when**: the user submits a follow-up prompt through the invocation control plane
- **then**: the prompt is accepted as a write on the existing invocation context (not a new invocation), appears in timeline order, and supports idempotent retry behavior

### prompt and log safety limits are enforced
- **given**: a user submits prompt text (directly or via file) or requests transcript/log data
- **when**: payload size or read limits exceed the S3 contract bounds
- **then**: the request is rejected with deterministic validation errors; accepted requests use bounded I/O behavior

### repeated detach and re-entry preserves continuity
- **given**: a user has detached from a headless invocation
- **when**: the user re-enters invocation context multiple times to inspect history and continue conversation
- **then**: invocation continuity is preserved across re-entry cycles and users do not need to recreate a fresh invocation

### explicit checkpoint restart is one command path
- **given**: a headless invocation has checkpoints and a selected checkpoint id
- **when**: the user runs the canonical invocation restart flow with explicit checkpoint selection
- **then**: checkpoint restore and runner restart execute as one flow with one result contract

### interactive history selector restores deterministically
- **given**: a headless invocation has timeline history and checkpoints
- **when**: the user invokes interactive restart selection and chooses an item with arrow-key navigation
- **then**: the selected history point maps deterministically to a checkpoint restore + restart outcome, and non-interactive/scripted usage remains available via explicit checkpoint flags

### timeline ordering survives daemon restarts and checkpoint apply
- **given**: an invocation timeline spans daemon restarts and checkpoint-apply events
- **when**: transcript/history is read incrementally with cursors
- **then**: ordering remains monotonic and stable for navigation, selector mapping, and resume flows

### headless restart execution uses non-interactive process defaults
- **given**: a headless invocation is started or restarted through the chat control plane
- **when**: the daemon launches the runner process
- **then**: the process does not inherit interactive stdin expectations and includes required non-interactive environment defaults for deterministic automation behavior

### turn-aware diff context is available from chat history
- **given**: an invocation timeline has persisted turns/messages and sandbox change history
- **when**: the user requests diff context for a selected turn or turn range
- **then**: the system returns a deterministic diff mapping anchored to that turn context, and the mapping remains stable across detach/re-entry

### checks-first readiness surface is available in terminal
- **given**: an invocation has review/check/todo signals that affect merge readiness
- **when**: the user opens the checks-focused terminal surface in human or `--json` mode
- **then**: the surface reports readiness state, blocking reasons, and invocation-linked navigation context without requiring GUI/full TUI chrome

## Key Decisions

**Canonical S3 surfaces live under `agent` with daemon-owned read/write behavior**: S3 defines invocation-scoped transcript, follow-up prompt, and restart-from-checkpoint as canonical `agent` workflows. Compatibility commands may remain, but they cannot redefine S3 semantics.

**Transcript is a unified invocation timeline contract**: S3 uses one cursored timeline model for detached inspection instead of separate ad hoc views. Timeline entries must be typed and scriptable so human inspection and automation use the same ordering model.

**Restart-from-checkpoint is invocation-scoped and integrated**: S3 treats checkpoint restore + restart as one invocation command path rather than a manual multi-command sequence. This is the canonical continuity contract for detached headless recovery.

**History selector mapping is deterministic and auditable**: selecting a timeline point must always resolve to the same checkpoint decision rule, with clear error behavior when no valid checkpoint mapping exists.

**Event ordering is persistent, not process-local**: timeline and checkpoint-related sequence ordering must remain monotonic across daemon lifecycle boundaries, including checkpoint apply operations, to keep cursoring and selector behavior reliable.

**Turn identity is a durable join key across chat, checkpoints, and diff surfaces**: turn-aware diff behavior requires stable turn identifiers and deterministic mapping rules so users can move from chat history to concrete code changes.

**Checks-first parity is terminal-first and scriptable**: S3 includes a checks-focused terminal surface with machine-readable output; full-screen workspace chrome remains deferred.

## Out of Scope

- Runner capability expansion to `amp`, `opencode`, `cursor-cli`, and `droid` (-> Slice S4)
- Broad mutation-command `--json` parity outside S3 chat/restart surfaces (-> Slice S4)
- Invocation-scoped review/PR/merge command family (-> Slice S5)
- Reports v2 and global CLI ergonomics cleanup (-> Slice S6)
- Full-screen watch/TUI workspace chrome or GUI parity work (-> Slice S7 / deferred)
