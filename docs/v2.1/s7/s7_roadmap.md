# Slice S7: Full-Screen Watch/TUI Seed (Stretch) — PR Roadmap

Last updated: 2026-03-04
Status: active
Upstream spec: `docs/v2.1/s7/s7_spec.md`

Current state: PR-01 is implemented. `agency watch` now launches a full-screen Bubble Tea v2 workspace in interactive terminals, composes snapshots from canonical daemon read/review APIs (`GET /repos`, paged `GET /worktrees`, paged `GET /invocations`, `GET /invocations/{ref}/review`), preserves selection by invocation identity across refresh reorderings, and surfaces recoverable refresh failures in-session without collapsing the workspace. Non-interactive startup fails deterministically with `E_NOT_INTERACTIVE` and an actionable hint. PR-02 remains the next step for delegation-first mutation/actions and explicit `E_SESSION_ENDED` action-path handling.

### PR-01: canonical `agency watch` real-TUI shell + daemon-snapshot readiness workspace
- **goal**: ship a usable full-screen terminal watch shell built on a dedicated TUI framework that composes workspace state from existing daemon read contracts and renders canonical invocation readiness without inventing new readiness logic.
- **builds on**: S6 merged baseline (`agent review`/`pr sync`/`merge` readiness + report contracts stable).
- **acceptance**:
  - in an interactive terminal, `agency watch` enters a full-screen keyboard-navigable workspace, refreshes periodically, and restores the prior shell state cleanly on exit.
  - watch uses Bubble Tea v2 + Bubbles + Lip Gloss (not bespoke ANSI clear/redraw loops) with deterministic terminal lifecycle handling (alt-screen/cursor/input mode setup and teardown).
  - in non-interactive contexts, `agency watch` fails deterministically with interactive-required behavior and an actionable hint.
  - watch workspace state is composed from daemon-owned read surfaces (repo/worktree/invocation + review) and does not scan local store files directly.
  - selecting an invocation shows canonical readiness verdict, blocking reasons, report diagnostics, and navigation context aligned with invocation review semantics.
  - summary/detail rendering makes ready-vs-blocked progression state visible for selected/mixed invocations without redefining progression taxonomy.
  - existing `agent ... --json` machine contracts remain unchanged; watch is additive human-interactive surface only.
- **non-goals**: no action/mutation dispatch from watch in PR-01 (read-only seed only); no new daemon event-stream dependency; no new daemon readiness logic; no watch-specific machine-readable API contract.

### PR-02: delegation-first watch actions + headed session-ended resilience closure (planned after PR-01 merges)
- **goal**: add invocation actions inside watch by delegating to canonical command/contract behavior, including explicit non-destructive handling of ended headed sessions.
- **builds on**: PR-01 merged watch shell and snapshot composition model.
- **acceptance**:
  - watch action paths delegate to canonical invocation/worktree behavior and preserve existing confirmation/error semantics rather than re-implementing policy logic.
  - when entering/attaching to a headed invocation whose tmux session has ended, watch surfaces deterministic session-ended guidance consistent with `E_SESSION_ENDED` contracts and keeps the watch workspace running.
  - action outcomes keep readiness monitoring actionable (users can see progression-blocked vs progression-ready states and why) without changing review/PR/merge truth semantics.
  - recoverable action failures do not collapse watch; users can continue navigation/monitoring in the same session.
  - test coverage asserts delegation contracts and session-ended workspace continuity behavior.
- **non-goals**: no tmux lifecycle redesign to keep sessions alive after runner exit; no GUI/web dashboard scope; no replacement of CLI-first scriptable contracts.
