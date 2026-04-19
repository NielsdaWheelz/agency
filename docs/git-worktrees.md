# Git Worktrees

## Scope

This document covers repository gating, integration worktrees, sandboxes, and landing rules.

## Model

- A repo is the user-owned git repository.
- An integration worktree is a stable branch intended for PR, merge, and review flows.
- An invocation sandbox is an isolated per-invocation tree derived from one integration worktree.
- Final PR progression uses `agency worktree pr sync` and `agency worktree pr merge`.
- Successful worktree PR merge archives the worktree record and removes the tree directory.
- `agency agent start` and worktree PR flows use the agency config selected by the standard config precedence; the config does not need to be committed to the repo.

## Rules

- Repo discovery must resolve one clean absolute repo root.
- `agency repo add [path]` accepts a positional checkout path. Omitting the path means use cwd.
- `agency init` and `agency doctor` default to cwd and accept `--path <checkout-path>` when targeting a different repo checkout.
- `agency worktree create` and `agency agent start` accept an explicit `--repo` selector from any cwd; when omitted, they resolve the repo from the current directory.
- `agency worktree create` defaults an omitted `--base` to the current branch of the selected checkout.
- `agency agent start` may infer `--worktree` only when cwd is inside a present agency integration worktree; otherwise `--worktree` remains required.
- `agency agent start` should honor explicit `--agency-config` and otherwise resolve agency config in standard precedence order.
- Creating a worktree requires a registered repo when `--repo` is supplied or a git repo with commits when falling back to cwd, plus a clean base checkout and an existing base branch.
- Integration worktrees are stable collaboration surfaces.
- Sandboxes are disposable execution surfaces.
- Landing and discard flows must preserve enough durable state to explain the outcome after restart.
- Agency-owned `.agency/` files are generated state and must not count as user dirty work.
- Archive scripts must be safe to rerun because `agency worktree pr merge` may resume cleanup after the PR is already merged.
- Compare repo, worktree, and sandbox paths only after canonicalization.
