# v2.1 Slice Roadmap

Last updated: 2026-03-03
Status: active
Mode: L1-style product slice sequencing

## Dependency graph

```text
S0 Docs Consolidation
  |
  v
S1 Platform Hardening Gates
  |
  v
S2 Daemon Read Convergence + Sandbox Navigation
  |
  v
S3 Chat Control Plane + Restart-From-Checkpoint
  |
  v
S4 Runner Capability Model + Agent Mutation JSON
  |
  v
S5 Invocation-Centric Review + PR + Merge
  |
  v
S6 Reports v2 + CLI Ergonomics Cleanup
  |
  v
S7 Full-Screen Watch/TUI Seed (Stretch)
```

## Slices

### Slice S0: Docs Consolidation
- **Goal**: establish one canonical v2.1 doc set.
- **Outcome**: product scope, parity matrix, release gates, and slice roadmap are centralized.
- **Dependencies**: none.
- **Acceptance**: `docs/v2.1/` is the declared source of truth and legacy docs point to it.

### Slice S1: Platform Hardening Gates
- **Goal**: close release-blocking safety and contract integrity issues.
- **Outcome**: P0 closure + parity-critical P1 hardening baseline.
- **Dependencies**: S0.
- **Acceptance**: all issues listed in `constitution.md` Gate A and Gate B are closed with tests.

### Slice S2: Daemon Read Convergence + Sandbox Navigation
- **Goal**: finish daemon-first read architecture and detached/fleet navigation basics.
- **Outcome**: v2 reads no longer depend on local store scans in CLI command handlers, and users can navigate many worktrees/invocations efficiently.
- **Dependencies**: S1.
- **Acceptance**: v2 `agent`/`worktree` reads resolve through daemon APIs (except bootstrap/health fallback), and navigation/list/filter flows support direct path/shell/open/select usage.

### Slice S3: Chat Control Plane + Restart-From-Checkpoint
- **Goal**: enable detached conversational continuation for headless invocations with CLI-first parity for history, restart, turn-aware diff context, and checks-first readiness.
- **Outcome**: users can read full transcript (prompts/messages/tool-use/logs), send follow-up prompts, enter/detach/re-enter sessions, restart from checkpoint in one flow, restore from an interactive history selector, request turn-aware diff context, and inspect checks-first readiness in terminal.
- **Dependencies**: S2.
- **Acceptance**: detached headless continuity supports follow-up prompting, repeated detach/re-entry, explicit checkpoint restore, arrow-key history-based restore, deterministic turn-to-diff mapping, and a scriptable checks-first readiness surface.

### Slice S4: Runner Capability Model + Agent Mutation JSON
- **Goal**: remove hard-coded runner assumptions and normalize automation outputs.
- **Outcome**: runner support is capability-driven for `claude-code`, `codex`, `amp`, `opencode`, `cursor`, and `droid`, and mutation commands expose stable JSON responses.
- **Dependencies**: S3.
- **Acceptance**: no hardcoded runner allowlist in start/control-plane paths; fallback behavior is explicit for unsupported semantic adapters.

### Slice S5: Invocation-Centric Review + PR + Merge
- **Goal**: move review/PR/merge operations under invocation scope.
- **Outcome**: command surfaces for review and PR lifecycle are explicit and scriptable.
- **Dependencies**: S4.
- **Acceptance**: users can run review -> PR -> merge workflow from `agent` command family with deterministic outputs.

### Slice S6: Reports v2 + CLI Ergonomics Cleanup
- **Goal**: reduce friction in report and confirmation/flag ergonomics.
- **Outcome**: one canonical reports-v2 model is available (`report.json` precedence with markdown compatibility), and high-traffic CLI confirmation/flag ergonomics are standardized.
- **Dependencies**: S5.
- **Acceptance**: headless `review`/`pr sync`/`merge` report consumption is strict and machine-parseable, headed/compatibility paths are deterministic fallback-with-diagnostics, and ergonomics targets (`--yes`, canonical high-traffic flags/aliases, open-on-create) are complete.

### Slice S7: Full-Screen Watch/TUI Seed (Stretch)
- **Goal**: provide a full-screen watch/TUI shell that builds on S3 checks-first terminal contracts.
- **Outcome**: users can monitor invocation/review/merge readiness in a richer terminal workspace without introducing GUI scope.
- **Dependencies**: S6.
- **Acceptance**: full-screen terminal watch shell exists and reuses S3 checks/readiness contracts without redefining CLI parity behavior.
