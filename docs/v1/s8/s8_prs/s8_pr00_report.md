# PR-00 Report: Cobra Migration + Command Skeletons

## Summary of Changes

This PR migrates the agency CLI from a hand-written command dispatcher to [Cobra](https://github.com/spf13/cobra), the industry-standard Go CLI framework. The migration preserves all existing command semantics while enabling future subcommand expansion.

### Key Changes

1. **Added Cobra dependency** (`github.com/spf13/cobra v1.10.2`)

2. **Created new CLI structure** in `internal/cli/cobra/`:
   - `root.go` - Root command with global flags
   - `cmd_*.go` - 17 command files (init, doctor, run, ls, show, path, open, attach, resume, stop, kill, push, verify, merge, clean, resolve, version)
   - `completion.go` - Cobra-generated completion scripts
   - `worktree.go`, `agent.go`, `watch.go` - Empty v2 command shells
   - `root_test.go` - Comprehensive CLI tests

3. **Updated main.go** to use Cobra root command

4. **Deleted legacy files**:
   - `internal/cli/dispatch.go` (1585 lines of manual dispatch code)
   - `internal/cli/dispatch_test.go` (test for old dispatcher)
   - `internal/commands/completion.go` (handwritten bash/zsh scripts)
   - `internal/commands/completion_test.go` (tests for handwritten completions)

5. **Updated documentation**:
   - `README.md` - Added CLI framework section
   - `docs/cli.md` - Updated to reflect Cobra-based CLI

## Problems Encountered

1. **go.mod indirect dependency**: After initial `go get`, running `go mod tidy` removed Cobra because no code was importing it yet. Solved by adding the code first, then running `go mod tidy`.

2. **Global verbose flag access**: The original code stored global options in a package-level variable in `cli` package. Solved by creating a similar `GlobalOpts` struct and `GetGlobalOpts()` function in the new `cli/cobra` package.

3. **E_USAGE error handling**: The spec requires `E_USAGE` errors to print help then exit 2. Cobra's default behavior is different. Solved by setting `SilenceErrors=true` and `SilenceUsage=true` on root command, and handling errors in main.go with `errors.PrintWithOptions()`.

## Solutions Implemented

### Command Adapter Pattern

Each legacy command is wrapped in a Cobra adapter that:
1. Parses flags using Cobra's pflags
2. Gets stdout/stderr from `cmd.OutOrStdout()` / `cmd.ErrOrStderr()`
3. Creates dependencies (cr, fsys, cwd)
4. Calls the existing `commands.Xxx()` function
5. Returns errors for main.go to handle

This pattern ensures zero business logic changes while gaining Cobra's features.

### Error Handling Contract

```go
// main.go
err := cobra.Execute(os.Stdout, os.Stderr)
if err != nil {
    opts := errors.PrintOptions{Verbose: cobra.GetGlobalOpts().Verbose}
    errors.PrintWithOptions(os.Stderr, err, opts)
    os.Exit(errors.ExitCode(err))
}
```

Exit codes preserved:
- `nil` → exit 0
- `E_USAGE` → exit 2
- all other errors → exit 1

### Empty Shell Commands

New v2 commands (`worktree`, `agent`, `watch`) are registered but return `E_USAGE`:

```go
RunE: func(cmd *cobra.Command, args []string) error {
    _ = cmd.Help()
    return errors.New(errors.EUsage, "specify a subcommand: agency worktree <create|ls|...>")
}
```

This ensures they appear in help output and provide guidance when invoked.

## Decisions Made

1. **Cobra over alternatives**: Cobra is the de facto standard for Go CLIs (used by kubectl, hugo, gh). It provides completions, help, and subcommand structure out of the box.

2. **Removed dynamic completion**: The spec explicitly states dynamic completion (run names, runner names) is deferred. Cobra provides static subcommand/flag completion by default; dynamic completion will be added via `ValidArgsFunction` in later PRs.

3. **Kept completion command**: Rather than using Cobra's auto-added completion command, we register our own with the same `completion <shell>` syntax for backward compatibility and control over help text.

4. **Preserved usage text structure**: While Cobra generates different help formatting, we preserved the same flag names and descriptions. Tests now check for stable substrings (flag names, command names) rather than exact formatting.

## Deviations from Spec

1. **No `help` subcommand test**: The spec doesn't mention testing `agency help`, but Cobra auto-adds a `help` command. This is fine as it matches expected behavior.

2. **Help formatting differs**: Cobra uses its own help template. The spec notes this is expected: "Help text and usage output **will change**". Tests were rewritten to check stable substrings.

3. **Completion scripts differ**: Cobra generates more comprehensive completion scripts than the handwritten ones. The spec explicitly says completions will change.

## How to Run Commands

### Build and test

```bash
go build -o agency ./cmd/agency
go test ./...
```

### Basic usage

```bash
# Help
./agency --help
./agency run --help

# Version
./agency version
./agency --version

# Completions
./agency completion bash > completions.bash
./agency completion zsh > completions.zsh

# New v2 shells (return E_USAGE with hint)
./agency worktree
./agency agent
./agency watch
```

### Verify unchanged behavior

```bash
# JSON output unchanged
./agency ls --json | jq '.schema_version'  # "1.0"
./agency show <run_id> --json | jq '.data.meta'

# All legacy commands work
./agency init --help
./agency doctor
./agency run --name test --detached  # (requires git repo with agency.json)
```

## Commit Message

```
feat(cli): migrate to Cobra CLI framework

Replace hand-written command dispatcher with Cobra for clean subcommand
structure and auto-generated completions. This unblocks slice 8's new
worktree/agent/watch commands.

Changes:
- Add github.com/spf13/cobra v1.10.2 dependency
- Create internal/cli/cobra/ with root command and all adapters
- Register empty worktree, agent, watch shells for v2
- Replace handwritten bash/zsh scripts with Cobra generators
- Delete internal/cli/dispatch.go (legacy dispatcher)
- Delete internal/commands/completion.go (handwritten scripts)
- Rewrite CLI tests for Cobra-style output
- Update README and cli.md documentation

Preserved:
- All command semantics (same flags, same behavior)
- Exit code contract (0/1/2)
- JSON output schemas (ls --json, show --json)
- Error handling via errors.PrintWithOptions()

Breaking changes:
- Help text formatting differs (Cobra layout)
- Completion scripts differ (Cobra generators)
- Dynamic completion deferred (run names, etc.)

Part of slice 8: PR-00
```
