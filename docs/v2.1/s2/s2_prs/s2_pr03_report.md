# pr-03 implementation report: worktree read + navigation convergence

Date: 2026-02-25
Status: complete
Upstream spec: `docs/v2.1/s2/s2_prs/s2_pr03.md`

## 1. summary of changes

### `internal/commands/worktree.go`
- **WorktreeShow**: switched from `client.GetWorktree` to `client.GetWorktreeRich` so ambiguity errors preserve daemon-provided candidate details via `DaemonReadError`.
- **WorktreePath**: replaced manual daemon setup + direct `GetWorktree` call with `setupWorktreeNav` + `ResolveNavigation` kernel using `target_kind=worktree`.
- **WorktreeOpen**: removed `integrationworktree.NewService().Resolve()` local store target resolution and the `E_WORKTREE_BROKEN` guard branch. Now resolves via `ResolveNavigation` kernel before editor dispatch.
- **WorktreeShell**: same migration as WorktreeOpen — removed local store resolution and `E_WORKTREE_BROKEN` branch, resolves via kernel before shell dispatch.
- **New helper**: `worktreeNavSetup` struct + `setupWorktreeNav()` + `buildNavDeps()` provide shared daemon client setup and navigation kernel wiring for all three navigation commands. The helper pre-ensures the daemon, then provides a no-op `EnsureDaemon` dep to the kernel (daemon already started before repo context resolution, which needs it for CWD-based repo registration).

### `internal/commands/worktree_test.go` (new)
14 daemon-backed tests covering all L3 acceptance bullets:
- 5 tests for worktree ls/show daemon-of-record read behavior (including JSON DTO shape and ambiguity candidate preservation)
- 5 tests for worktree path/open/shell daemon-first navigation (including shim-based dispatch path/cwd verification and ambiguity error code adoption)
- 4 tests for deterministic identity/output (including multi-repo JSON IDs, human output format stability, and no-E_WORKTREE_BROKEN regression)

## 2. problems encountered

| problem | resolution |
|---|---|
| macOS `/var` → `/private/var` symlink causes `pwd` output in shim to differ from `filepath.EvalSymlinks` output | Compared shim `pwd` against `env.TreePath` directly (from `os.MkdirTemp` output) instead of using `EvalSymlinks`. The shim runs with `cmd.Dir` set to the unresolved path, so `pwd` returns the same unresolved path. |
| Kernel lifecycle calls `ResolveRepo` (Phase 2) before `EnsureDaemon` (Phase 3), but `ResolveRepoViaClient` needs a running daemon for CWD-based repo registration | Pre-ensure daemon in `setupWorktreeNav` before calling `ResolveNavigation`. The kernel's `EnsureDaemon` dep becomes a no-op. This preserves the kernel's fallback boundary semantics while satisfying the repo registration requirement. |
| Unix socket path length limit on macOS (~104 bytes) | Used `os.MkdirTemp("", "wd")` with short prefix for daemon-backed test envs, consistent with existing `setupAgentTestEnvShort` pattern. |

## 3. solutions implemented

- **Shared navigation setup helper**: `worktreeNavSetup` eliminates duplication across path/open/shell while keeping the kernel consumption clean. Each command builds a `NavigationIntent` with appropriate fields and delegates resolution entirely to `ResolveNavigation`.
- **GetWorktreeRich for show**: preserves the `DaemonReadError` wrapper with `Hint` and `RawDetails` fields, allowing callers to extract candidates programmatically via `dre.Candidates()`.
- **Shim-based dispatch testing**: creates temporary executable scripts that record `pwd` and `$@` to a file. Editor dispatch uses `opts.Editor` (existing production field). Shell dispatch uses `SHELL` env var override. No new production launcher seams added.

## 4. decisions made (and why)

| decision | rationale |
|---|---|
| Pre-ensure daemon before kernel invocation | `ResolveRepoViaClient` needs a running daemon for CWD-based repo registration. Pre-ensuring is equivalent to the pre-migration flow and doesn't violate the kernel's no-local-store-discovery invariant. |
| `EnsureDaemon` dep is no-op in worktree navigation deps | Daemon is already running from `setupWorktreeNav`. The kernel's Phase 3 (daemon ensure) adds no value but must exist in the deps interface. |
| Navigation kernel `GetWorktree` dep uses `GetWorktreeRich` | Ensures the kernel's `translateNavigationError` receives full `DaemonReadError` with candidate data for ambiguity normalization. Non-rich `GetWorktree` would lose candidates before the kernel sees them. |
| All tests non-parallel | Required by `t.Setenv("AGENCY_DATA_DIR", ...)` which mutates process-global state. Spec explicitly allows this for dispatch tests; applied uniformly for consistency. |
| `opts.Editor` used for open dispatch test | Existing production field (corresponds to `--editor` flag). Not a new seam. The spec permits it alongside env overrides. |

## 5. deviations from L4/L3/L2 with justification

