# v2.1 Slice Roadmap

Last updated: 2026-02-16
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
S7 Checks-First Watch Seed (Stretch)
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
- **Acceptance**: all gates listed in `release-gates.md` section A and B are closed with tests.

### Slice S2: Daemon Read Convergence + Sandbox Navigation
- **Goal**: finish daemon-first read architecture and detached navigation basics.
- **Outcome**: v2 reads no longer depend on local store scans in CLI command handlers.
- **Dependencies**: S1.
- **Acceptance**: invocation navigation commands support direct path/shell/open flows with daemon-backed resolution.

### Slice S3: Chat Control Plane + Restart-From-Checkpoint
- **Goal**: enable detached conversational continuation for headless invocations.
- **Outcome**: users can read transcript, send follow-up prompts, and restart from checkpoint in one flow.
- **Dependencies**: S2.
- **Acceptance**: functional parity baseline for detached headless continuity is met.

### Slice S4: Runner Capability Model + Agent Mutation JSON
- **Goal**: remove hard-coded runner assumptions and normalize automation outputs.
- **Outcome**: runner support is capability-driven, and mutation commands expose stable JSON responses.
- **Dependencies**: S3.
- **Acceptance**: no `claude|codex` allowlist gate in start/control-plane paths; fallback behavior is explicit.

### Slice S5: Invocation-Centric Review + PR + Merge
- **Goal**: move review/PR/merge operations under invocation scope.
- **Outcome**: command surfaces for review and PR lifecycle are explicit and scriptable.
- **Dependencies**: S4.
- **Acceptance**: users can run review -> PR -> merge workflow from `agent` command family with deterministic outputs.

### Slice S6: Reports v2 + CLI Ergonomics Cleanup
- **Goal**: reduce friction in report and confirmation/flag ergonomics.
- **Outcome**: JSON report compatibility is available and CLI consistency is improved.
- **Dependencies**: S5.
- **Acceptance**: report strictness is mode-aware and ergonomics targets (`--yes`, high-traffic flags) are complete.

### Slice S7: Checks-First Watch Seed (Stretch)
- **Goal**: provide a minimal checks-first watch surface without full TUI scope.
- **Outcome**: users can monitor review/merge readiness in one terminal-oriented surface.
- **Dependencies**: S6.
- **Acceptance**: checks summary view exists and is useful without introducing GUI/full-TUI complexity.
