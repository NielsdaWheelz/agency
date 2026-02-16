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

1. Deliver invocation-centric recovery and navigation for detached operation.
2. Add headless conversational continuation and restart-from-checkpoint.
3. Make review/PR/merge flows explicit under `agent` and machine-readable.
4. Complete runner-portable execution model and consistent `--json` outputs.
5. Reduce report friction (JSON-compatible, optional stricter sections by mode).
6. Finish migration toward daemon-owned reads/mutations for v2 surfaces.

## In scope (v2.1)

1. `agent path`, `agent shell`, `agent restart` (including checkpoint-based restart path).
2. Chat control plane for headless invocations:
   stream transcript, send follow-up prompt, restart in place, and resume from checkpoint.
3. Invocation-scoped review/PR/merge command family (`agent review`, `agent pr ...`, `agent merge`).
4. Runner capability model replacing `claude|codex` allowlists, with raw-log fallback.
5. Mutation command `--json` parity in agent surfaces.
6. Reports v2 transition (`report.json` optional artifact, markdown compatibility retained).

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

1. A user can start headless, detach, monitor logs/messages, send follow-up prompts, and continue work without recreating a fresh invocation.
2. A user can restore checkpoint N and restart in one guided command path.
3. A user can complete review -> PR -> merge from invocation-scoped commands with stable `--json` responses.
4. Parity-critical release gates in `release-gates.md` are all satisfied.
