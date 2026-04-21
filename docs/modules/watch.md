# Watch

## Scope

This document covers `internal/watch`.

## Rules

- `internal/watch` owns the single Bubble Tea runtime for workspace, history, transcript, and logs pages.
- It may expose explicit pages, but should not split into separate TUI runtimes.
- It should compose daemon read APIs into one snapshot.
- It should not own persistence or mutation policy.
- The workspace page should be a dense operator surface over invocations, not a prose-heavy status dump.
- Workspace rows should stay single-line and table-like so the selected agent, worktree, repo, state, and latest activity are easy to scan.
- Workspace pages should use one canonical invocation `state`: `starting`, `running`, `waiting`, `stopping`, `succeeded`, or `failed`.
- `waiting` covers both turn-complete idle and waiting-for-user cases.
- When more detail is needed, show `reason` or the pending question text beside the state instead of layering separate semantic, display, or readiness labels.
- `blocked` is not a primary user-facing state.
- Human-readable labels such as invocation names, worktree names, and repo labels should be primary in the UI; canonical ids stay visible but secondary.
- Page headers should make the current agent, worktree, and repo obvious before showing transcript, logs, or history content.
- The workspace detail pane should prefer a small set of high-signal fields: context, state, reason, latest activity, next action, and ids.
- Action handling should stay explicit and local to the runtime; avoid generic menu or command frameworks when a direct key/action flow is sufficient.
- History, transcript, and log views should be read-model pages over canonical daemon reads, not parallel UI stacks.
- `agency agent <invocation-ref> history` remains the canonical inspection surface; `attach` is only a thin tmux handoff for running headed invocations.
- Headed interactive logs should prefer live terminal output over reconstructed UI output.
- Actions should forward into canonical command contracts such as `agency agent <invocation-ref> history`, `agency agent <invocation-ref> attach`, and `agency agent <invocation-ref> restore`.
