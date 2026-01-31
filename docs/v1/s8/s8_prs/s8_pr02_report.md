# Slice 8 PR-02 Report: Sandbox Creation + Invocation Records

## Summary

PR-02 introduces **per-invocation sandbox worktrees** and **canonical invocation records**, establishing the physical isolation model for agent execution. This PR creates the foundation for the slice 8 concurrency model where multiple agents can work on the same integration branch without interference.

### Key Deliverables

1. **Store Layer Extensions**
   - Added invocation paths: `InvocationsDir`, `InvocationDir`, `InvocationMetaPath`, `InvocationEventsPath`
   - Added sandbox paths: `SandboxesDir`, `SandboxDir`, `SandboxTreePath`, `SandboxLogsDir`, `SandboxCheckpointsPath`
   - CRUD operations: `EnsureInvocationDir`, `EnsureSandboxDir`, `WriteInvocationMeta`, `UpdateInvocationMeta`, `ReadInvocationMeta`, `RemoveInvocationDir`, `RemoveSandboxDir`

2. **Invocation Metadata**
   - `InvocationMeta` struct with all fields per spec (schema_version, invocation_id, sandbox_path, sandbox_branch, base_commit, runner, mode, status, etc.)
   - Status types: `starting`, `running`, `finished`, `failed`
   - Landing status: `pending`, `landed`, `discarded`

3. **Invocation Discovery & Resolution**
   - `ScanInvocationsForRepo`, `ScanInvocationsForWorktree`, `ScanAllInvocations`
   - `ResolveInvocationRef` - resolves by ID or unique prefix (names NOT used for resolution per spec)
   - Proper handling of broken invocations

4. **Invocation Service**
   - `Create` - creates sandbox worktree + invocation record atomically
   - Safety checks: INTEGRATION_MARKER enforcement, sandbox path validation
   - Robust cleanup on partial failure

5. **CLI Commands**
   - `agency agent start --worktree <ref>` - creates sandbox (no runner execution yet)
   - `agency agent ls` - lists invocations
   - `agency agent show` - shows invocation details

6. **Invariant Enforcement Tests**
   - `TestSandboxNeverResolvesToIntegrationTree`
   - `TestIntegrationMarkerEnforcement`
   - `TestSandboxMarkerWritten`
   - `TestCleanupOnPartialFailure`
   - `TestMultipleSandboxesPerWorktree`
   - `TestIntegrationTreeUntouched`

## Problems Encountered

### 1. Path Safety Validation
**Problem**: Ensuring sandbox paths never resolve to or overlap with integration tree paths required careful implementation.

**Solution**: Implemented `validateSandboxPath` with multiple checks:
- Path equality check
- Parent/child relationship checks
- Pre-existing INTEGRATION_MARKER check

### 2. Atomic Creation with Cleanup
**Problem**: If any step after `git worktree add` fails, we need to clean up the partially created state.

**Solution**: Implemented comprehensive cleanup function that runs in reverse order:
1. Remove git worktree
2. Delete sandbox branch
3. Remove sandbox directory
4. Remove invocation directory

All cleanup steps are best-effort to avoid masking the original error.

### 3. Git Default Branch Name
**Problem**: Tests failed because git uses "master" as default on some systems.

**Solution**: Updated test helper to explicitly create repos with "main" branch using `git init -b main`.

### 4. Dual Marker Protection
**Problem**: Need to distinguish integration trees from sandboxes to prevent accidental runner execution in wrong location.

**Solution**: 
- Integration trees have `INTEGRATION_MARKER` (prevents runner execution)
- Sandboxes have `SANDBOX_MARKER` (allows runner execution)
- Service checks both markers during creation

## Solutions Implemented

### Store Layer Pattern
Followed the existing integration worktree patterns exactly:
- Path helpers on Store struct
- `EnsureXxxDir` with exclusive `os.Mkdir`
- `WriteXxxMeta` with atomic JSON write
- `ReadXxxMeta` with proper error wrapping

### Resolution Pattern
Mirrored the worktree resolver but with key differences:
- Invocation names are NOT used for resolution (per spec - names are display-only)
- Only ID and prefix matching supported
- Includes `IncludeFinished` option for filtering landed/discarded

### Service Layer Pattern
Followed `integrationworktree.Service` structure:
- Constructor with all dependencies (`Store`, `CR`, `FS`, `Now`)
- `Create` and `Resolve` methods
- Cleanup functions for error recovery

## Decisions Made

### 1. Invocation Name Semantics
Per spec: `invocation_name` is optional, not used for identity, uniqueness NOT enforced. This differs from worktree names which ARE used for resolution.

### 2. No Scaffold Directories
Per spec restraint N2: We don't create future directories (logs/, checkpoints/) during sandbox creation. They will be created by their respective features (PR-04, PR-06).

### 3. Status Initialized to "starting"
All new invocations start with `status = "starting"`. Runner execution (PR-03/04) will update to `"running"`.

