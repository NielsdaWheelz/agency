# agency

local-first runner manager: creates isolated git workspaces, launches `claude`/`codex` TUIs in tmux, opens GitHub PRs via `gh`.

## status

**v1 in development** — slice 0 (bootstrap) PR-01 complete.

current progress:
- [x] PR-00: project skeleton + shared contracts
- [x] PR-01: directory resolution + repo discovery + origin parsing
- [ ] PR-02: agency.json schema + validation
- [ ] PR-03: persistence schemas + repo store
- [ ] PR-04: `agency init` command
- [ ] PR-05: `agency doctor` command
- [ ] PR-06: docs + cleanup

## installation

### from source (development)

```bash
go install github.com/NielsdaWheelz/agency@latest
```

### from releases

prebuilt binaries available on [GitHub releases](https://github.com/NielsdaWheelz/agency/releases) for:
- darwin-amd64
- darwin-arm64
- linux-amd64

### homebrew (coming soon)

```bash
brew install NielsdaWheelz/tap/agency
```

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
agency run --title "implement feature X"
agency attach <id>
agency push <id>
agency merge <id>
```

## commands

```
agency init                       create agency.json template
agency run [--title] [--runner] [--parent]
                                  create workspace, setup, start tmux
agency ls                         list runs + statuses
agency show <id> [--path]         show run details
agency attach <id>                attach to tmux session
agency resume <id> [--detached] [--restart]
                                  attach to tmux session (create if missing)
agency stop <id>                  send C-c to runner (best-effort)
agency kill <id>                  kill tmux session
agency push <id> [--force]        push + create/update PR
agency merge <id> [--force]       verify, confirm, merge, archive
agency clean <id>                 archive without merging
agency doctor                     check prerequisites + show paths
```

## development

### build

```bash
go build -o agency ./cmd/agency
```

### test

```bash
go test ./...
```

### run from source

```bash
go run ./cmd/agency --help
go run ./cmd/agency init --help
go run ./cmd/agency doctor --help
```

## project structure

```
agency/
├── cmd/agency/           # main entry point
├── internal/
│   ├── cli/              # command dispatcher (stdlib flag)
│   ├── errors/           # stable error codes
│   ├── exec/             # CommandRunner interface for external commands
│   ├── fs/               # FS interface + atomic write
│   ├── git/              # repo discovery + origin info
│   ├── identity/         # repo_key + repo_id derivation
│   ├── paths/            # XDG directory resolution
│   └── version/          # build version
└── docs/                 # specifications
```

## documentation

- [constitution](docs/v1/constitution.md) — full v1 specification
- [slice roadmap](docs/v1/slice_roadmap.md) — implementation plan
- [slice 0 spec](docs/v1/s0/s0_spec.md) — bootstrap slice details
- [slice 0 PRs](docs/v1/s0/s0_prs.md) — PR breakdown

## license

MIT
