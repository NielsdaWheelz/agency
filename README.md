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

The daemon package includes a comprehensive integration test suite that exercises real server/client communication, real git repos, and real process supervision. A compiled fake runner binary stands in for `claude` — no mocking. The checkpoint package adds 25+ tests covering snapshot creation, duplicate detection, rollback, typed error propagation, and denylist behavior. The landing package adds 12 integration tests (cherry-pick, apply, conflict, nothing-to-land, already-landed/discarded, discard running) plus unit tests for precondition validation and routing.

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
│   │   ├── checkpoint/   # checkpoint engine (fsnotify, snapshots, rollback)
│   │   ├── landing/      # landing service (cherry-pick, apply, discard)
│   │   └── stream/       # stream parser for semantic status
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
- **Landing**: Apply sandbox changes back to integration branch via `agent land`

### Landing Workflow (PR-09)

After an agent completes work in a sandbox, use the landing workflow to apply changes back to the integration worktree:

```bash
# View sandbox changes before landing
agency agent diff 20260131

# Land sandbox changes (cherry-picks commits onto integration)
agency agent land 20260131

# Land uncommitted changes (when sandbox has no commits)
agency agent land 20260131 --apply

# Land with strict base check (fails if integration moved)
agency agent land 20260131 --require-base

# Discard sandbox without landing
agency agent discard 20260131

# Open sandbox in editor for manual inspection
agency agent open 20260131
```

**Landing behavior:**
- **Cherry-pick mode (default)**: If sandbox has commits, cherry-picks them onto integration HEAD
- **Apply mode (--apply)**: If sandbox has uncommitted changes but no commits, applies as patch
- **Conflicts**: If cherry-pick conflicts, landing aborts and sandbox is preserved for resolution
- **Cleanup**: On success, sandbox worktree, branch, and checkpoint refs are removed
- **Artifact exclusion**: Daemon-internal files (`.agency/`) are excluded from dirty checks, staging, and diffs — only user changes are landed

**Discard behavior:**
- Stops running invocations (graceful, then forceful after 5s)
- Removes sandbox worktree, branch, and checkpoint refs
- Preserves invocation record with `landing_status=discarded`

## Daemon (v2)

The agency daemon is the **unified control plane** for all agent invocations — both headed (tmux) and headless (subprocess). As of PR-10, the daemon:
- Creates invocation IDs for all invocation types
- Creates sandbox worktrees atomically
- Manages all invocation metadata
- For headed: Creates and owns tmux sessions
- For headless: Supervises runner processes
- Streams logs to disk (headless mode)
- Handles lifecycle transitions (stop, kill)
- Provides idempotent start operations

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

- **Auto-start**: Daemon starts automatically when any invocation is created (headed or headless)
- **Unified invocation creation**: Single control plane for both headed and headless modes
- **Headed mode management**: Creates and manages tmux sessions for interactive agents
- **Headed reconciliation**: Background loop detects tmux session exit and updates invocation state (PR-11)
- **Log capture**: Captures stdout/stderr to `raw.jsonl` and `stderr.log` (headless mode)
- **Stream parsing**: Parses runner output and writes normalized events to `stream.jsonl`
- **Semantic status**: Derives meaningful status (`working`, `ready_for_review`) from parsed output
- **Graceful stop**: For headed: sends C-c via tmux; for headless: SIGINT → SIGTERM → SIGKILL escalation
- **Forceful kill**: For headed: tmux kill-session; for headless: immediate SIGKILL via process groups
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

### Headed Reconciliation (PR-11)

For headed (tmux) invocations, the daemon runs a background **reconciliation loop** that automatically detects when a tmux session exits and updates the invocation state accordingly.

**How it works:**
- Every 3 seconds, the daemon checks if each headed invocation's tmux session still exists
- If a `running` invocation's session is gone → marks invocation as `finished`
- If a `starting` invocation's session fails to appear for 2+ consecutive checks → marks as `failed`
- Transient tmux errors (connection issues, etc.) are logged but don't cause state transitions

