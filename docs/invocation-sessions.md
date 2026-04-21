# Invocation Sessions

> Normative target-state document for the headed-invocation session-manager cutover. This describes the desired steady state, not necessarily the current code.

## Scope

This document covers local tmux session management for headed invocations across `internal/daemon`, `internal/daemonclient`, `internal/commands`, `internal/watch`, and `internal/tmux`.

It does not cover headless invocation control, checkpoint restore semantics, or remote/browser sharing.

## Goals

- Make `agency watch` the canonical local session manager for headed invocations.
- Keep session mutation and recovery daemon-owned.
- Keep terminal attach and switch at the command boundary.
- Show live tmux client facts for the selected headed invocation.
- Keep the implementation invocation-centric, explicit, and linear.
- Remove duplicated attachability heuristics and parallel paths.

## Non-Goals

- No remote attach, HTTPS, tokens, browser clients, or web sharing.
- No local read-only attach in this cutover.
- No cross-backend abstraction for Zellij, WezTerm, tmate, or future runners.
- No new top-level `agency session` noun.
- No change to headless workflows.
- No change to checkpoint `restore`: it remains sandbox rollback only.
- No embedded terminal inside Bubble Tea.
- No generic session framework or reusable control-plane toolkit.

## Target Behaviour

- `agency watch` stays the only Bubble Tea runtime.
- The workspace stays the canonical local operator surface. It lists invocations, not a second session list.
- For a selected headed invocation, the detail pane shows session facts from daemon reads:
  - session status: `live` or `missing`
  - tmux session name
  - connected client count
  - connected clients, including whether each client is read-only
  - attach command
  - recreate availability
- `enter` on a live headed invocation attaches from a plain terminal and switches the current client when already inside tmux.
- `enter` on a headed invocation with a missing tmux session does not guess. It shows the missing-session result from daemon state and routes the user to explicit recreate.
- `agency agent <invocation-ref> attach` remains the direct local entrypoint for live headed sessions.
- `agency agent <invocation-ref> clients` prints the connected tmux clients for that invocation.
- `agency agent <invocation-ref> recreate` is the only session resurrection path. It keeps the same invocation id and sandbox.
- `agency agent <invocation-ref> history` remains the canonical inspection surface.
- `agency agent <invocation-ref> restore` remains checkpoint restore only.

## Final State

- A headed invocation is the only user-facing session unit. There is no separate top-level session resource in the CLI.
- One headed invocation may have zero or one tmux session and zero or more tmux clients.
- The daemon exposes one explicit read endpoint for headed session facts: `GET /invocations/{ref}/session`.
- The daemon remains the mutable owner for headed session recovery and supervision.
- Command code remains the terminal boundary for tmux client handoff.
- `internal/watch` consumes daemon reads and delegates to canonical commands. It does not shell to tmux and does not own session policy.
- tmux remains the only session backend.
- The codebase has one attach path and one recreate path. The cutover removes duplicate local heuristics rather than keeping both.

## Rules

- Do not add `internal/session`.
- Do not add a backend interface, adapter, registry, manifest, or generic manager layer.
- Do not add a second TUI runtime or a separate session TUI package.
- Do not add a new top-level `agency session` command family.
- Do not persist tmux client snapshots. Connected clients are live read data.
- Do not add a new top-level invocation state for session presence. Session facts are separate from invocation state.
- Replace the current watch-side attachability heuristic with daemon-backed session facts. Do not keep both paths in parallel.
- Route all tmux operations through the existing `tmux.Client` seam.
- Keep new code in the owning packages that already exist: `internal/daemon`, `internal/daemonclient`, `internal/commands`, `internal/watch`, and `internal/tmux`.
- Prefer direct handlers and explicit branches over shared helper layers.
- Only extract a new function, type, constant, or field when it has clear reuse or safety value.

## Key Decisions

- Keep the user model invocation-first, not session-first.
- Copy the worktree-merge ownership pattern: the daemon owns live lifecycle state; the CLI and TUI attach to it.
- Keep `attach` thin at the terminal boundary, but make its preconditions and session facts daemon-authored.
- Keep `watch` as the session-manager UI, not the session authority.
- Keep `restore` and `recreate` separate.
- Keep tmux-specific implementation explicit. Do not hide it behind a generic session abstraction.
- Add one narrow daemon read surface for headed session facts instead of spreading client lists across existing list DTOs.

## Files

- New
  - `docs/invocation-sessions.md`: this target-state spec
- Docs to update in the cutover
  - `docs/terminal-ui.md`
  - `docs/modules/watch.md`
  - `docs/daemon.md`
  - `docs/modules/daemon.md`
  - `README.md`
- Daemon reads
  - `internal/daemon/read_types.go`
  - `internal/daemon/read_handlers.go`
  - `internal/daemon/read_common.go`
  - `internal/daemon/read_session.go`
  - `internal/daemon/read_session_test.go`
- Daemon client
  - `internal/daemonclient/invocation_session.go`
- Commands
  - `internal/cli/cobra/agent_navigation.go`
  - `internal/commands/agent_navigation.go`
  - `internal/commands/watch_tui.go`
- Watch
  - `internal/watch/model.go`
  - `internal/watch/view.go`
  - `internal/watch/run.go`
  - `internal/watch/snapshot.go`
- tmux seam
  - `internal/tmux/client.go`
  - `internal/tmux/client_exec.go`
  - `internal/tmux/*_test.go`

## Implementation Order

1. Add one daemon read surface for headed invocation session facts.
2. Extend the tmux seam only as much as needed to support that read surface.
3. Add `agency agent <invocation-ref> clients`.
4. Cut `agency agent <invocation-ref> attach` over to the daemon-authored session facts.
5. Cut `agency watch` over to the same session facts and remove duplicate local heuristics.
6. Update docs and help text in the same change.

## Acceptance Criteria

1. `agency watch` shows daemon-authored session facts for the selected headed invocation without shelling to tmux directly.
2. `agency watch` can attach or switch to a live headed invocation through the existing command boundary.
3. `agency watch` can show when a headed tmux session is missing without inventing a new invocation state.
4. `agency watch` can route the user to explicit recreate for a missing headed session.
5. `agency agent <invocation-ref> clients` returns the live tmux clients for that invocation.
6. `agency agent <invocation-ref> attach` and `agency watch` use the same authoritative session facts.
7. `agency agent <invocation-ref> history` remains the canonical inspection surface.
8. `agency agent <invocation-ref> restore` still restores sandbox state only.
9. The cutover does not add `internal/session`, a new top-level `agency session` noun, or a multi-backend abstraction.
10. The cutover removes any watch-local session availability heuristic that duplicates daemon session reads.
