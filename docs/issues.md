# Issues

Tracking deferred fixes and small feature gaps surfaced during real use. Each entry should be concrete enough to act on: symptom, where it lives, and a suggested fix shape. Remove entries as they ship.

## Open

### Add `agency config show` for repo-free config inspection

**Symptom.** No way to load and validate the user `config.json` without being inside a registered repo. `agency doctor` is repo-scoped by design (its `Long` help promises repo diagnostics) and errors with `E_NO_REPO` or `E_REPO_NOT_FOUND` before it ever validates user config.

**Where.** `internal/cli/cobra/cmd_config.go` (only registers `config init`). `internal/cli/cobra/cmd_doctor.go:20` and `internal/commands/doctor.go:128,152` are the repo-coupled paths.

**Fix shape.** New `agency config show` subcommand: load `UserConfig` via `config.LoadUserConfig`, run `config.ValidateUserConfig`, print resolved `defaults`, `runner_defaults`, `execution_profiles`, and `LookPath` results for each `runners.*` / `editors.*` mapping. Plain stdout, plus `--json`. No repo dependency.

### Daemon autostart fails on a fresh install (no data dir)

**Symptom.** First daemon-touching command (`agency repo add`, `agency watch`, etc.) on a clean machine errors with `E_DAEMON_START_FAILED: failed to open daemon log file`. `agency config init` only creates the config dir, not the data dir.

**Where.** `internal/daemonclient/bootstrap.go:45` calls `os.OpenFile(logPath, …)` without ensuring the parent exists. Log path is `<dataDir>/agencyd.log` (`internal/store/store.go:211`); socket is `<dataDir>/agencyd.sock` (`store.go:205`). The data dir is `~/.local/share/agency` on Linux (`internal/paths/xdg.go:77`). `paths.ResolveDirs` is explicitly mkdir-free. Existing data-dir creation lives only inside `store.SaveRepoIndex` (`internal/store/repo_index.go:102`), which is called *after* the daemon is already up.

**Fix shape.** At the top of `StartDaemonBackground`, `os.MkdirAll(filepath.Dir(logPath), 0o700)` before the `OpenFile`. Permissions must be 0700 per the events binding rule. Same parent serves the socket, so this covers both.

**Workaround.** `mkdir -p ~/.local/share/agency` once.

### Worktree navigation is hostile; needs a tmux-backed `attach`

**Symptom.** To work inside a worktree you have to know `<canonical-repo-parent>/.agency/checkouts/<repo_id>/worktrees/<name>-<suffix>/` — opaque `repo_id`, hidden suffix, deeply nested. The two ergonomic helpers that already exist (`agency worktree <ref> shell`, `agency worktree <ref> path`) are not advertised in the README or `worktree --help` quick-start and `shell` dies with the controlling terminal, so it's a poor parallel to `agency agent <invocation> attach`.

**Where.** Existing surfaces: `internal/commands/worktree_navigation.go:37,76,130`. Help text: `internal/cli/cobra/worktree.go:29-51` (the `Long` block lists `create`, `ls`, `pr sync`, `pr merge`, `open` — but not `shell` or `path`).

**Fix shape.** Two parts:

1. **New `agency worktree <ref> attach`** — symmetric with `agency agent <invocation> attach`. Create (or attach to, if it already exists) a tmux session keyed on `worktree_id`, with cwd set to the worktree's `tree_path`, then attach. Survives terminal close; detach with the usual tmux keys; re-attach later from any cwd. Implementation can reuse `internal/tmux` helpers and the same daemon-backed worktree resolution as `shell`/`path`/`open`. Decide whether to ride on top of `agent attach`'s session-clients tracking or stay separate (probably separate — a worktree session has no invocation lifecycle).
2. **Surface `shell` and `path` in `worktree --help` and the README quick-start** so users don't have to spelunk source to find them. Tiny doc PR, can ship before the tmux work.

### `agency worktree create` cannot reuse an existing branch

**Symptom.** Worktree create always materializes a new branch named after the worktree. There is no way to spin up a worktree whose checkout *is* an existing branch (local or remote) — basic `git worktree add <path> <branch>` behavior. `--base` only seeds the starting commit and is validated against `refs/heads/<base>` only (`internal/git/repo.go:265`), so remote-tracking refs like `origin/foo` are also rejected.

**Where.** `internal/integrationworktree/service.go:198` hardcodes `git worktree add -b <branch> <treePath> <baseBranch>`. CLI surface is `internal/cli/cobra/worktree.go:201` — only `--base` exists today.

**Fix shape.** Either:

- Add `--branch <existing-branch>` (mutually exclusive with `--base`) that resolves to `git worktree add <treePath> <branch>` (no `-b`); or
- Add `--reuse-branch` that interprets the positional name as an existing branch and drops `-b`.

Either path needs: store metadata that records "branch was pre-existing" (so archive/cleanup doesn't delete a branch that may be shared), event-log accounting, and a contract decision about whether `pr sync` should still push the branch (probably yes). Also: allow remote-tracking input by fetching + creating the local ref first, the way step 6's manual workaround does.

## Done

_(none yet)_
