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
go test ./...
```

### lint

```bash
make lint
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
│   └── commands/         # command implementations
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

See [slice 8 spec](docs/v1/s8/s8_spec.md) for the full roadmap including agents, sandboxes, and the watch TUI.

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
