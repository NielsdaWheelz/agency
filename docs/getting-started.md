# getting started

this guide walks through agency from setup to merge.

## how agency works

agency creates isolated git workspaces, launches AI agents (configured runners) inside them via tmux, tracks their progress with automatic checkpoints, and helps you land and merge the results. a background daemon supervises everything.

```
YOUR REPO                        AGENCY (managed by daemon)
/projects/myapp/                 ~/Library/Application Support/agency/repos/<id>/
└── .git/                        ├── integration_worktrees/
                                 │   └── my-feature/
                                 │       └── tree/          ← your stable branch
                                 ├── sandboxes/
                                 │   └── <invocation>/
                                 │       ├── tree/          ← agent's workspace
                                 │       └── logs/          ← captured output
                                 └── invocations/
                                     └── <invocation>/
                                         └── meta.json     ← status, checkpoints

HIERARCHY:

  Repo ──► Worktree ──► Agent Invocation ──► Sandbox
  (yours)  (stable       (one run of         (isolated copy
           branch)       configured runner)   agent works in)

LIFECYCLE:

  ┌────────────────┐   ┌──────────────┐   ┌──────────────┐   ┌──────────────┐
  │worktree create │──►│ agent start  │──►│  agent land  │──►│ push / merge │
  └────────────────┘   └──────────────┘   └──────────────┘   └──────────────┘
        │                    │                   │                   │
        ▼                    ▼                   ▼                   ▼
    creates             launches            applies              pushes to
    isolated            configured runner   sandbox changes      GitHub +
    branch              in sandbox          to worktree          merges PR

  DETACH FROM TMUX: press Ctrl+b, then d (session keeps running)
  RE-ATTACH: agency agent attach <id>
```

## prerequisites

agency requires:
- `git`
- `tmux`
- `gh` (authenticated via `gh auth login`)
- explicit runner mappings in user config (`$AGENCY_CONFIG_DIR/config.json`; defaults to `~/Library/Preferences/agency/config.json` on macOS and `~/.config/agency/config.json` on Linux)

example:

```json
{
  "version": 1,
  "defaults": {
    "runner": "claude-code",
    "editor": "code"
  },
  "runners": {
    "claude-code": "claude",
    "codex": "codex"
  }
}
```

supported canonical runner ids: `claude-code`, `codex`, `amp`, `opencode`, `cursor`, `droid`.
legacy aliases are accepted: `claude` -> `claude-code`, `cursor-cli` -> `cursor`.

verify everything is set up:

```bash
agency doctor
```

## step 1: register your repo

```bash
cd /path/to/your/repo
agency repo add
```

this tells agency about your repo. you only need to do this once per repo. repos are also auto-registered on first use.

```bash
agency repo ls                    # list registered repos
agency repo show <repo-id>       # show details
```

### optional: initialize v1 scripts

if you want to use `agency push` and `agency merge` (which run verify/archive scripts), initialize the repo with v1 config:

```bash
agency init
```

this creates:
```
your-repo/
├── agency.json                    # configuration
├── CLAUDE.md                      # runner protocol (status reporting)
└── scripts/
    ├── agency_setup.sh            # runs BEFORE ai starts (install deps)
    ├── agency_verify.sh           # runs to check work (tests/lint)
    └── agency_archive.sh          # runs on cleanup
```

commit these files — worktrees only contain committed files:

```bash
git add agency.json CLAUDE.md scripts/
git commit -m "add agency configuration"
```

see [configuration](configuration.md) for details on `agency.json` and scripts.

## step 2: create a worktree

a **worktree** is an isolated git branch + directory. it's the stable "integration branch" that you own. agents work in sandboxes underneath it.

```bash
agency worktree create --name add-user-auth
```

options:
- `--parent develop` — branch off a specific branch (default: current branch)
- `--open` — open in editor immediately after creation
- `--editor cursor` — pick your editor

list and inspect worktrees:

```bash
agency worktree ls                          # list all
agency worktree ls --watch                  # live-updating view
agency worktree show add-user-auth          # details
```

open a worktree in your editor or shell:

```bash
agency worktree open add-user-auth                   # opens in $EDITOR
agency worktree open add-user-auth --editor cursor    # pick editor
agency worktree shell add-user-auth                   # interactive shell
cd $(agency worktree path add-user-auth)              # cd into it
```

## step 3: start an agent

an **agent invocation** is one execution of a configured runner inside an isolated sandbox. the sandbox is branched off your worktree — the agent can't mess up your integration branch.

### headed mode (interactive)

```bash
agency agent start --worktree add-user-auth
```

this creates a tmux session with your configured default runner. you interact directly.

- **detach** (keep running): press `Ctrl+b` then `d`
- **re-attach**: `agency agent attach <invocation-id>`

```bash
agency agent start --worktree add-user-auth --detached      # start without attaching
agency agent start --worktree add-user-auth --runner codex   # use codex
agency agent start --worktree add-user-auth --name fix-auth  # human-readable name
```

### headless mode (fire-and-forget)

