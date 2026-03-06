# concepts

this document explains agency's core abstractions and how they fit together.

## hierarchy

```
Repo ──► Integration Worktree ──► Agent Invocation ──► Sandbox
```

each layer is fully isolated from the one above it. agents can't corrupt your worktree. worktrees can't corrupt your repo.

## repositories

a **repository** is a registered git repo that agency knows about. repos are auto-registered on first use or manually via `agency repo add`.

```bash
agency repo add                       # register current directory
agency repo add --path /path/to/repo  # register a specific path
agency repo ls                        # list registered repos
```

the `--repo` flag on worktree/agent commands accepts a name, ID, or unique prefix and enables CWD-less operation:

```bash
agency worktree create --name my-feature --repo <name|id|prefix>
agency agent ls --all-repos
```

## integration worktrees

a **worktree** is a stable, human-owned branch. it's an isolated git worktree directory with its own branch (`agency/<name>-<shortid>`). agents work in sandboxes underneath it — their changes get landed back here when you're ready.

```bash
agency worktree create --name my-feature           # create
agency worktree create --name my-feature --parent develop  # branch off develop
agency worktree ls [--watch]                        # list (live-updating)
agency worktree show <ref>                          # details
agency worktree open <ref> [--editor cursor]        # open in editor
agency worktree shell <ref>                         # interactive shell
agency worktree path <ref>                          # print path (for scripting)
agency worktree rm <ref> [--force]                  # remove
```

worktrees are independent of agents. you can have zero, one, or many agents running against the same worktree. you can also edit the worktree directly — it's just a git checkout.

## agent invocations

an **invocation** is a single execution of a configured runner. each invocation gets its own **sandbox** — a throwaway git worktree branched off the integration worktree. the agent works in the sandbox and can't touch the integration tree.

runner ids are capability-based (`claude-code`, `codex`, `amp`, `opencode`, `cursor`, `droid`) and runner commands must be explicitly configured in user config (`config.runners`). legacy aliases are accepted: `claude` -> `claude-code`, `cursor-cli` -> `cursor`.

### headed mode (interactive)

```bash
agency agent start --worktree my-feature
```

creates a tmux session with the runner. you interact directly. detach with `Ctrl+b, d`, re-attach with `agency agent attach <id>`.

```bash
agency agent start --worktree my-feature --detached      # don't auto-attach
agency agent start --worktree my-feature --runner codex   # use codex
agency agent start --worktree my-feature --name fix-auth  # human-readable name
```

### headless mode (fire-and-forget)

```bash
agency agent start --worktree my-feature --headless --prompt "Fix the auth bug"
agency agent start --worktree my-feature --headless --prompt-file task.md
```

the daemon supervises headless agents, captures their output, and creates automatic checkpoints. you don't need to be attached.

### monitoring

```bash
agency agent ls [--watch]                     # list agents (live-updating)
agency agent show <ref>                       # details
agency agent attach <ref>                     # attach to headed session
agency agent logs <ref> [--follow]            # view logs (tail -f style)
agency agent logs <ref> --kind stderr         # stderr
agency agent logs <ref> --kind stream         # normalized events
agency agent diff <ref>                       # show changes vs worktree
agency agent diff <ref> --turn <entry_id>     # turn-aware diff context
agency agent review <ref>                      # review verdict + blocking reasons
agency worktree pr sync <worktree-ref>        # push branch + create/update PR
agency worktree merge <worktree-ref> --yes    # verify + merge worktree PR
agency agent open <ref> [--editor cursor]     # open sandbox in editor
```

### lifecycle

```bash
agency agent stop <ref>       # graceful (Ctrl-C / SIGINT)
agency agent kill <ref>       # forceful (SIGKILL)
```

sandbox is preserved after stop/kill for inspection.

## sandboxes

a **sandbox** is the isolated working directory where an agent runs. it's a git worktree branched off the integration worktree's branch (`agency/sandbox-<invocation-id>`). the sandbox is automatically created when you start an agent and cleaned up when you land or discard.

sandboxes live at `${AGENCY_DATA_DIR}/repos/<repo_id>/sandboxes/<invocation_id>/tree/`.

agents never touch the integration worktree directly — all changes happen in the sandbox.

## landing

**landing** is the process of applying sandbox changes back to the integration worktree.

