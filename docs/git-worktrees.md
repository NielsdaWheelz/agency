# Git Worktrees

## Scope

This document covers repository gating, integration worktrees, sandboxes, and landing rules.

## Model

- A repo is the user-owned git repository.
- An integration worktree is a stable branch intended for PR, merge, and review flows.
- An invocation sandbox is an isolated per-invocation tree derived from one integration worktree.
- Final PR progression uses `agency worktree pr sync` and `agency worktree pr merge`.
- Successful worktree PR merge archives the worktree record and removes the tree directory.

## Rules

- Repo discovery must resolve one clean absolute repo root.
- `agency worktree create` and `agency agent start` require an explicit `--repo` selector and must not infer repository context from the current directory.
- Creating a worktree requires a registered repo, a git repo with commits, a clean parent tree, and an existing parent branch.
- Integration worktrees are stable collaboration surfaces.
- Sandboxes are disposable execution surfaces.
- Landing and discard flows must preserve enough durable state to explain the outcome after restart.
- Archive scripts must be safe to rerun because `agency worktree pr merge` may resume cleanup after the PR is already merged.
- Compare repo, worktree, and sandbox paths only after canonicalization.