```bash
# inline prompt
agency agent start --worktree add-user-auth --headless \
  --prompt "Implement JWT-based user authentication with login and logout endpoints"

# prompt from file (for complex tasks)
agency agent start --worktree add-user-auth --headless --prompt-file task.md
```

headless agents run as daemon subprocesses. the daemon captures logs, creates automatic checkpoints, and tracks status.

## step 4: monitor your agents

```bash
agency watch                                 # full-screen readiness workspace (enter/o/p actions, q to exit)
agency agent ls                              # list all agents
agency agent ls --worktree add-user-auth     # filter to one worktree
agency agent ls --all-repos                  # across all repos
agency agent show <invocation-id>            # details on one agent
```

you can use invocation IDs, name prefixes, or the `--name` you gave at start:

```bash
agency agent show fix-auth                   # resolve by name
agency agent show 2026                       # resolve by ID prefix
```

## step 5: read agent logs

```bash
agency agent logs <invocation-id>                 # raw stdout (default)
agency agent logs <invocation-id> --follow        # tail -f style, live updates
agency agent logs <invocation-id> --kind stderr   # stderr
agency agent logs <invocation-id> --kind stream   # normalized event stream
agency agent logs <invocation-id> --offset 1024   # start from byte offset
agency agent history <invocation-id> --limit 50   # unified timeline (limit must be 1..500)
agency agent chat <invocation-id> --prompt "continue with tests"
```

## step 6: work with checkpoints

the daemon automatically snapshots the sandbox every ~10 seconds as the agent works. if things go sideways, you can roll back.

```bash
# list checkpoints
agency checkpoint ls --invocation <invocation-id>
agency checkpoint ls --invocation <invocation-id> --json

# roll back (agent must be stopped first)
agency agent stop <invocation-id>
agency checkpoint apply --invocation <invocation-id> 3
```

after rollback, start a new invocation to continue work.

## step 7: review what the agent did

```bash
# see changes vs the integration worktree
agency agent diff <invocation-id>
agency agent diff <invocation-id> --turn <entry_id>     # turn-aware context
agency agent diff <invocation-id> --turn-range <a>..<b> # inclusive range

# open the sandbox in your editor
agency agent open <invocation-id>
agency agent open <invocation-id> --editor cursor

# check readiness blockers before landing/review
agency agent review <invocation-id>
```

## step 8: land changes into your worktree

when you're happy with the agent's work, **land** its sandbox changes back into your integration worktree:

```bash
agency agent land <invocation-id>            # dry-run preview (shows what would happen)
agency agent land <invocation-id> --apply    # actually apply changes
```

landing behavior:
- **cherry-pick mode (default)**: cherry-picks sandbox commits onto your worktree
- **apply mode (--apply)**: applies uncommitted changes as a patch
- if there are conflicts, landing aborts and the sandbox is preserved

or throw away the sandbox entirely:

```bash
agency agent discard <invocation-id>
```

## step 9: stop or kill agents

```bash
agency agent stop <invocation-id>    # graceful (sends Ctrl-C / SIGINT)
agency agent kill <invocation-id>    # forceful (SIGKILL)
```

sandbox is preserved after stop/kill for inspection.

## step 10: sync PR for the worktree

once changes are landed, sync the integration branch and PR using the worktree ref:

```bash
agency worktree pr sync <worktree-ref>
```

what happened:
1. resolved worktree -> integration branch
2. pushed the branch to origin
3. created or updated the branch-scoped GitHub PR
4. evaluated reports v2 canonically (`.agency/report.json` authoritative over `.agency/report.md`)
5. synced canonical report body (or deterministic bounded fallback body) to PR body

report behavior for worktree PR sync:
- compatibility-first fallback with explicit diagnostics when canonical report contract is invalid

policy flags:

```bash
agency worktree pr sync <worktree-ref> --allow-dirty      # allow dirty integration tree
agency worktree pr sync <worktree-ref> --force-with-lease # safe force push after rebase
agency worktree pr sync <worktree-ref> --json             # machine-readable outcome
```

legacy compatibility still exists:

```bash
agency push <worktree-name>
```

## step 11: merge and cleanup

```bash
agency worktree merge <worktree-ref> --yes
```

what happened:
1. resolved worktree -> branch -> PR identity
2. ran `scripts/agency_verify.sh` in worktree-scoped non-interactive mode
3. merged the PR via `gh pr merge` with your selected strategy
4. evaluated the same reports-v2 contract used by worktree PR sync (compatibility-first with diagnostics)
5. persisted verify/merge logs under worktree state for auditability
6. appended merge lifecycle events to worktree event history

merge options:
```bash
agency worktree merge <worktree-ref> --yes                     # script-safe confirmation
agency worktree merge <worktree-ref> --squash --yes            # squash merge (default)
agency worktree merge <worktree-ref> --merge --yes             # regular merge
agency worktree merge <worktree-ref> --rebase --yes            # rebase merge
agency worktree merge <worktree-ref> --no-delete-branch --yes  # keep remote branch
agency worktree merge <worktree-ref> --json --yes              # machine-readable outcome
agency worktree update <worktree-ref> --json                   # fetch + rebase with typed conflict errors
```

