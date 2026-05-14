# Terminal UI

## Scope

This document covers Bubble Tea and terminal-facing workspace UI rules.

## Rules

- `internal/watch` owns the only Bubble Tea runtime in the codebase.
- `agency watch` and `agency agent <invocation-ref> history` open explicit pages of that same runtime.
- `watch` is a terminal UI over daemon state, not a source of truth.
- The runtime should expose workspace, history, transcript, and logs pages over the same read model.
- The `agency watch` workspace page is one responsive compact surface.
- Agents are the primary action target.
- Repos and worktrees narrow the agent list; they are not top-level modes or tabs.
- Present worktrees are shown by default.
- Archived worktrees appear only through an explicit archived or all-worktrees toggle.
- Narrow layouts may reflow, collapse, or shorten panes, but must not fall back to the legacy stacked workspace.
- Read-only detail panes display selected agent details and are not focus targets.
- Workspace focus cycles through interactive panes with `tab` and `shift+tab`.
- In the workspace, `enter` applies scope in repo/worktree panes and runs the selected agent default action in the agent pane.
- `b` and `esc` broaden workspace scope by clearing worktree scope before repo scope.
- `agency agent <invocation-ref> history` is the canonical invocation inspection surface for turns, checkpoints, transcripts, and logs.
- `agency agent <invocation-ref> attach` stays a thin tmux handoff for running headed invocations, not a parallel inspection workflow.
- `agency agent <invocation-ref> attach` and `agency watch` must use the same daemon-authored headed-session facts instead of local tmux/session heuristics.
- `agency watch` may show headed session status, tmux session name, attach command, recreate availability, and connected tmux clients for the selected invocation, but it still remains a read-model over daemon state.
- `agency agent <invocation-ref> history logs` is the raw log subcommand of that same inspection surface, not a separate top-level workflow.
- Headed interactive log viewing should prefer live terminal output; daemon-backed transcript/log pages remain the inspection surface.
- The runtime should present one canonical invocation `state`: `starting`, `running`, `waiting`, `stopping`, `succeeded`, or `failed`.
- `waiting` covers both turn-complete idle and waiting-for-user cases.
- If the UI needs more detail, show `reason` or the pending question text next to the state instead of inventing a second state label.
- Do not expose separate semantic, display, or readiness state layers in the UI.
- Do not show `blocked` as a primary user-facing state.
- Snapshot loading should compose daemon read APIs, including worktree archival mode, rather than reconstruct state from raw files.
- Interactive terminal checks belong at the command boundary before launching the UI.
- Invocation history UI should live in `internal/watch`, not in a second TUI package.
- UI actions should delegate to canonical command contracts instead of duplicating policy.
- Keep UI model state ephemeral and reconstructable from daemon reads.
