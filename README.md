# agency

Run a team of coding agents on your Mac/Linux.  Instantly create parallel Codex + Claude Code agents in isolated workspaces. See at a glance what they're working on, then review and merge their changes.

local-first runner manager: creates isolated git workspaces, launches `claude`/`codex` TUIs in tmux, opens GitHub PRs via `gh`.

## installation

### macos (homebrew)

```bash
brew install NielsdaWheelz/tap/agency
```

this installs the binary and shell completions (bash and zsh) automatically. restart your shell after installation.

for zsh users: ensure `compinit` is enabled in your `~/.zshrc`:

```bash
autoload -Uz compinit && compinit
```

### linux (manual binary)

download the release tarball from [GitHub Releases](https://github.com/NielsdaWheelz/agency/releases):

```bash
# download and extract (linux/amd64)
curl -LO https://github.com/NielsdaWheelz/agency/releases/download/v0.1.0/agency_0.1.0_linux_amd64.tar.gz
tar xzf agency_0.1.0_linux_amd64.tar.gz

# place on PATH
mkdir -p ~/.local/bin
mv agency ~/.local/bin/
```

ensure `~/.local/bin` is in your PATH:

```bash
export PATH="$HOME/.local/bin:$PATH"
```

add this to `~/.bashrc` or `~/.zshrc` to persist.

for shell completions, see [configuration](docs/configuration.md#shell-completion).

### from source

```bash
go install github.com/NielsdaWheelz/agency/cmd/agency@latest
```

ensure your Go bin directory is on PATH (uses `GOBIN` if set, otherwise `GOPATH/bin`):

```bash
export PATH="$(go env GOPATH)/bin:$PATH"
```

dev builds show `agency dev` for version. completions must be configured manually.

## prerequisites

agency requires:
- `git`
- `gh` (authenticated via `gh auth login`)
- `tmux`
- configured runner (`claude` or `codex` on PATH)

## quick start

```bash
cd myrepo
agency init       # create agency.json + stub scripts
agency doctor     # verify prerequisites
agency run --name feature-x   # creates workspace and enters tmux session
# Ctrl+b, d to detach from tmux when done working
agency push feature-x
agency merge feature-x
```

see [getting started](docs/getting-started.md) for a complete walkthrough.

## documentation

- [getting started](docs/getting-started.md) — setup to merge walkthrough
- [CLI reference](docs/cli.md) — all commands and flags
- [configuration](docs/configuration.md) — agency.json, environment variables, shell completion
- [releasing](docs/releasing.md) — how to cut releases

### specifications (internal)

- [constitution](docs/v1/constitution.md) — full v1 specification
- [slice roadmap](docs/v1/slice_roadmap.md) — implementation plan

## development

### build

```bash
go build -o agency ./cmd/agency
```

### test

```bash
# all tests (includes daemon integration tests)
go test ./...

# with race detector (recommended — catches data races in daemon concurrency)
make test-race

# verbose, specific package
go test ./internal/daemon/ -v -count=1

# skip integration tests (fast, Layer 2 only)
go test ./internal/daemon/ -v -short
```

The daemon package includes a comprehensive integration test suite (28+ tests across 3 layers) that exercises real server/client communication, real git repos, and real process supervision. A compiled fake runner binary stands in for `claude` — no mocking.

### lint

```bash
make lint
```

### full CI check

```bash
make check         # fmt-check, lint, test, build
make verify        # check + race detector + e2e
```

### run from source

```bash
go run ./cmd/agency --help
```

## project structure

```
agency/
├── cmd/agency/           # main entry point
├── internal/             # implementation packages
│   ├── cli/cobra/        # Cobra CLI command tree
│   ├── commands/         # command implementations
│   ├── daemon/           # daemon server, handlers, process supervision
│   ├── daemonclient/     # daemon IPC client
│   └── store/            # on-disk persistence (repos, invocations, worktrees)
└── docs/                 # documentation
```

## integration worktrees (v2)

Slice 8 introduces **integration worktrees** — stable branches you intend to merge, push, or PR. They are independent of any agent invocation and serve as the human-owned workspace where agent work is eventually landed.

```bash
# Create an integration worktree
agency worktree create --name my-feature

# List integration worktrees
agency worktree ls

# Show details
agency worktree show my-feature

# Get path for scripting
cd $(agency worktree path my-feature)

# Open in editor
agency worktree open my-feature

# Open shell in worktree
agency worktree shell my-feature

# Remove worktree (archives record, deletes tree)
agency worktree rm my-feature
```

## Agent Invocations (v2)

Agent invocations are executions of runners (Claude, Codex, etc.) inside isolated sandbox worktrees. Each invocation is independent and runs in its own sandbox derived from an integration worktree's branch.

```bash
# Start a headed (interactive) agent - creates sandbox, launches tmux, attaches
agency agent start --worktree my-feature

# Start in detached mode (don't attach immediately)
agency agent start --worktree my-feature --detached

# Start a headless agent (non-interactive, via daemon)
agency agent start --worktree my-feature --headless --prompt "Fix the bug in auth.go"
agency agent start --worktree my-feature --headless --prompt-file task.md

# Give invocations human-readable names
agency agent start --worktree my-feature --name auth-fix

# List agent invocations
agency agent ls
agency agent ls --worktree my-feature  # filter by worktree

# Show invocation details
agency agent show 20260131
agency agent show auth-fix  # resolve by name

# Attach to a running headed invocation
agency agent attach 20260131

# Stop an invocation gracefully (sends Ctrl-C / SIGINT)
agency agent stop 20260131

# Kill an invocation forcefully
agency agent kill 20260131
```

Key concepts:
- **Sandbox**: Isolated worktree per invocation (runners never touch integration trees)
- **Invocation**: Single agent execution with its own logs, checkpoints, and outcomes
- **Headed mode**: Interactive tmux session (default) - attach with `agent attach`
- **Headless mode**: Non-interactive subprocess execution via daemon - prompts required
- **Invocation names**: Optional human-readable labels, unique among active invocations
- **Landing**: Apply sandbox changes back to integration branch (coming in PR-07)

## Daemon (v2)

The agency daemon is the **control plane** for headless invocations. For headless execution, the daemon:
- Creates invocation IDs
- Creates sandbox worktrees
- Manages all invocation metadata
- Supervises runner processes
- Streams logs to disk
- Handles lifecycle transitions

```bash
# Start daemon (runs in foreground, Ctrl-C to stop)
agency daemon start

# Check daemon status
agency daemon status
agency daemon status --json

# Stop daemon (refuses if active invocations; use --force to terminate all)
agency daemon stop
agency daemon stop --force
```

### Daemon Features

- **Auto-start**: Daemon starts automatically when a headless invocation is created
- **Log capture**: Captures stdout/stderr to `raw.jsonl` and `stderr.log` in sandbox logs
- **Stream parsing**: Parses runner output and writes normalized events to `stream.jsonl`
- **Semantic status**: Derives meaningful status (`working`, `ready_for_review`) from parsed output
- **Graceful stop**: Escalates SIGINT → SIGTERM (5s) → SIGKILL (2s) on `agent stop`
- **Forceful kill**: Immediate SIGKILL via process groups on `agent kill`
- **Orphan detection**: Detects and marks orphaned invocations on restart
- **Idempotency**: Duplicate requests return existing invocation (via client_request_id)
- **API versioning**: CLI checks daemon API version before operations
- **Repo registration**: Automatically registers repositories on first use

### Stream Parsing and Semantic Status (PR-07)

For headless invocations, the daemon parses runner output in real-time to derive semantic status:

**Log files produced:**
```
sandboxes/<invocation_id>/logs/
├── raw.jsonl        # Verbatim runner stdout (exactly as emitted)
├── stderr.log       # Verbatim runner stderr
└── stream.jsonl     # Normalized events (daemon-generated)
```

**Semantic status values:**
- `working` — Agent is actively working (any assistant/command activity)
- `ready_for_review` — Agent has completed successfully (result:success / agent_message)

**Normalized events (stream.jsonl):**
Each line contains a JSON event with a stable schema across runners:
```json
{
  "schema_version": "1.0",
  "seq": 1,
  "timestamp": "2026-02-01T12:00:00Z",
  "invocation_id": "20260201-a1b2",
  "runner": "claude",
  "kind": "session_start|message|tool_start|tool_end|final|error|usage|parse_error",
  "data": { "..." }
}
```

**Supported runners:**
- Claude Code (`claude -p --output-format stream-json --verbose`)
- Codex CLI (`codex exec --json`)

**Error handling:**
- Parse errors do not crash the daemon or fail the invocation
- Malformed lines emit `kind=parse_error` events (throttled to prevent flooding)
- `raw.jsonl` always contains verbatim output regardless of parse success
- Final lines without trailing newlines are still captured and parsed

**Status persistence:**
- Semantic status is written to `InvocationMeta.semantic_status`
- Updates are throttled to 500ms and only written on actual change
- Final status is always persisted on invocation exit

### Headless Mode Architecture

When you run `agency agent start --headless`:
1. CLI sends a single RPC to the daemon with repo_root, worktree_ref, runner, and prompt
2. Daemon resolves the integration worktree and validates the request
3. Daemon atomically creates sandbox worktree and invocation record
4. Daemon spawns the runner process and streams logs
5. CLI receives invocation_id and sandbox_path, then exits

The CLI **never** writes invocation or sandbox files for headless mode — the daemon is the single writer.

### Daemon-Owned Worktree Mutations

Integration worktree create/remove operations are daemon-owned for single-writer consistency:

```bash
# Create worktree (routes through daemon RPC)
agency worktree create --name my-feature

# Remove worktree (routes through daemon RPC)
agency worktree rm my-feature [--force]
```

Key behaviors:
- **Atomic creation**: Partial failures roll back worktree, branch, and record
- **Repo lock**: Daemon acquires repo lock before name check/branch generation
- **Idempotency**: Duplicate create requests return existing worktree
- **Active invocation guard**: rm fails if agents are running (unless --force)
- **Force escalation**: --force sends SIGINT → 5s wait → SIGKILL before removing

Read-only commands (ls, show, path, open, shell) remain CLI-local for performance.

See [slice 8 spec](docs/v1/s8/s8_spec.md) for the full roadmap including checkpoints and the watch TUI.

## cli framework

agency uses [Cobra](https://github.com/spf13/cobra) for command-line parsing. This provides:
- auto-generated shell completions (bash, zsh)
- built-in help for all commands
- consistent flag parsing

## versioning

releases follow semantic versioning (v0.1.0, v0.2.0, etc.).

```bash
agency --version
```

## releasing (contributors)

see [docs/releasing.md](docs/releasing.md).

## license

MIT
