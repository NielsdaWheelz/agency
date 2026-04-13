# Git Worktrees

## Scope

This document covers repository gating, integration worktrees, sandboxes, and landing rules.

## Model

- A repo is the user-owned git repository.
- An integration worktree is a stable branch intended for PR, merge, and review flows.
- An invocation sandbox is an isolated per-invocation tree derived from one integration worktree.

## Rules

- Repo discovery must resolve one clean absolute repo root.
- Creating a worktree requires a git repo with commits, a clean parent tree, and an existing parent branch.
- Integration worktrees are stable collaboration surfaces.
- Sandboxes are disposable execution surfaces.
- Landing and discard flows must preserve enough durable state to explain the outcome after restart.
- Compare repo, worktree, and sandbox paths only after canonicalization.
