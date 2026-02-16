# v2.1 Product Brief

Last updated: 2026-02-16
Status: active

## Problem

Agency has strong local isolation and daemon primitives, but v2 UX is still
fragmented for detached/headless operation and invocation-centric review/merge.
Users cannot yet run the full "chat -> review -> PR -> merge -> archive" loop
with Conductor-like continuity from one invocation surface.

## v2.1 outcome

v2.1 is complete when Agency delivers functional Conductor parity at the
daemon+CLI layer while preserving Agency's sandbox-first model.

## Product goals

1. Make daemon APIs the read/write authority for v2 `agent` + `worktree` surfaces.
2. Deliver invocation-centric recovery and navigation for detached operation.
3. Add headless conversational continuation and restart-from-checkpoint.
4. Make review/PR/merge flows explicit under `agent` and machine-readable.
5. Complete runner-portable execution model for `claude-code`, `codex`, `amp`, `opencode`, `cursor-cli`, and `droid`, with consistent `--json` outputs.
6. Reduce report friction (JSON-compatible, optional stricter sections by mode).
7. Ensure fleet-scale ergonomics for many concurrent worktrees/invocations.

## In scope (v2.1)

1. Daemon-owned read/write path for v2 `agent` + `worktree` commands (local read fallback only for daemon bootstrap/health boundaries).
2. Invocation navigation and re-entry commands:
   `agent path`, `agent shell`, `agent enter`, `agent restart` (including checkpoint-based restart path).
3. Chat control plane for headless invocations:
   stream transcript (prompts/messages/tool-use/raw logs), send follow-up prompt, restart in place, and resume from checkpoint.
4. Fleet operations for many invocations/worktrees:
   list/filter/sort/status views and fast enter/detach loops with scriptable outputs.
5. Invocation-scoped review/PR/merge command family (`agent review`, `agent pr ...`, `agent merge`).
6. Runner capability model replacing hardcoded allowlists, with first-class targets:
   `claude-code`, `codex`, `amp`, `opencode`, `cursor-cli`, and `droid`.
   Runners without semantic parser support must still work via raw-log fallback.
7. Mutation command `--json` parity in agent surfaces.
8. Reports v2 transition (`report.json` optional artifact, markdown compatibility retained).
9. History-based checkpoint reversion UX for headless invocations:
   users can browse prior messages/tool activity in terminal, navigate with arrow keys,
   choose a point, and revert to the corresponding checkpoint.

## Out of scope (v2.1)

1. Full GUI parity with Conductor desktop/web experience.
2. Full-featured TUI with panes/workspace chrome.
3. Merge queue orchestration.
4. Autonomous policy-driven auto-fix loops from review comments.
5. Any move away from sandbox-first safety model.

## Conductor parity definition

### Functional parity (required for v2.1)

1. Detached/headless invocation can be inspected, chatted with, restarted, and resumed from checkpoint.
2. Invocation has first-class PR/review/merge operations with deterministic outputs.
3. Review + merge decisions can be made from invocation-scoped checks/evidence.

### UI parity (deferred)

1. Diff-viewer-first visual review UX.
2. Checks tab and todo-first merge center UX.
3. Rich timeline/chat panel coupling and workspace visual state.

## Acceptance shape

1. A user can start headless, detach, monitor prompts/messages/tool-use/log history, send follow-up prompts, and continue work without recreating a fresh invocation.
2. A user can restore checkpoint N and restart in one guided command path.
3. A user can also restore from an interactive history selector (arrow-key navigation over prior activity) without manually looking up checkpoint IDs.
4. A user can manage large numbers of invocations/worktrees via list/filter/status flows, enter any selected invocation context, and detach safely.
5. A user can complete review -> PR -> merge from invocation-scoped commands with stable `--json` responses.
6. Target runners `claude-code`, `codex`, `amp`, `opencode`, `cursor-cli`, and `droid` are usable through the same invocation model.
7. Daemon APIs are the canonical source for v2 read/write command behavior.
8. Parity-critical release gates in `release-gates.md` are all satisfied.
