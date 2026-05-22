# Git Worktrees

## Scope

This document covers repository gating, integration worktrees, sandboxes, and landing rules.

## Model

- A repo is the user-owned git repository.
- An integration worktree is a stable branch intended for PR, merge, and review flows.
- An invocation sandbox is an isolated per-invocation tree derived from one integration worktree.
- Integration worktrees and sandboxes are execution surfaces only; repo-shared config belongs to the registered repo canonical root.
- New integration worktrees and sandboxes are created under the repo's resolved checkout root, not under `AGENCY_DATA_DIR`.
- The default checkout root is repo-sibling: `<canonical-repo-parent>/.agency/checkouts/<repo-id>/`.
- A repo may set `agency.json` `execution.checkout_root` to `repo-sibling` or an absolute path. Absolute paths resolve to `<execution.checkout_root>/<repo-id>/`.
- Final PR progression uses `agency worktree <worktree-ref> pr sync` and `agency worktree <worktree-ref> pr merge`.
- `agency worktree <worktree-ref> pr merge` is a daemon-owned durable lifecycle for verify, PR merge, and archive cleanup.
- Worktree PR merge state must be persisted per integration worktree so daemon restart can resume execution or explain the last known outcome.
- Successful worktree PR merge archives the worktree record and removes the tree directory.
- Archived worktrees stay discoverable through `agency worktree ls --all`, but targeted lookup by name or id prefix only considers present worktrees.
- Archived worktrees must be addressed by exact `worktree_id`.
- Targeting is explicit or cwd-derived. Stored context does not participate.
- `agency task start`, `agency task <task-ref> retry`, `agency agent start`, and `agency worktree <worktree-ref> pr merge` resolve config in this order: explicit `--agency-config`, repo-shared `<canonical-repo-root>/agency.json`, then per-repo config under `AGENCY_CONFIG_DIR`.
- An `agency.json` inside an integration worktree or sandbox is not repo-shared config and must not override the canonical repo-root config.
- Managed-tree detection is marker-based plus store metadata, not `AGENCY_DATA_DIR` path-prefix inference.
- Data-dir-owned tree layouts are not supported.

## Rules

- Repo discovery must resolve one clean absolute repo root.
- `agency repo add [path]` accepts a positional checkout path. Omitting the path means use cwd.
- `agency init` and `agency doctor` default to cwd and accept `--path <checkout-path>` when targeting a different repo checkout.
- `agency task <task-ref>`, `agency worktree <worktree-ref>`, and `agency agent <invocation-ref>` are the canonical default show forms for targeted inspection.
- `agency task start <name>`, `agency worktree create <name>`, and `agency agent start` accept an explicit `--repo` selector from any cwd; when omitted, they resolve the repo from the current directory or error.
- `agency task start <name>` creates a durable task, a new integration worktree, and a primary invocation in one daemon-owned mutation. It must not rely on ambient worktree inference for the newly-created worktree.
- `agency task start <name>` and `agency worktree create <name>` default an omitted `--base` to the current branch of the selected checkout chosen by explicit `--repo`, then the current directory.
- `agency agent start --worktree <worktree-ref>` is the explicit worktree override from any cwd.
- When `--worktree` is omitted, `agency agent start` resolves the worktree from cwd only when cwd is inside a present agency integration worktree. Otherwise `--worktree` is required.
- `agency task start`, `agency task <task-ref> retry`, and `agency agent start` should resolve config in this order: explicit `--agency-config`, repo-shared `<canonical-repo-root>/agency.json`, then per-repo config under `AGENCY_CONFIG_DIR`.
- Targeted worktree actions stay target-first, for example `agency worktree <worktree-ref> open`, `agency worktree <worktree-ref> rebase`, and `agency worktree <worktree-ref> pr sync`.
- Creating a worktree requires a registered repo when `--repo` is supplied or, when `--repo` is omitted, a cwd git repo with commits, plus a clean base checkout and an existing base branch.
- Integration worktrees are stable collaboration surfaces.
- Sandboxes are disposable execution surfaces.
- Integration worktree metadata persists the tree path, checkout root, and execution profile.
- Invocation metadata persists the sandbox path, checkout root, and execution profile.
- Merge, verify, and archive flows must resolve repo-shared config from the canonical repo root, not from the integration worktree tree path.
- Repo-shared writes must not target agency-managed integration worktrees or sandboxes.
- Request acceptance for `agency worktree <worktree-ref> pr merge` is distinct from later verify, merge, and archive completion.
- Merge status should be read explicitly from daemon-owned durable worktree state after acceptance, reconnect, or restart.
- Transport disconnects and client-side cancellation must not become merge cancellation after the daemon accepts the work.
- Landing and discard flows must preserve enough durable state to explain the outcome after restart.
- Agency-owned `.agency/` files are generated state and must not count as user dirty work.
- Archive scripts must be safe to rerun because `agency worktree <worktree-ref> pr merge` may resume cleanup after the PR is already merged.
- Compare repo, worktree, and sandbox paths only after canonicalization.