legacy compatibility command still exists:

```bash
agency merge <worktree-ref>
```

## alternative: abandon a run

```bash
agency clean add-user-auth
```

this deletes the worktree and tmux session but does NOT merge anything.

## the daemon

the daemon is the background supervisor for all agent invocations. it auto-starts when you create agents, but you can manage it directly:

```bash
agency daemon status          # is it running?
agency daemon status --json   # machine-readable
agency daemon start           # start in background (default)
agency daemon start --foreground  # start in foreground (for debugging)
agency daemon stop            # graceful shutdown
agency daemon stop --force    # kill active agents and stop
agency daemon install         # install as OS service (launchd/systemd)
agency daemon uninstall       # remove OS service
```

to have the daemon start automatically on login, install it as an OS service:

```bash
agency daemon install         # writes launchd plist (macOS) or systemd unit (Linux)
```

daemon responsibilities:
- creates and owns sandbox worktrees
- manages tmux sessions (headed mode)
- supervises runner processes (headless mode)
- captures logs to disk
- creates automatic checkpoints (fsnotify-based)
- derives semantic status from runner output
- detects orphaned/stalled invocations

## full workflow example

```bash
# 1. register repo (once)
agency repo add --path .

# 2. create isolated worktree
agency worktree create --name auth-refactor

# 3. fire up a headless agent
agency agent start --worktree auth-refactor --headless \
  --prompt-file tasks/refactor-auth.md

# 4. monitor it
agency watch
agency agent logs <id> --follow

# 5. check snapshots
agency checkpoint ls --invocation <id>

# 6. went sideways? roll back
agency agent stop <id>
agency checkpoint apply --invocation <id> 3

# 7. happy? land it
agency agent land <id> --apply

# 8. review manually
agency worktree open auth-refactor --editor cursor

# 9. review + PR sync
agency agent review <id>
agency worktree pr sync auth-refactor

# 10. merge
agency worktree merge auth-refactor --yes --squash
```

## command quick reference

```
SETUP
  agency repo add [--path <path>]           register a repo
  agency repo ls                            list repos
  agency doctor                             check prerequisites
  agency init                               create agency.json + stub scripts

WORKTREES (stable branches you own)
  agency worktree create --name <name>      create worktree
  agency worktree ls [--watch]              list worktrees
  agency worktree show <ref>                show details
  agency worktree open <ref>                open in editor
  agency worktree shell <ref>               shell into it
  agency worktree path <ref>                print path
  agency worktree rm <ref>                  remove worktree
  agency worktree pr sync <ref>             push branch + create/update PR
  agency worktree merge <ref> --yes         verify + merge worktree PR
  agency worktree update <ref>              fetch + rebase onto parent branch

AGENTS (AI executions in sandboxes)
  agency agent start --worktree <ref>       start headed agent
    --headless --prompt "..."               start headless agent
    --headless --prompt-file task.md        start headless with file
  agency watch                              full-screen readiness workspace
  agency agent ls [--watch]                 list agents
  agency agent show <ref>                   show details
  agency agent enter <ref>                  attach to tmux session (canonical)
  agency agent attach <ref>                 attach compatibility alias
  agency agent logs <ref> [--follow]        view logs
  agency agent history <ref> [--limit <n>]  unified timeline (n in 1..500)
  agency agent chat <ref> --prompt "..."    send follow-up prompt to headless run
  agency agent diff <ref>                   show sandbox changes
  agency agent diff <ref> --turn <entry_id> turn-aware diff context
  agency agent review <ref>                 review verdict + blocking reasons
  agency agent open <ref>                   open sandbox in editor
  agency agent stop <ref>                   graceful stop
  agency agent kill <ref>                   forceful kill
  agency agent land <ref> [--apply]         land changes to worktree
  agency agent discard <ref>                throw away sandbox

CHECKPOINTS (automatic sandbox snapshots)
  agency checkpoint ls --invocation <ref>   list checkpoints
  agency checkpoint apply --invocation <ref> <num>  rollback

DAEMON (background supervisor)
  agency daemon status                      check daemon health
  agency daemon start                       start daemon (background)
  agency daemon start --foreground          start daemon (foreground)
  agency daemon stop [--force]              stop daemon
  agency daemon install                     install as OS service
  agency daemon uninstall                   remove OS service

PUSH & MERGE (v1 commands)
  agency push <ref>                         push + create PR
  agency merge <ref> [--squash]             verify + merge + cleanup
  agency verify <ref>                       run verify script
  agency clean <ref>                        abandon run
  agency resolve <ref>                      conflict resolution guidance

SESSION CONTROL (v1 commands)
  agency attach <ref>                       compatibility alias for agent attach/enter
  agency resume <ref>                       attach (create if needed)
  agency stop <ref>                         send Ctrl+C
  agency kill <ref>                         kill session
```

see also:
- [concepts](concepts.md) — deeper explanation of repos, worktrees, agents, sandboxes
- [CLI reference](cli.md) — every command and flag
- [configuration](configuration.md) — agency.json, environment variables, shell completion
- [architecture](architecture.md) — daemon internals, stream parsing, data model
