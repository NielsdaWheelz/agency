# Git Worktrees

## Scope

This document covers repository gating, integration worktrees, sandboxes, and landing rules.

## Model

- A repo is the user-owned git repository.
- An integration worktree is a stable branch intended for PR, merge, and review flows.
- An invocation sandbox is an isolated per-invocation tree derived from one integration worktree.
- Integration worktrees and sandboxes are execution surfaces only; repo-shared config belongs to the registered repo canonical root.
- Final PR progression uses `agency worktree <worktree-ref> pr sync` and `agency worktree <worktree-ref> pr merge`.
- Successful worktree PR merge archives the worktree record and removes the tree directory.
- Archived worktrees stay discoverable through `agency worktree ls --all`, but targeted lookup by name or id prefix only considers present worktrees.
- Archived worktrees must be addressed by exact `worktree_id`.
- `agency agent start` and worktree PR flows honor explicit `--agency-config`; otherwise they resolve repo-shared config from the registered repo canonical root before falling back to local per-repo config.
- An `agency.json` inside an integration worktree or sandbox is not repo-shared config and must not override the canonical repo-root config.

## Rules

- Repo discovery must resolve one clean absolute repo root.
- `agency repo add [path]` accepts a positional checkout path. Omitting the path means use cwd.
- `agency init` and `agency doctor` default to cwd and accept `--path <checkout-path>` when targeting a different repo checkout.
- `agency worktree <worktree-ref>` and `agency agent <invocation-ref>` are the canonical default show forms for targeted inspection.
- `agency worktree create <name>` and `agency agent start [<worktree-ref>]` accept an explicit `--repo` selector from any cwd; when omitted, they resolve the repo from the current directory.
- `agency worktree create <name>` defaults an omitted `--base` to the current branch of the selected checkout.
- `agency agent start [<worktree-ref>]` may infer an omitted positional ref only when cwd is inside a present agency integration worktree.
- `agency agent start` should honor explicit `--agency-config` and otherwise resolve repo-shared config from the registered repo canonical root, then fall back to local per-repo config under `AGENCY_CONFIG_DIR`.
- Targeted worktree actions stay target-first, for example `agency worktree <worktree-ref> open`, `agency worktree <worktree-ref> rebase`, and `agency worktree <worktree-ref> pr sync`.
- Creating a worktree requires a registered repo when `--repo` is supplied or a git repo with commits when falling back to cwd, plus a clean base checkout and an existing base branch.
- Integration worktrees are stable collaboration surfaces.
- Sandboxes are disposable execution surfaces.
- Merge, verify, and archive flows must resolve repo-shared config from the canonical repo root, not from the integration worktree tree path.
- Repo-shared writes must not target agency-managed integration worktrees or sandboxes.
- Landing and discard flows must preserve enough durable state to explain the outcome after restart.
- Agency-owned `.agency/` files are generated state and must not count as user dirty work.
- Archive scripts must be safe to rerun because `agency worktree <worktree-ref> pr merge` may resume cleanup after the PR is already merged.
- Compare repo, worktree, and sandbox paths only after canonicalization.