**Startup recovery:**
- On daemon start, all headed invocations are immediately reconciled
- Invocations whose tmux sessions have disappeared are marked finished
- Recently-started invocations (< 30s old) are given time for tmux to initialize

**Shutdown behavior:**
- The reconciliation loop exits cleanly before daemon terminates active invocations
- This prevents race conditions between reconciliation and force-kill during shutdown

This ensures the daemon is the **single authority** for headed invocation lifecycle, making CLI status queries purely read-only.

### Checkpoints (PR-08)

The daemon automatically creates **checkpoints** during headless agent execution, enabling safe rollback if something goes wrong. Checkpoints are stored as private git refs and never pollute branch history.

```bash
# List checkpoints for an invocation
agency checkpoint ls --invocation 20260201
agency checkpoint ls --invocation auth-fix --json

# Restore sandbox to a checkpoint state (invocation must be stopped/finished)
agency checkpoint apply --invocation 20260201 5
agency checkpoint apply --invocation auth-fix 3
```

**Checkpoint creation:**
- **Trigger**: fsnotify watches sandbox tree + 30s polling fallback
- **Debounce**: 3 seconds after last file change before snapshotting
- **Rate limit**: Maximum 1 checkpoint per 10 seconds
- **Final checkpoint**: Created on invocation exit (only if content changed)
- **Deduplication**: Tree-SHA comparison skips checkpoints when content is identical to the last snapshot

**Checkpoint storage:**
```
sandboxes/<invocation_id>/
├── checkpoints.json    # Checkpoint metadata
└── tree/               # Sandbox (watched by fsnotify)

refs/agency/snapshots/<invocation_id>/
├── 1                   # Snapshot commit for checkpoint 1
├── 2                   # Snapshot commit for checkpoint 2
└── ...
```

**Denylist policy:**
Certain files are excluded from snapshots to prevent accidental secret capture:
- `.env`, `.env.*`
- `*.key`, `*.pem`
- `credentials.json`, `secrets.json`

If denylisted files are detected, the checkpoint degrades to tracked-files-only and continues (non-fatal).

**Usage flags:**
```bash
# Exclude untracked files from all checkpoints for this invocation
agency agent start --worktree my-feature --headless --no-include-untracked --prompt "..."
```

**Rollback:**
- Rollback restores the sandbox to exact checkpoint state
- Invocation must be stopped/finished first (use `agent stop` or `agent kill`)
- After rollback, start a new invocation to continue work
- Checkpoint refs remain valid for future rollback
- Typed error codes: `E_CHECKPOINT_NOT_FOUND` (missing ID or snapshot), `E_ROLLBACK_FAILED` (git error), `E_INVOCATION_STILL_RUNNING` (must stop first)

**Events:**
Checkpoint events are emitted to `invocations/<id>/events.jsonl`:
- `agency.checkpoint_created` — checkpoint successfully created
- `agency.checkpoint_failed` — checkpoint creation failed
- `agency.checkpoint_applied` — checkpoint was applied (rollback)
- `agency.checkpoint_denylist_triggered` — denylisted files found, degraded to tracked-only

### Invocation Architecture (PR-10)

Both headed and headless invocations are created and managed by the daemon:

**Headed mode** (`agency agent start --worktree my-feature`):
1. CLI sends RPC to daemon with repo_root, worktree_ref, runner
2. Daemon resolves integration worktree and validates request
3. Daemon atomically creates sandbox worktree and invocation record
4. Daemon creates tmux session with runner command in sandbox directory
5. CLI receives invocation_id, sandbox_path, tmux_session
6. CLI optionally attaches to the tmux session (unless `--detached`)

**Headless mode** (`agency agent start --headless --prompt "..."`):
1. CLI sends RPC to daemon with repo_root, worktree_ref, runner, and prompt
2. Daemon resolves integration worktree and validates request
3. Daemon atomically creates sandbox worktree and invocation record
4. Daemon spawns runner process and streams logs
5. CLI receives invocation_id and sandbox_path, then exits

The CLI **never** writes invocation or sandbox files — the daemon is the **single writer** for all invocation types.

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
