# S2 PR-04 Implementation Report

## 1. Summary of Changes

### Production code (`internal/commands/agent.go`)
- **D-001**: `AgentShow` migrated from `client.GetInvocation` to `client.GetInvocationRich` to preserve daemon-provided ambiguity candidate details on direct reads.
- **New shared setup**: `agentNavSetup` / `buildNavDeps` — mirrors the PR-03 `worktreeNavSetup` pattern but wires `GetInvocationRich` for invocation target resolution via the PR-02 navigation kernel.
- **New `AgentPath`**: prints daemon-resolved `sandbox_path` to stdout. Pure path-printing surface — no local existence gating.
- **New `AgentShell`**: resolves invocation via navigation kernel, then spawns `$SHELL` with cwd set to daemon-resolved `sandbox_path`. Fails with `E_SANDBOX_MISSING` if path doesn't exist.
- **New `AgentEnter`**: canonical interactive navigation. Consumes PR-02 kernel with `RequiresTTY: true`. Headed-only (rejects headless with `E_INVOCATION_INVALID_MODE`). Tmux session name derived from `tmux.SessionName(invocationID)` per D-002. Narrow testability seam via `TmuxAttachFn` per D-003.
- **Migrated `AgentOpen`**: removed local `invSvc.Resolve` and `E_INVOCATION_BROKEN` branch. Now routes through navigation kernel. Sandbox existence check uses daemon-resolved path (D-005). Added `Editor` option for test shim injection.
- **No changes to**: `AgentAttach`, `AgentStop`, `AgentKill`, `AgentLand`, `AgentDiscard`, `AgentLogs`, `AgentStart`, `AgentLS`, `AgentDiff` (all out of PR-04 scope).
- **No changes to**: `navigation_kernel.go` (PR-02 semantics preserved).

### CLI registration (`internal/cli/cobra/agent.go`)
- Added `newAgentPathCmd`, `newAgentShellCmd`, `newAgentEnterCmd` cobra subcommands.
- Registered in `newAgentCmd` alongside existing subcommands.
- Updated help text to list all canonical + compatibility subcommands.
- No `agent restart` subcommand introduced (S3 scope).

### Tests (`internal/commands/agent_test.go`)
- New `agentNavTestEnv` setup helper: creates daemon + worktree + invocation with `t.Setenv` for `AGENCY_DATA_DIR`/`AGENCY_CONFIG_DIR`.
- 21 acceptance tests covering all 4 L3 acceptance bullets.
- Uses shim-based editor/shell dispatch pattern from PR-03.
- Non-parallel tests due to env mutation (per spec constraint).

### Tests (`internal/cli/cobra/root_test.go`)
- `TestAgentCLI_RegistersCanonicalPathShellEnterSubcommands`: verifies cobra tree includes `path`, `shell`, `enter`, retains `attach`, and does NOT include `restart`.

### Docs (`docs/cli.md`)
- Added `agent path`, `agent shell`, `agent enter` sections with usage, error codes, and behavior.
- Updated agent subcommand listing.

## 2. Problems Encountered

1. **Editor shim dispatch in `AgentOpen` tests**: Initial test used `t.Setenv("EDITOR", shimPath)` but the editor wasn't being picked up. Root cause: `config.LoadUserConfig` precedence made it fragile. Fixed by adding an explicit `Editor` option to `AgentOpenOpts`, matching the PR-03 `WorktreeOpenOpts.Editor` pattern.

2. **Socket path length on macOS**: Inherited from existing test infrastructure — `setupAgentNavEnv` uses short `os.MkdirTemp` prefixes (`"an"`) to stay under the ~104-byte Unix domain socket limit.

## 3. Solutions Implemented

1. **Editor override**: Added `Editor string` field to `AgentOpenOpts` so tests can inject shim paths directly without relying on env var ordering. Production code falls through to config → env → default `"code"`.

2. **DataDirOverride for AgentEnter**: `AgentEnter` accepts `DataDirOverride` to allow tests to control the data directory without relying on env resolution timing during `setupAgentNav`.

