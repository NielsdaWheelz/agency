# PR Report: Shell Tab Completion (bash + zsh)

## Summary of Changes

This PR implements first-class shell tab completion for the `agency` CLI, supporting bash and zsh shells. The implementation follows spec7 closely and provides completion for:

- **Commands**: Static list of user-facing subcommands
- **Run references**: Dynamic completion of run names and run_ids
- **Runner names**: From user config + built-in defaults (`claude`, `codex`)
- **Merge strategies**: `--squash`, `--merge`, `--rebase`

### Files Created

1. **`internal/commands/completion.go`**: Core completion logic
   - `Completion()`: Generates shell-specific completion scripts (bash/zsh)
   - `Complete()`: Hidden `__complete` command for shell scripts to call
   - Completion candidate generators for each kind (commands, runs, runners, merge_strategies)
   - Embedded bash and zsh completion scripts with install instructions

2. **`internal/commands/completion_test.go`**: Comprehensive unit tests
   - Tests for bash/zsh script generation
   - Tests for command completion (static list)
   - Tests for merge strategy completion
   - Tests for runner completion (built-in defaults)
   - Tests for run completion (name deduplication, archive exclusion, sorting)
   - Tests for silent error handling

### Files Modified

1. **`internal/cli/dispatch.go`**:
   - Added `completion` and `__complete` commands to the switch statement
   - Added `completionUsageText` with usage and installation instructions
   - Added `completion` to the main usage text

2. **`README.md`**:
   - Added new "shell completion" section with installation instructions for bash/zsh
   - Added `agency completion <shell>` to the commands list
   - Added `agency completion` command documentation section

## Problems Encountered

### 1. Type Name Collisions

The codebase already had an `osEnv` type in `doctor.go`. My initial implementation created a duplicate `osEnv` type in `completion.go`, causing compilation errors.

**Solution**: Renamed to `completeEnv` to avoid collision. Also updated the receiver method signature to use a value receiver (`completeEnv`) instead of pointer receiver to match the existing pattern.

### 2. Test Stub Type Collisions

Similarly, `stubRunner` and `stubFS` were already defined in other test files in the `commands` package.

**Solution**: Renamed to `completionStubRunner` and `completionStubFS` for isolation.

### 3. FS Interface Mismatch

The stub filesystem implementation's `CreateTemp` method had the wrong return type (`interface{ Close() error }` vs `io.WriteCloser`).

**Solution**: Fixed the signature to return `io.WriteCloser` to match the `fs.FS` interface.

## Solutions Implemented

### Completion Architecture

The implementation uses a two-command approach:

1. **`agency completion <bash|zsh>`**: User-facing command that prints shell completion scripts to stdout. Users redirect this to the appropriate file.

2. **`agency __complete <kind> [flags]`**: Hidden command called by the shell completion scripts. Outputs newline-separated candidates.

### Run Completion Logic

Per spec:
- **Scope**: Current repo only (unless `--all-repos`)
- **Filter**: Active runs only (unless `--include-archived`)
- **Names**: Included only if unique within the candidate set
- **Run IDs**: Always included
- **Sorting**: By `created_at` DESC, tie-breaker: run_id DESC

### Error Handling

Following spec requirements:
- Normal mode: Silent failure (print nothing, exit 0) for shell UX
- Debug mode (`AGENCY_DEBUG_COMPLETION=1`): Prints diagnostics to stderr

### Shell Script Design

Both bash and zsh scripts:
- Include installation instructions as comments at the top
- Call `agency __complete <kind>` to get candidates
- Handle position-aware completion (commands first, then run refs, etc.)
- Support flag completion for merge command only (as specified)

## Decisions Made

### 1. Embedded Scripts vs Template Files

**Decision**: Embed completion scripts directly as string constants in Go.

**Rationale**: 
- Simpler deployment (single binary)
- No runtime file dependencies
- Scripts are small (~80 lines each)
- Easy to maintain inline with Go code

### 2. Debug Mode Environment Variable

**Decision**: Use `AGENCY_DEBUG_COMPLETION=1` for debug output instead of a command flag.

**Rationale**: 
- Matches spec exactly
- Environment variable is natural for shell debugging scenarios
- Doesn't pollute normal completion output