| deviation | justification |
|---|---|
| No changes to `navigation_kernel.go` | Spec predicted "no PR-03-owned semantic changes" and this held. No shared behavior gaps discovered. |
| README not modified | The PR changes internal routing only; no user-facing command surface changes. The spec constraint says "only touch files listed in deliverables." |
| v2.1 README artifact listing not updated | Out of PR-03 deliverable scope. S2 artifact listing is a doc maintenance item for broader S2 tracking. |

## 6. commands to run new/changed behavior

```bash
# worktree path now uses navigation kernel (same UX, daemon-first routing)
agency worktree path <ref>

# worktree open now uses navigation kernel (no local store re-resolution)
agency worktree open <ref>

# worktree shell now uses navigation kernel (no local store re-resolution)
agency worktree shell <ref>

# worktree show now preserves rich ambiguity details
agency worktree show <ambiguous-prefix>
```

## 7. commands used to verify correctness

```bash
# All worktree tests (14 tests)
go test -v -count=1 -run 'TestWorktree' ./internal/commands/

# Full commands package test suite
go test -count=1 ./internal/commands/

# Full verification (lint + race tests + e2e + build)
make verify
```

All passed: 0 lint issues, all tests green (including -race), clean build.

## 8. traceability table

| L3 acceptance item | files | tests | status |
|---|---|---|---|
| `worktree ls` and `worktree show` satisfy S2 daemon-of-record read behavior and render daemon-owned fields | `internal/commands/worktree.go` (WorktreeShow → GetWorktreeRich) | `TestWorktreeLS_DaemonOfRecord_RendersDaemonDTO`, `TestWorktreeShow_DaemonOfRecord_RendersDaemonDTO`, `TestWorktreeLS_JSONOutput_DirectDaemonDTO`, `TestWorktreeShow_JSONOutput_DirectDaemonDTO`, `TestWorktreeShow_AmbiguousPreservesCandidates` | PASS |
| `worktree path`, `worktree open`, and `worktree shell` resolve authoritative `tree_path` via daemon-first shared resolution before local dispatch | `internal/commands/worktree.go` (WorktreePath/Open/Shell → ResolveNavigation kernel), `internal/commands/worktree_test.go` | `TestWorktreePath_UsesNavigationKernelDaemonResolution`, `TestWorktreeOpen_UsesNavigationKernelDaemonPath_NoLocalResolve`, `TestWorktreeShell_UsesNavigationKernelDaemonPath_NoLocalResolve`, `TestWorktreePath_AmbiguityUsesEAmbiguous`, `TestWorktreeOpen_AmbiguityUsesEAmbiguous_NoDispatch` | PASS |
| Worktree command behavior preserves deterministic target identity/output needed for script-driven selection and follow-on commands | `internal/commands/worktree.go` (no rendering changes), `internal/commands/worktree_test.go` | `TestWorktreeLS_JSONOutput_PreservesRepoScopedIDs`, `TestWorktreePath_OutputsDaemonResolvedPath`, `TestWorktreeHumanOutput_RemainsHumanOriented_ScriptContractViaJSON`, `TestWorktreeNavigation_DoesNotReturnEWorktreeBrokenForTargetResolution` | PASS |

## 9. commit message

```
s2-pr03: converge worktree commands onto daemon-first navigation kernel

Migrate worktree path/open/shell target resolution onto the PR-02 shared
navigation kernel (ResolveNavigation) so all three commands resolve
authoritative tree_path via daemon-first reads before local dispatch.
Remove post-daemon local store target re-resolution and the
E_WORKTREE_BROKEN guard branch from worktree open and worktree shell.

Switch worktree show from GetWorktree to GetWorktreeRich so ambiguous
single-target read failures preserve daemon-provided candidate details
(DaemonReadError with hint and structured details).

Add worktree_test.go with 14 daemon-backed tests covering all L3
acceptance bullets: daemon-of-record reads, daemon-first navigation
dispatch, JSON DTO shape stability, ambiguity error code adoption
(E_AMBIGUOUS for navigation, E_WORKTREE_ID_AMBIGUOUS for direct reads),
no-E_WORKTREE_BROKEN regression, shim-based dispatch path/cwd
verification, and multi-repo scriptable identity preservation.

Internal routing change only — no user-facing command surface changes,
no new flags, no daemon API changes.

Acceptance:
- worktree ls/show render daemon-owned fields (no local re-derivation)
- worktree path/open/shell use ResolveNavigation kernel (no local store
  target discovery)
- worktree show ambiguity returns E_WORKTREE_ID_AMBIGUOUS with candidates
- worktree path/open/shell ambiguity returns E_AMBIGUOUS (kernel normalization)
- worktree open/shell no longer return E_WORKTREE_BROKEN for target resolution
- JSON output shape is daemon DTO passthrough (field names unchanged)
- all 14 tests pass, make verify clean (lint + race + e2e + build)

Refs: docs/v2.1/s2/s2_prs/s2_pr03.md
```
