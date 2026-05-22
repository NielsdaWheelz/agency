# Issues

Tracking deferred fixes and small feature gaps surfaced during real use. Each entry should be concrete enough to act on: symptom, where it lives, and a suggested fix shape. Remove entries as they ship.

## Open

### Add `agency config show` for repo-free config inspection

**Symptom.** No way to load and validate the user `config.json` without being inside a git repo. `agency doctor` is repo-scoped by design (its `Long` help promises repo diagnostics) and errors with `E_NO_REPO` before it ever validates user config; inside an unregistered git repo, user config is validated before `E_REPO_NOT_FOUND`.

**Where.** `internal/cli/cobra/cmd_config.go` (only registers `config init`). `internal/cli/cobra/cmd_doctor.go:22` and `internal/commands/doctor.go:128,152` are the repo-coupled paths.

**Fix shape.** New `agency config show` subcommand: load `UserConfig` via `config.LoadUserConfig`, run `config.ValidateUserConfig`, print resolved `defaults`, `runner_defaults`, `execution_profiles`, and `LookPath` results for each `runners.*` / `editors.*` mapping. Plain stdout, plus `--json`. No repo dependency.

### Worktree navigation needs a tmux-backed `attach`

**Symptom.** `agency worktree <ref> path` and `agency worktree <ref> shell` cover one-shot navigation, but `shell` dies with the controlling terminal. There is no tmux-backed worktree session parallel to `agency agent <invocation> attach`.

**Where.** Existing one-shot surfaces: `internal/commands/worktree_navigation.go:37,76,130`.

**Fix shape.** Add `agency worktree <ref> attach`, symmetric with `agency agent <invocation> attach`. Create (or attach to, if it already exists) a tmux session keyed on `worktree_id`, with cwd set to the worktree's `tree_path`, then attach. Survives terminal close; detach with the usual tmux keys; re-attach later from any cwd. Implementation can reuse `internal/tmux` helpers and the same daemon-backed worktree resolution as `shell`/`path`/`open`. Decide whether to ride on top of `agent attach`'s session-clients tracking or stay separate (probably separate — a worktree session has no invocation lifecycle).

### `agency worktree create` cannot reuse an existing branch

**Symptom.** Worktree create always materializes a new branch named after the worktree. There is no way to spin up a worktree whose checkout *is* an existing branch (local or remote) — basic `git worktree add <path> <branch>` behavior. `--base` only seeds the starting commit and is validated against local heads only (`git.BranchExists` in `internal/git/repo.go`), so remote-tracking refs like `origin/foo` are also rejected.

**Where.** `internal/integrationworktree/service.go` builds `git worktree add -b <branch> <treePath> <baseBranch>`. `internal/commands/worktree.go` validates `--base` through `git.BranchExists`, and `internal/cli/cobra/worktree.go` exposes only `--base` for `worktree create`.

**Fix shape.** Either:

- Add `--branch <existing-branch>` (mutually exclusive with `--base`) that resolves to `git worktree add <treePath> <branch>` (no `-b`); or
- Add `--reuse-branch` that interprets the positional name as an existing branch and drops `-b`.

Either path needs: store metadata that records "branch was pre-existing" (so archive/cleanup doesn't delete a branch that may be shared), event-log accounting, and a contract decision about whether `pr sync` should still push the branch (probably yes). Also: allow remote-tracking input by fetching + creating the local ref first.

## Done

_(none yet)_