3. **Narrow attach seam (D-003)**: `AgentEnter.TmuxAttachFn` defaults to `realTmuxAttach` in production. Tests inject a recording function. Avoids global test hooks or interface-level changes to `tmux.Client`.

## 4. Decisions Made

| ID | Decision | Rationale |
|---|---|---|
| D-001 | `AgentShow` uses `GetInvocationRich` | Preserves daemon-provided ambiguity candidates on direct reads. Direct read still returns `E_INVOCATION_ID_AMBIGUOUS` (not `E_AMBIGUOUS`). |
| D-002 | `AgentEnter` derives tmux session name from `tmux.SessionName(invocationID)` | No local store discovery needed. Matches existing `AgentAttach` fallback behavior. |
| D-003 | Narrow `TmuxAttachFn` seam on `AgentEnterOpts` | Enables deterministic test assertions without misusing `tmux.Client.Attach` or adding global hooks. |
| D-004 | No `E_INVOCATION_BROKEN` on canonical navigation surfaces | Aligns with S2 no-local-target-discovery invariant. Daemon returns `E_INVOCATION_NOT_FOUND` for missing targets. |
| D-005 | `E_SANDBOX_MISSING` on `agent open`/`agent shell` using daemon-resolved path; `agent path` does not gate on existence | Preserves useful runtime contract while keeping navigation daemon-first. |
| D-006 | Navigation ambiguity returns `E_AMBIGUOUS` (not `E_INVOCATION_ID_AMBIGUOUS`) | Aligns with PR-02 kernel normalization. `agent show` retains entity-specific code via D-001. |
| D-007 | Three ambiguity regression tests (path, open, enter); omit shell | Covers all distinct surface categories without redundancy. |

## 5. Deviations from L4/L3/L2

None. All L3 acceptance bullets are satisfied. All L4 spec decisions (D-001 through D-007) are implemented as specified.

The `Editor` field added to `AgentOpenOpts` is an additive change not in the L4 spec, but is consistent with the PR-03 `WorktreeOpenOpts.Editor` pattern. This was necessary to make the shim-based editor dispatch tests reliable.

## 6. Commands to Run New/Changed Behavior

```bash
# new: print sandbox path
agency agent path <invocation_ref>

# new: open shell in sandbox
agency agent shell <invocation_ref>

# new: attach to headed invocation (canonical)
agency agent enter <invocation_ref>

# changed: agent open now uses daemon-first resolution
agency agent open <invocation_ref>

# changed: agent show now preserves ambiguity candidates
agency agent show <ambiguous_prefix>
```

## 7. Commands Used to Verify Correctness

```bash
# targeted tests (all 22 pass)
go test -run "TestAgent(LS|Show|Path|Open|Shell|Enter|Navigation|Human)_" -count=1 -v ./internal/commands/
go test -run "TestAgentCLI_RegistersCanonicalPathShellEnterSubcommands" -count=1 -v ./internal/cli/cobra/

# full package tests
go test -count=1 ./internal/commands/ ./internal/cli/cobra/

# format + vet
gofmt -l internal/commands/agent.go internal/cli/cobra/agent.go internal/commands/agent_test.go internal/cli/cobra/root_test.go
go vet ./internal/commands/ ./internal/cli/cobra/

# full verify (lint + race + e2e + completions + build)
make verify
```

All pass. 0 lint issues. No race conditions detected.

## 8. Traceability Table

