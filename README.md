# agency

local-first AI coding agent manager for Mac and Linux. creates isolated git workspaces, launches `claude`/`codex` in tmux, tracks progress with automatic checkpoints, and lands changes back via GitHub PRs.

## installation

### macos (homebrew)

```bash
brew install NielsdaWheelz/tap/agency
```

### linux

```bash
curl -LO https://github.com/NielsdaWheelz/agency/releases/download/v0.1.0/agency_0.1.0_linux_amd64.tar.gz
tar xzf agency_0.1.0_linux_amd64.tar.gz
mkdir -p ~/.local/bin && mv agency ~/.local/bin/
```

### from source

```bash
go install github.com/NielsdaWheelz/agency/cmd/agency@latest
```

## prerequisites

`git`, `tmux`, `gh` (authenticated), and a runner (`claude` or `codex`) on PATH.

## quick start

```bash
cd myrepo
agency repo add                              # register this repo
agency worktree create --name my-feature     # create an isolated branch
agency agent start --worktree my-feature     # launch claude in a tmux session
# Ctrl+b, d to detach from tmux
agency agent ls --watch                      # monitor your agents
agency agent land <invocation-id> --apply    # land changes back to worktree
```

headless (fire-and-forget):

```bash
agency agent start --worktree my-feature --headless --prompt "Fix the auth bug"
agency agent logs <invocation-id> --follow
agency agent chat <invocation-id> --prompt "continue with edge-case tests"
agency agent history <invocation-id> --limit 50   # limit must be 1..500
agency agent checks <invocation-id>               # readiness + blocking reasons
agency agent diff <invocation-id> --turn <entry> # turn-anchored diff context
agency checkpoint ls --invocation <invocation-id>
agency agent restart <invocation-id> --checkpoint 3 --env FAKE_RUNNER_MODE=sleep
agency agent restart <invocation-id> --history     # interactive history selector (tty only)
```

if the original headless start used custom env keys, `agent restart` requires explicitly replaying those keys via `--env KEY=VALUE`.
for non-interactive/scripted use, prefer `--checkpoint`; `--history` is interactive.

## how it works

```
Repo ──► Worktree ──► Agent Invocation ──► Sandbox
(yours)  (stable       (one run of         (isolated copy
         branch)       claude/codex)        agent works in)
```

you register a repo, create worktrees (isolated branches), start agents inside sandboxed copies of those branches, then land the agent's changes back. a background daemon supervises everything — auto-starts on first use.

## documentation

- **[getting started](docs/getting-started.md)** — full walkthrough from zero to merged PR
- **[concepts](docs/concepts.md)** — repos, worktrees, agents, sandboxes, landing, checkpoints
- **[CLI reference](docs/cli.md)** — every command and flag
- **[configuration](docs/configuration.md)** — agency.json, environment variables, shell completion
- **[architecture](docs/architecture.md)** — daemon internals, stream parsing, data model
- **[v2.1 docs](docs/v2.1/README.md)** — consolidated product scope, parity matrix, release gates, roadmap
- **[contributing](docs/contributing.md)** — build, test, lint, project structure

## license

MIT