```bash
agency agent land <ref>            # dry-run preview
agency agent land <ref> --apply    # actually apply
agency agent discard <ref>         # throw away sandbox instead
```

landing modes:
- **cherry-pick (default)**: if the sandbox has commits, cherry-picks them onto the integration branch
- **apply (`--apply`)**: if the sandbox has uncommitted changes, applies them as a patch

if there are conflicts, landing aborts and the sandbox is preserved. fix conflicts and retry.

on success, the sandbox worktree, branch, and checkpoint refs are cleaned up. the invocation record is preserved.

## checkpoints

the daemon automatically creates **checkpoints** during agent execution — snapshots of the sandbox tree stored as private git refs. checkpoints let you roll back if an agent goes off the rails.

```bash
agency checkpoint ls --invocation <ref>              # list snapshots
agency checkpoint apply --invocation <ref> <num>     # restore to checkpoint
```

checkpoint behavior:
- created on each mutating tool completion (Edit, Write, Bash, etc.) — every meaningful agent action gets its own checkpoint
- drift safety net via filesystem watcher catches changes not covered by semantic triggers
- deduplicated by tree-SHA (no duplicate snapshots)
- stored as `refs/agency/snapshots/<invocation_id>/<num>` (never pollute branch history)
- final checkpoint created on invocation exit
- certain files excluded from snapshots (`.env`, `*.key`, `*.pem`, `credentials.json`, `secrets.json`)

rollback requires the agent to be stopped first. after rollback, start a new invocation to continue work.

## the daemon

the daemon is the background supervisor for all agent invocations. it auto-starts when you create your first agent. it:

- creates and owns sandbox worktrees
- manages tmux sessions (headed mode)
- supervises runner processes (headless mode)
- captures logs to disk
- creates automatic checkpoints
- derives semantic status from runner output
- detects orphaned/stalled invocations

```bash
agency daemon status [--json]     # check health
agency daemon start               # start in background (default)
agency daemon start --foreground  # start in foreground (for debugging/service managers)
agency daemon stop [--force]      # stop (--force kills active agents)
agency daemon install             # install as OS service (launchd/systemd)
agency daemon uninstall           # remove OS service
```

use `agency daemon install` to have the daemon start automatically on login. on macOS this creates a launchd plist; on Linux a systemd user unit. both are configured to restart on failure.

see [architecture](architecture.md) for daemon internals.

## push and merge

once changes are landed into your worktree, push to GitHub and merge:

```bash
agency push <ref>                          # push branch, create/update PR
agency push <ref> --force-with-lease       # after rebase
agency merge <ref> [--squash]              # verify + merge + cleanup
agency clean <ref>                         # abandon without merging
```

push creates a PR via `gh` CLI. merge runs your verify script, prompts for confirmation, merges the PR, and archives the workspace.

## status values

the daemon derives display status from lifecycle and semantic state:

| status | meaning |
|--------|---------|
| `starting` | invocation is being set up |
| `running` | tmux session or process is active |
| `working` | agent is actively making progress |
| `ready for review` | agent completed successfully |
| `needs input` | agent is waiting for user answer |
| `blocked` | agent cannot proceed |
| `needs attention` | something requires human intervention |
| `finished` | agent exited |
| `failed` | agent or setup failed |

attention flags: `stalled` (no output for >5 min), `orphaned` (tmux session disappeared), `landable` (finished and ready to land).

## data directory

all state lives under the platform data directory:

| platform | path |
|----------|------|
| macOS | `~/Library/Application Support/agency` |
| Linux | `~/.local/share/agency` |

```
${AGENCY_DATA_DIR}/
├── repo_index.json
├── agencyd.sock / agencyd.pid
└── repos/<repo_id>/
    ├── repo.json
    ├── integration_worktrees/<worktree_id>/
    │   ├── meta.json
    │   └── tree/                    ← git worktree
    ├── invocations/<invocation_id>/
    │   ├── meta.json
    │   └── events.jsonl
    └── sandboxes/<invocation_id>/
        ├── tree/                    ← sandbox worktree
        ├── checkpoints.json
        └── logs/
            ├── raw.jsonl            ← stdout
            ├── stderr.log           ← stderr
            └── stream.jsonl         ← normalized events
```
