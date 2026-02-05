# contributing

## prerequisites

- Go 1.21+
- `git`, `tmux`
- `golangci-lint` (for linting)

## build

```bash
go build -o agency ./cmd/agency
```

## run from source

```bash
go run ./cmd/agency --help
```

## test

```bash
# all tests (includes daemon integration tests)
go test ./...

# with race detector (recommended for daemon concurrency)
make test-race

# verbose, specific package
go test ./internal/daemon/ -v -count=1

# skip integration tests (fast, unit-only)
go test ./internal/daemon/ -v -short
```

### test coverage

the test suite exercises real infrastructure — no mocking:

- **daemon integration tests**: real server/client over Unix socket, real git repos, real process supervision with a compiled fake runner binary
- **checkpoint tests** (25+): snapshot creation, deduplication, rollback, typed errors, denylist
- **landing tests** (12+): cherry-pick, apply, conflict, nothing-to-land, already-landed/discarded, discard running
- **read API tests** (42+): status derivation, attention flags, DTO conversion, handlers, filters, pagination, log reading, diff, parameter parsing, routing

## lint

```bash
make lint
```

## full CI check

```bash
make check         # fmt-check, lint, test, build
make verify        # check + race detector + e2e
```

## project structure

```
agency/
├── cmd/agency/              # main entry point
├── internal/
│   ├── cli/cobra/           # Cobra command tree (flag parsing, dispatch)
│   ├── commands/            # command implementations
│   ├── daemon/              # daemon server, handlers, process supervision
│   │   ├── checkpoint/      # checkpoint engine (fsnotify, snapshots, rollback)
│   │   ├── landing/         # landing service (cherry-pick, apply, discard)
│   │   └── stream/          # stream parser for semantic status
│   ├── daemonclient/        # daemon IPC client (HTTP-over-Unix-socket)
│   ├── exec/                # process execution (pty, streaming, process groups)
│   ├── fs/                  # safe filesystem operations (SafeRemoveAll)
│   ├── git/                 # git operations
│   ├── store/               # on-disk persistence (atomic writes, locking)
│   └── config/              # agency.json parsing
└── docs/                    # documentation
```

## conventions

- **no `os/exec` outside `internal/exec`** — all process spawning goes through the exec package
- **no raw `os.RemoveAll`** — use `fs.SafeRemoveAll` with containment checks
- **no `os.Chdir`** — pass working directories explicitly
- **events on mutations** — append-only JSONL, locked, atomic; append failure fails the operation
- **atomic file writes** — temp file + rename, never partial writes
- **strict schema versions** — reject unknown/empty, no silent fallbacks
- **repo-level locks** — acquired before any mutation
- **absolute paths** — path comparisons use clean, symlink-resolved absolute paths

see [architecture](architecture.md) for full internals.

## releasing

see [releasing](releasing.md) for how to cut releases.

## versioning

releases follow semver (v0.1.0, v0.2.0, etc.). dev builds show `agency dev`.

```bash
agency --version
```
