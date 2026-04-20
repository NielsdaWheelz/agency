# Git Worktrees

## Scope

This document covers repository gating, integration worktrees, sandboxes, and landing rules.

## Model

- A repo is the user-owned git repository.
- An integration worktree is a stable branch intended for PR, merge, and review flows.
- An invocation sandbox is an isolated per-invocation tree derived from one integration worktree.
- Final PR progression uses `agency worktree <worktree-ref> pr sync` and `agency worktree <worktree-ref> pr merge`.
- Successful worktree PR merge archives the worktree record and removes the tree directory.
- Archived worktrees stay discoverable through `agency worktree ls --all`, but targeted lookup by name or id prefix only considers present worktrees.
- Archived worktrees must be addressed by exact `worktree_id`.
- `agency agent start` and worktree PR flows use the agency config selected by the standard config precedence; the config does not need to be committed to the repo.

## Rules

- Repo discovery must resolve one clean absolute repo root.
- `agency repo add [path]` accepts a positional checkout path. Omitting the path means use cwd.
- `agency init` and `agency doctor` default to cwd and accept `--path <checkout-path>` when targeting a different repo checkout.
- `agency worktree <worktree-ref>` and `agency agent <invocation-ref>` are the canonical default show forms for targeted inspection.
- `agency worktree create <name>` and `agency agent start [<worktree-ref>]` accept an explicit `--repo` selector from any cwd; when omitted, they resolve the repo from the current directory.
- `agency worktree create <name>` defaults an omitted `--base` to the current branch of the selected checkout.
- `agency agent start [<worktree-ref>]` may infer an omitted positional ref only when cwd is inside a present agency integration worktree.
- `agency agent start` should honor explicit `--agency-config` and otherwise resolve agency config in standard precedence order.
- Targeted worktree actions stay target-first, for example `agency worktree <worktree-ref> open`, `agency worktree <worktree-ref> rebase`, and `agency worktree <worktree-ref> pr sync`.
- Creating a worktree requires a registered repo when `--repo` is supplied or a git repo with commits when falling back to cwd, plus a clean base checkout and an existing base branch.
- Integration worktrees are stable collaboration surfaces.
- Sandboxes are disposable execution surfaces.
- Landing and discard flows must preserve enough durable state to explain the outcome after restart.
- Agency-owned `.agency/` files are generated state and must not count as user dirty work.
- Archive scripts must be safe to rerun because `agency worktree <worktree-ref> pr merge` may resume cleanup after the PR is already merged.
- Compare repo, worktree, and sandbox paths only after canonicalization.
