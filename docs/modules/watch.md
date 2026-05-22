# Watch

## Scope

This document covers `internal/watch`.

## Rules

- `internal/watch` owns the single Bubble Tea runtime for workspace, history, transcript, and logs pages.
- It may expose explicit pages, but should not split into separate TUI runtimes.
- It should compose daemon read APIs into one snapshot.
- It should not own persistence or mutation policy.
- The workspace page is a dense operator surface over agents, repos, and worktrees, not a prose-heavy status dump.
- Agents are the primary workspace target.
- Repos and worktrees are scope controls, not top-level modes.
- Present worktrees are shown by default.
- Archived worktrees appear only through an explicit archived or all-worktrees toggle.
- The workspace starts at all repos, present worktrees, with focus on `Agents`.
- The workspace uses one responsive compact surface across terminal sizes.
- Narrow layouts may reflow, collapse, or shorten panes, but must keep the same responsive workspace model.
- `tab` and `shift+tab` move focus across interactive panes; read-only detail panes are not focus targets.
- `enter` applies repo/worktree scope in scope panes and runs the selected agent default action in `Agents`.
- `b` and `esc` broaden scope by clearing worktree scope, then repo scope; `r` reloads the workspace snapshot.
- Workspace rows should stay single-line and table-like so the selected agent, worktree, repo, state, and latest activity are easy to scan.
- Workspace pages should use one canonical invocation `state`: `starting`, `running`, `waiting`, `stopping`, `succeeded`, or `failed`.
- `waiting` covers both turn-complete idle and waiting-for-user cases.
- When more detail is needed, show `reason` or the pending question text beside the state instead of layering separate semantic, display, or readiness labels.
- `blocked` is not a primary user-facing state.
- Human-readable labels such as invocation names, worktree names, and repo labels should be primary in the UI; canonical ids stay visible but secondary.
- Page headers should make the current agent, worktree, and repo obvious before showing transcript, logs, or history content.
- The workspace detail pane should prefer a small set of high-signal fields: context, state, reason, latest activity, actions, and ids.
- Workspace snapshot loading should use daemon reads scoped by the active repo, worktree, and worktree archival mode; avoid parallel UI filtering for those scopes.
- For headed invocations, the workspace detail pane may also show daemon-authored session facts: session status, tmux session name, connected tmux clients, attach command, and recreate availability.
- Action handling should stay explicit and local to the runtime; avoid generic menu or command frameworks when a direct key/action flow is sufficient.
- History, transcript, and log views should be read-model pages over canonical daemon reads, not parallel UI stacks.
- `agency agent <invocation-ref> history` remains the canonical inspection surface; `attach` is only a thin tmux handoff for running headed invocations.
- `attach` and `watch` should consume the same daemon headed-session read instead of keeping local attachability heuristics in parallel.
- Headed interactive logs should prefer live terminal output over reconstructed UI output.
- Actions should forward into canonical command contracts such as `agency agent <invocation-ref> history`, `agency agent <invocation-ref> attach`, and `agency agent <invocation-ref> restore`.