| L3 Acceptance Item | Files Changed | Tests | Status |
|---|---|---|---|
| `agent ls` / `agent show` satisfy S2 daemon-of-record read behavior + daemon-owned rendering/scriptability | `internal/commands/agent.go` (AgentShow → GetInvocationRich) | `TestAgentLS_DaemonOfRecord_RendersDaemonDTO`, `TestAgentShow_DaemonOfRecord_RendersDaemonDTO`, `TestAgentLS_JSONOutput_DirectDaemonDTO`, `TestAgentShow_JSONOutput_DirectDaemonDTO`, `TestAgentShow_AmbiguousPreservesCandidates` | PASS |
| Canonical `agent path/open/shell/enter` resolve through shared daemon-first navigation contract before local dispatch | `internal/commands/agent.go` (AgentPath, AgentOpen migration, AgentShell, AgentEnter, agentNavSetup), `internal/cli/cobra/agent.go` (registration) | `TestAgentPath_UsesNavigationKernelDaemonResolution`, `TestAgentOpen_UsesNavigationKernelDaemonPath_NoLocalResolve`, `TestAgentShell_UsesNavigationKernelDaemonPath_NoLocalResolve`, `TestAgentEnter_UsesNavigationKernelInvocationResolution_HeadedOnly`, `TestAgentOpen_AmbiguityUsesEAmbiguous_NoDispatch`, `TestAgentEnter_AmbiguityUsesEAmbiguous_NoDispatch`, `TestAgentPath_AmbiguityUsesEAmbiguous` | PASS |
| Canonical `agent` navigation aligns with S2 command-family policy + deterministic target selection | `internal/cli/cobra/agent.go`, `internal/commands/agent.go` | `TestAgentCLI_RegistersCanonicalPathShellEnterSubcommands`, `TestAgentLS_JSONOutput_PreservesRepoScopedIDs`, `TestAgentPath_OutputsDaemonResolvedPath`, `TestAgentHumanOutput_RemainsHumanOriented_ScriptContractViaJSON` | PASS |
| Canonical invocation navigation enforces mode validity + `E_INVOCATION_INVALID_MODE` | `internal/commands/agent.go` (AgentEnter) | `TestAgentEnter_HeadlessInvocation_ReturnsInvalidMode`, `TestAgentEnter_NotInteractive_ReturnsENotInteractive` | PASS |
| D-004: no E_INVOCATION_BROKEN on canonical navigation | `internal/commands/agent.go` | `TestAgentNavigation_DoesNotReturnEInvocationBrokenForTargetResolution` (4 subtests) | PASS |
| D-005: sandbox missing uses daemon-resolved path | `internal/commands/agent.go` | `TestAgentOpen_SandboxMissing_UsesDaemonResolvedPath`, `TestAgentShell_SandboxMissing_UsesDaemonResolvedPath` | PASS |

## 9. Commit Message

```
feat(s2-pr04): canonical agent read + invocation navigation convergence

Converge canonical `agency agent` read and invocation navigation surfaces
onto daemon-first reads and the PR-02 shared navigation kernel, completing
S2 PR-04 from the v2.1 slice roadmap.

Changes:

- Migrate AgentShow to GetInvocationRich for ambiguity candidate
  preservation on direct reads (D-001)
- Add shared agentNavSetup/buildNavDeps mirroring PR-03 worktree pattern,
  wiring GetInvocationRich for invocation resolution via navigation kernel
- Add canonical AgentPath: prints daemon-resolved sandbox_path to stdout
- Add canonical AgentShell: spawns login shell at daemon-resolved sandbox
- Add canonical AgentEnter: interactive tmux attach with TTY preflight,
  headed-only mode validation, session name from tmux.SessionName (D-002),
  narrow TmuxAttachFn seam for testability (D-003)
- Migrate AgentOpen from local invSvc.Resolve to navigation kernel;
  remove E_INVOCATION_BROKEN branch on canonical surfaces (D-004);
  sandbox existence check uses daemon-resolved path (D-005)
- Register agent path/shell/enter subcommands in cobra; no agent restart
  (reserved for S3)
- Add 21 acceptance tests covering all 4 L3 acceptance bullets plus
  D-004/D-005/D-006/D-007 regression coverage
- Add TestAgentCLI_RegistersCanonicalPathShellEnterSubcommands
- Update docs/cli.md with agent path/shell/enter documentation

No changes to:
- navigation_kernel.go (PR-02 semantics preserved)
- AgentAttach compatibility behavior (PR-05 scope)
- Daemon read endpoints (PR-01 scope)
- Mutation commands (stop/kill/land/discard/start)

Tested: make verify (lint 0 issues, race tests pass, e2e pass, build ok)
```