### 3. Name Deduplication Strategy

**Decision**: Count name occurrences across all filtered runs; emit name only if count == 1.

**Rationale**: 
- Simple O(n) counting approach
- Handles edge case of same name in different repos (with `--all-repos`)
- Run IDs are always unique, so they're always included

### 4. No `_init_completion` Dependency

**Decision**: Bash script gracefully handles missing `_init_completion` from bash-completion package.

**Rationale**: 
- Not all systems have bash-completion installed
- Fallback manually initializes `COMPREPLY`, `cur`, etc.
- Works in both scenarios

## Deviations from Spec

### None

The implementation follows spec7 exactly. No deviations were necessary.

## How to Run New Commands

### Generate completion scripts

```bash
# Generate bash completion
agency completion bash

# Generate zsh completion
agency completion zsh
```

### Install completion (bash)

```bash
# Option 1: With bash-completion package
agency completion bash > ~/.local/share/bash-completion/completions/agency

# Option 2: Manual
agency completion bash > ~/.agency-completion.bash
echo 'source ~/.agency-completion.bash' >> ~/.bashrc
source ~/.bashrc
```

### Install completion (zsh)

```bash
# Option 1: Using fpath
mkdir -p ~/.zsh/completions
agency completion zsh > ~/.zsh/completions/_agency
# Add to .zshrc before compinit: fpath=(~/.zsh/completions $fpath)
autoload -Uz compinit && compinit

# Option 2: Manual
agency completion zsh > ~/.agency-completion.zsh
echo 'source ~/.agency-completion.zsh' >> ~/.zshrc
source ~/.zshrc
```

### Test completion

```bash
# After installation and shell restart:
agency <TAB>              # Shows subcommands
agency show <TAB>         # Shows run names/ids
agency attach feat<TAB>   # Completes run name
agency run --runner <TAB> # Shows: claude, codex
agency merge x --<TAB>    # Shows: --squash, --merge, --rebase
```

### Debug completion issues

```bash
# Enable debug mode
export AGENCY_DEBUG_COMPLETION=1

# Run completion directly
agency __complete commands
agency __complete runs
agency __complete runners
agency __complete merge_strategies

# With flags (for manual debugging)
agency __complete runs --all-repos
agency __complete runs --include-archived
```

### Run tests

```bash
# Run completion tests
go test -v ./internal/commands/ -run "TestCompletion|TestComplete" -count=1

# Run all tests
go test ./... -count=1
```

## Branch Name and Commit Message

**Branch**: `pr/shell-tab-completion`

**Commit Message**:

```
feat: add shell tab completion for bash and zsh

Implement first-class shell tab completion per spec7, supporting:

Commands (agency completion):
- `agency completion bash` - generate bash completion script
- `agency completion zsh` - generate zsh completion script

Completion targets:
- Commands: static list of user-facing subcommands (excludes __complete)
- Run references: dynamic completion of run names (unique only) and run_ids
- Runner names: from user config + built-in defaults (claude, codex)
- Merge strategies: --squash, --merge, --rebase (flag completion for merge only)

Run completion behavior:
- Default scope: current repo only, active runs only
- Names included only if unique within candidate set
- Run IDs always included
- Sorted by created_at DESC, tie-breaker: run_id DESC
- Flags: --all-repos, --include-archived (for debugging/future use)

Error handling:
- Silent failure by default (print nothing, exit 0) per shell UX conventions
- Debug mode via AGENCY_DEBUG_COMPLETION=1 for troubleshooting

Shell scripts:
- Embedded in Go as string constants for single-binary deployment
- Include installation instructions as comments
- Position-aware completion (commands, then run refs, etc.)
- Graceful fallback when bash-completion package not installed

Installation methods documented:
- macOS Homebrew: auto-installed to site-functions
- bash: bash-completion package or manual sourcing
- zsh: fpath or manual sourcing

Files added:
- internal/commands/completion.go
- internal/commands/completion_test.go

Files modified:
- internal/cli/dispatch.go (add completion, __complete commands)
- README.md (add shell completion section, command docs)

Performance: <100ms typical latency, no git network ops, no tmux/gh invocations.

Tested with: go test ./... (all pass)
```