### 4. No Auto-Repair
Per spec restraint N3: Broken invocations are surfaced but not auto-repaired. They can be viewed with `--all` and resolved by exact ID.

## Deviations from Spec

### Minor Deviations
1. **Directory naming**: Used `integration_worktrees/` directory name from PR-01 instead of `worktrees/` mentioned in some spec sections. This follows the PR-01 established convention.

2. **Output format**: Added helpful "Note:" message about runner execution not being implemented yet, to set user expectations.

### No Major Deviations
The implementation follows the PR-02 spec closely, including all mandatory tests and invariants.

## How to Run Commands

### Create a Sandbox

```bash
# First, create an integration worktree (if not already done)
agency worktree create --name my-feature

# Create an agent invocation (sandbox)
agency agent start --worktree my-feature

# With optional name
agency agent start --worktree my-feature --name arch-agent

# Specify runner and mode
agency agent start --worktree my-feature --runner codex --headless
```

### List Invocations

```bash
# List active invocations for current repo
agency agent ls

# Filter by worktree
agency agent ls --worktree my-feature

# Include finished (landed/discarded)
agency agent ls --all

# JSON output
agency agent ls --json
```

### Show Invocation Details

```bash
# By full ID
agency agent show 20260131120500-b7c9

# By prefix
agency agent show 20260131

# JSON output
agency agent show 20260131 --json
```

## How to Test

### Run All Tests

```bash
go test ./...
```

### Run Invocation-Specific Tests

```bash
# Run all invocation tests
go test -v ./internal/invocation/...

# Run specific invariant tests
go test -v ./internal/invocation/... -run TestSandboxNeverResolvesToIntegrationTree
go test -v ./internal/invocation/... -run TestIntegrationMarkerEnforcement
go test -v ./internal/invocation/... -run TestSandboxMarkerWritten
go test -v ./internal/invocation/... -run TestCleanupOnPartialFailure
```

### Manual Testing

```bash
# Build
go build -o agency ./cmd/agency

# Initialize a test repo
mkdir /tmp/test-repo && cd /tmp/test-repo
git init -b main
echo "test" > test.txt && git add . && git commit -m "init"
./agency init

# Create integration worktree
./agency worktree create --name test-feature

# Create sandbox (agent start)
./agency agent start --worktree test-feature

# List invocations
./agency agent ls

# Show invocation
./agency agent show <invocation_id_prefix>

# Verify sandbox structure
ls -la $(./agency agent show <prefix> --json | jq -r '.sandbox_path')
```

## Files Changed

### New Files
- `internal/store/invocation.go` - InvocationMeta struct and CRUD
- `internal/store/invocation_scan.go` - Discovery functions
- `internal/ids/invocation_resolve.go` - ID/prefix resolution
- `internal/invocation/service.go` - Create and Resolve logic
- `internal/invocation/service_test.go` - Invariant tests
- `internal/commands/agent.go` - Command implementations
- `docs/v1/s8/s8_prs/s8_pr02_report.md` - This report

### Modified Files
- `internal/errors/errors.go` - Added invocation error codes
- `internal/store/store.go` - Added invocation/sandbox path helpers
- `internal/cli/cobra/agent.go` - Added start/ls/show subcommands
- `README.md` - Added agent invocation section
- `docs/cli.md` - Added agent command documentation

## Commit Message

```
feat(s8-pr02): add sandbox creation and invocation records

Implement PR-02 of slice 8: per-invocation sandbox worktrees and
canonical invocation records. This establishes the physical isolation
model where multiple agents can work on the same integration branch
without interference.

Key changes:
- Add invocation/sandbox store paths and CRUD operations
- Add InvocationMeta struct with full schema per spec
- Add invocation discovery (ScanInvocationsForRepo, etc.)
- Add invocation resolution by ID/prefix (names NOT used for resolution)
- Add invocation service with atomic sandbox creation
- Add CLI commands: agent start, ls, show
- Add comprehensive invariant enforcement tests

Sandbox creation process:
1. Verify target is integration worktree (INTEGRATION_MARKER)
2. Generate invocation_id
3. Validate sandbox path safety (never resolves to integration)
4. Create invocation directory (exclusive)
5. Create sandbox directory
6. Capture base_commit
7. Create sandbox worktree + branch via git worktree add
8. Write SANDBOX_MARKER
9. Write invocation meta.json

Invariants enforced (with tests):
- Sandbox path never equals/overlaps integration path
- INTEGRATION_MARKER required on target worktree
- SANDBOX_MARKER written in sandbox
- Cleanup on partial failure (no orphaned state)
- Integration tree never modified by sandbox creation

Note: Runner execution is NOT implemented in this PR. The sandbox is
created but no runner is started. PR-03 (headed) and PR-04 (headless)
will add runner execution.

Storage layout:
- invocations/<id>/meta.json - canonical record
- sandboxes/<id>/tree/ - sandbox worktree (runner CWD)
```
