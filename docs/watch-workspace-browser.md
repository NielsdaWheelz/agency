# Watch Workspace Browser

> Target-state spec for the `agency watch` workspace browser cutover.

## Scope

This document covers the `agency watch` workspace page in `internal/watch`.

It does not cover invocation history, transcript, logs, review rendering, daemon mutation policy, or headed-session ownership.

## Related Rules

- [modules/watch.md](modules/watch.md): `internal/watch` ownership and workspace UI rules.
- [terminal-ui.md](terminal-ui.md): Bubble Tea runtime and daemon read-model rules.
- [git-worktrees.md](git-worktrees.md): repo, integration worktree, and invocation sandbox model.
- [simplicity.md](simplicity.md): fewer code paths, no speculative surface.
- [control-flow.md](control-flow.md): explicit finite-state branching.
- [module-apis.md](module-apis.md): use existing daemon DTOs and expose one capability in one form.

## Goals

- Make `agency watch` a scoped resource browser for repos, worktrees, and agents.
- Keep agents as the primary operator surface.
- Let the user narrow agents by repo and worktree without leaving the workspace.
- Keep the UI dense, table-like, and keyboard-first.
- Keep the implementation linear and local to `internal/watch`.
- Use existing daemon read APIs.
- Remove the old flat workspace layout in the same cutover.

## Non-Goals

- No top-level tabs for repos, worktrees, and agents.
- No command palette.
- No free-text search or filter input.
- No new CLI flags.
- No persisted workspace scope.
- No daemon API changes.
- No new TUI runtime.
- No reusable pane framework, table framework, keymap framework, or scope model.
- No compatibility flag or fallback path for the old workspace layout.

## Target Behaviour

`agency watch` opens one workspace page with three list panes and one details pane:

```text
agency watch  all repos / all worktrees  agents:42  worktrees:9  repos:3

Repos          Worktrees          Agents                         Selected
all repos      all worktrees      running  codex  auth-fix ...    Agent: ...
agency         auth-fix           waiting  claude api-clean ...    Worktree: ...
platform       api-clean          failed   codex  billing ...      Repo: ...
```

The panes are ordered left to right:

- `Repos`
- `Worktrees`
- `Agents`
- `Selected`

`Selected` is a read-only details pane for the selected agent. It is not a focus target.

The workspace starts with:

- repo scope: all repos
- worktree scope: all worktrees
- focused pane: agents
- selected agent: first agent in the loaded snapshot

## Pane Behaviour

### Repos

- The first row is `all repos`.
- Repo rows use repo labels first and ids second.
- `enter` on `all repos` clears repo and worktree scope.
- `enter` on a repo sets repo scope to that repo and clears worktree scope.
- After repo scope changes, worktrees and agents reload from daemon reads.
- After repo scope changes, focus moves to `Worktrees`.

### Worktrees

- The first row is `all worktrees`.
- Worktree rows use worktree labels first and ids second.
- When repo scope is set, the pane lists worktrees for that repo.
- When repo scope is all repos, the pane lists worktrees across repos and includes repo labels in each row.
- `enter` on `all worktrees` clears worktree scope and keeps the current repo scope.
- `enter` on a worktree sets repo scope to the worktree repo and worktree scope to that worktree.
- After worktree scope changes, agents reload from daemon reads.
- After worktree scope changes, focus moves to `Agents`.

### Agents

- Agents are the operating target.
- Agent rows stay single-line and table-like.
- Agent rows show state, agent label, worktree label, repo label, and latest activity.
- `enter` keeps the existing agent default action: attach when available, otherwise open actions.
- `h`, `l`, `d`, `x`, `o`, and `p` keep their existing agent meanings.

### Selected

- The details pane shows the selected agent context, state, latest activity, headed-session facts, actions, and ids.
- It renders from the selected invocation DTO and daemon-authored session facts.
- It does not own selection state.

## Keys

| Key | Behaviour |
| --- | --- |
| `tab` | Move focus `Repos -> Worktrees -> Agents -> Repos`. |
| `shift+tab` | Move focus in reverse. |
| `j` / down | Move down in the focused list pane. |
| `k` / up | Move up in the focused list pane. |
| `g` | Move to the first row in the focused list pane. |
| `G` | Move to the last row in the focused list pane. |
| `enter` in `Repos` | Apply repo scope. |
| `enter` in `Worktrees` | Apply worktree scope. |
| `enter` in `Agents` | Run the selected agent default action. |
| `b` | Clear worktree scope, then repo scope. |
| `esc` | Cancel an open prompt/menu; otherwise clear worktree scope, then repo scope. |
| `r` | Reload the workspace snapshot. |
| `q` | Quit. |
| `ctrl+c` | Quit. |

The footer shows the keys for the current focus. The footer is the discoverability mechanism for this cutover.

## Structure

The workspace page remains `pageWorkspace`.

The model stores only the state needed to render and act:

- focused workspace pane
- active repo id
- active worktree id
- selected repo index
- selected worktree index
- selected invocation index
- selected invocation id
- selected invocation repo id

Do not add a `WorkspaceScope` struct. The model fields are the state.

Do not store synthetic `all repos` or `all worktrees` rows. Render them directly in the repo and worktree panes, and account for their index offset locally.

## Snapshot Loading

Workspace snapshot loading takes the active repo id and active worktree id.

It reads:

- `ListRepos`
- `ListWorktrees` with `state=all` and `repo_id` when repo scope is set
- `ListInvocations` with `state=all`, `repo_id` when repo scope is set, and `worktree_ref` when worktree scope is set

Selecting a worktree always sets repo scope to the worktree repo before loading invocations. The daemon invocation list requires `repo_id` when `worktree_ref` is present.

Filtering happens in daemon reads where existing read options support it. The UI does not add a second filtering layer for repo or worktree scope.

## Selection Reconciliation

After every workspace snapshot load:

- Keep active repo scope only if the repo still exists.
- Keep active worktree scope only if the worktree still exists in the active repo scope.
- Keep selected invocation only if it still exists in the filtered agent list.
- Select the first available row when the previous selection is gone.
- Clear selected invocation ids when the filtered agent list is empty.
- Load headed-session facts only for the selected agent.

## Rendering Rules

- Render the workspace as resource panes, not tabs.
- Mark the focused list pane visibly.
- Mark active repo and worktree scope visibly even when the pane is not focused.
- Keep rows single-line.
- Truncate long labels at the render boundary.
- Prefer labels over ids in visible rows.
- Keep ids visible in details.
- Keep empty states short:
  - `no repos`
  - `no worktrees`
  - `no agents`
- Keep the action status and refresh error lines below the panes.

## Architecture

Keep the control flow direct:

1. Key handler updates focus, selection, or active scope.
2. Scope changes trigger one workspace snapshot reload.
3. Snapshot load calls daemon reads with the active scope.
4. Snapshot result replaces the model snapshot.
5. Reconciliation fixes selected indexes and ids.
6. Session facts load for the selected agent.
7. Render functions read the model directly.

Do not add intermediate render models, builders, adapters, manifests, registries, or generic utilities.

Extract a function only when it has a clear payoff:

- it is reused
- it hides substantial incidental complexity
- it keeps a page-level function readable
- it prevents a real safety bug

Inline one-use constants, one-use helpers, and one-use object shapes.

## Final State

- `agency watch` has one workspace browser.
- The old flat invocations-only workspace is gone.
- Repos and worktrees are scope controls, not peer app modes.
- Agents remain the selected action target.
- History, transcript, logs, and review pages continue to hang off the selected agent.
- Existing agent actions continue to delegate to command contracts.
- Existing daemon DTOs remain the read model.
- No new package owns workspace browser policy.

## Key Decisions

- Use panes instead of tabs because repo, worktree, and agent form a hierarchy.
- Use `tab` for focus movement because it matches terminal UI convention and does not steal agent action keys.
- Use `enter` for scope application in parent panes and agent action in the agent pane.
- Use `b` and `esc` to broaden scope because narrowing needs an obvious inverse.
- Keep details out of the focus cycle because it has no list selection.
- Keep text search out of this cutover because repo and worktree narrowing solve the requested navigation problem without another input mode.
- Keep state filters out of this cutover because the current watch read model already uses all invocation and worktree states.

## Files

Docs changed by this spec:

- `docs/watch-workspace-browser.md`
- `docs/index.md`

Implementation files for the cutover:

- `internal/watch/model.go`
- `internal/watch/model_state.go`
- `internal/watch/model_keys.go`
- `internal/watch/model_loaders.go`
- `internal/watch/snapshot.go`
- `internal/watch/view.go`
- `internal/watch/model_test.go`
- `internal/watch/loader_test.go`
- `internal/watch/run_test.go`

Docs to update with the implementation:

- `docs/modules/watch.md`
- `docs/terminal-ui.md`
- `README.md` if user-facing watch help changes there

## Implementation Plan

1. Add workspace focus and scope fields to `model`.
2. Replace the flat invocation selection helpers with explicit repo, worktree, and agent selection helpers.
3. Change workspace snapshot loading to accept active repo and worktree ids.
4. Use daemon read filters for scoped worktrees and invocations.
5. Replace workspace key handling with pane focus, scope application, broadening, and existing agent actions.
6. Replace the old two-panel workspace render with repo, worktree, agent, and selected panes.
7. Keep history, review, transcript, logs, and action execution wired to selected invocation id plus selected invocation repo id.
8. Remove old flat workspace-only code that no longer has a caller.
9. Update tests for loading, reconciliation, key handling, and rendering.
10. Update watch docs and user-facing help.

## Acceptance Criteria

1. `agency watch` opens the scoped workspace browser by default.
2. There is no old flat workspace path, flag, or compatibility mode.
3. Repos, worktrees, agents, and selected details are visible in the workspace.
4. `tab` and `shift+tab` move focus across `Repos`, `Worktrees`, and `Agents`.
5. `enter` on a repo narrows worktrees and agents to that repo.
6. `enter` on a worktree narrows agents to that worktree and its repo.
7. `b` and plain `esc` broaden scope one level when no prompt or menu is open.
8. `q` and `ctrl+c` quit.
9. Agent action keys still operate on the selected agent.
10. History, logs, transcript, and review pages still open for the selected agent.
11. Snapshot loading passes repo and worktree filters to existing daemon reads.
12. Selection survives refresh when the selected row still exists.
13. Selection moves to the first available row when the selected row disappears.
14. Empty repo, worktree, and agent panes render clear empty states.
15. The cutover adds no new package, daemon endpoint, generic widget framework, or workspace scope data model.
16. `go test ./internal/watch ./internal/commands ./internal/cli/cobra` passes.
