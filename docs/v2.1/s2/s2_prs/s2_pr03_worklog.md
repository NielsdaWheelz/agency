# pr-03 worklog: worktree read + navigation convergence

Last updated: 2026-02-25
Status: draft
Upstream l2: `docs/v2.1/s2/s2_spec.md`
Upstream l3: `docs/v2.1/s2/s2_roadmap.md` (PR-03)

## purpose
- capture code-fact evidence used to scope PR-03 L4.
- record current worktree-surface convergence drift relative to PR-03-owned cluster `C3`.
- log drafting progress, boundary checks, and unresolved PR-03 decisions.

## evidence log

| date | source | finding | relevance |
|---|---|---|---|
| 2026-02-25 | `docs/v2.1/s2/s2_roadmap.md:89` | PR-03 is the worktree command-family convergence PR. | Primary L3 PR ownership scope. |
| 2026-02-25 | `docs/v2.1/s2/s2_roadmap_ownership.md:12` | PR-03 owns worktree surface adoption only and must consume PR-02 kernel semantics. | Boundary guard against shared-kernel drift and PR-04/PR-05 scope smuggling. |
| 2026-02-25 | `docs/v2.1/s2/s2_spec.md:498` | `worktree ls/show` must be daemon-of-record reads in S2. | PR-03 primary acceptance basis (read side). |
| 2026-02-25 | `docs/v2.1/s2/s2_spec.md:503` | `worktree path/open/shell` must source authoritative `tree_path` from daemon worktree read data before local dispatch. | PR-03 primary acceptance basis (navigation/dispatch side). |
| 2026-02-25 | `docs/v2.1/s2/s2_spec.md:471` | S2 list/show reads render daemon DTO data without local store re-derivation. | PR-03 list/show rendering invariant. |
| 2026-02-25 | `docs/v2.1/s2/s2_spec.md:472` | S2 path/open/shell must obtain authoritative target path from daemon read contract before local dispatch. | PR-03 navigation path invariant. |
| 2026-02-25 | `docs/v2.1/s2/s2_spec.md:476` | Ambiguous single-target read/navigation must preserve candidate information when daemon provides candidates. | Drives PR-03 `worktree show` ambiguity-detail decision (D-001). |
| 2026-02-25 | `docs/v2.1/s2/s2_spec.md:479` | S2 CLI handlers must not scan local store filesystem for read/navigation target resolution. | PR-03 must remove local target resolve in `worktree open/shell`. |
| 2026-02-25 | `internal/commands/worktree.go:210` | `WorktreeLS` already uses daemon `ListWorktrees` and renders daemon DTOs. | PR-03 read-side baseline; mostly regression coverage + scriptability assertions. |
| 2026-02-25 | `internal/commands/worktree.go:305` | `WorktreeShow` uses daemon `GetWorktree`, but via non-rich client path. | PR-03 D-001 decision seam for ambiguity candidate preservation. |
| 2026-02-25 | `internal/commands/worktree.go:377` | `WorktreePath` already uses daemon `GetWorktree` for `tree_path`. | PR-03 needs kernel adoption without breaking current path output semantics. |
| 2026-02-25 | `internal/commands/worktree.go:426` | `WorktreeOpen` still resolves worktree target via local `integrationworktree.Service.Resolve` after daemon repo resolution. | Direct PR-03 drift to close. |
| 2026-02-25 | `internal/commands/worktree.go:511` | `WorktreeShell` has the same post-daemon local target re-resolution pattern. | Direct PR-03 drift to close. |
| 2026-02-25 | `internal/commands/navigation_kernel.go:161` | PR-02 shared kernel enforces daemon-first routing and no-local-discovery target resolution. | PR-03 must consume this seam instead of duplicating behavior. |
| 2026-02-25 | `internal/daemonclient/client.go:655` | `GetWorktree` returns code+message only on daemon errors. | Candidate detail loss risk for `worktree show` ambiguous errors. |
| 2026-02-25 | `internal/daemonclient/client.go:700` | `GetWorktreeRich` preserves daemon `hint/details` for read errors. | Candidate-preserving option for PR-03 `worktree show` error behavior. |
| 2026-02-25 | `internal/commands/agent_test.go:126` | Existing daemon-backed command tests use short temp paths to avoid Unix socket path-length issues. | Reusable test harness pattern for new `worktree_test.go`. |
| 2026-02-25 | `internal/commands/worktree.go:453` | `WorktreeOpen` dispatches via raw `osexec.Command(editorCmd, treePath)` with `cmd.Dir=treePath`. | PR-03 test seam requirement for asserting daemon-resolved path/cwd. |
| 2026-02-25 | `internal/commands/worktree.go:533` | `WorktreeShell` dispatches via raw `osexec.Command(shell, "-l")` with `cmd.Dir=treePath`. | PR-03 test seam requirement for asserting daemon-resolved path/cwd. |
| 2026-02-25 | `internal/paths/xdg.go:75`, `internal/paths/xdg.go:96` | `paths.ResolveDirs` honors `AGENCY_DATA_DIR` and `AGENCY_CONFIG_DIR` process env overrides. | Supports a daemon-backed `worktree` test harness without adding PR-03 production option overrides. |
| 2026-02-25 | `internal/config/runner.go:50`, `internal/config/runner.go:53` | `ResolveEditorCmd` supports relative editor executables resolved under config dir. | Enables test-local executable editor shim for `worktree open` dispatch assertions. |
| 2026-02-25 | `internal/commands/worktree.go:527` | `WorktreeShell` honors `SHELL` env var before `/bin/sh` fallback. | Enables test-local shell shim for `worktree shell` dispatch assertions. |
| 2026-02-25 | `docs/v2.1/s2/s2_spec.md:421`, `docs/v2.1/s2/s2_spec.md:426` | CLI navigation contract for worktree navigation includes `E_WORKTREE_NOT_FOUND` and ambiguity/daemon errors, not `E_WORKTREE_BROKEN`. | Supports D-003 recommendation to drop local broken-target semantics on PR-03 navigation surfaces. |
| 2026-02-25 | `internal/daemon/read_handlers.go:110`, `internal/daemon/read_handlers.go:578` | Daemon read list/ref resolution skips broken worktree records (`r.Broken || r.Meta == nil`). | Daemon-first resolution cannot preserve legacy local `E_WORKTREE_BROKEN` behavior for `open/shell` without reintroducing forbidden local checks. |
| 2026-02-25 | `internal/commands/navigation_kernel.go:235` | PR-02 kernel normalizes worktree/invocation ambiguity to `E_AMBIGUOUS` for navigation-resolution operations. | Creates a PR-03 ambiguity-code policy decision for `worktree path/open/shell` migration (D-004). |
| 2026-02-25 | `internal/integrationworktree/service.go:372` | Legacy local worktree resolver returns `E_WORKTREE_ID_AMBIGUOUS` on ambiguous worktree prefixes. | Confirms PR-03 kernel migration changes a user-visible ambiguity code unless explicitly translated. |
| 2026-02-25 | `internal/commands/navigation_kernel_test.go:706` | PR-02 already tests navigation ambiguity normalization to `E_AMBIGUOUS` with candidate preservation. | Supports D-005 recommendation to focus PR-03 ambiguity tests on surface adoption (wiring + no-dispatch), not duplicate kernel internals. |

## drift notes (PR-03 relevant)
- `worktree ls/show` already meet most daemon-of-record read behavior, but `worktree show` may not preserve ambiguity candidate details because it uses the non-rich daemonclient path.
- `worktree path` already uses daemon `GetWorktree` but duplicates routing steps instead of consuming the PR-02 shared kernel.
- `worktree open` and `worktree shell` still violate the S2 no-post-daemon-local-resolution invariant by re-resolving targets via local store service.
- No dedicated `worktree` command test file exists yet, so PR-03 must establish an explicit test harness and coverage surface.

## open decisions encountered
- None.

## hardening pass status
- skeleton pass: complete (L4 sections created in `s2_pr03.md`).
- acceptance-cluster micro-loop: complete (D-001 through D-005 resolved).
- boundary cleanup: complete (PR-03 scope and PR-02/PR-04/PR-05 boundaries rechecked after D-005).
- traceability completeness: complete for all PR-03 L3 bullets, including explicit ambiguity surface-adoption coverage (`worktree path` + `worktree open`).
- open-questions/defaults freeze check: complete (no remaining temporary defaults).

## decisions resolved during drafting
- D-001 resolved: PR-03 will adopt `daemonclient.GetWorktreeRich` for `worktree show` so ambiguous single-target read failures preserve daemon-provided candidate details while retaining direct read endpoint ambiguity code semantics (`E_WORKTREE_ID_AMBIGUOUS`).
- D-002 resolved: PR-03 dispatch-path/cwd assertions for `worktree open/shell` will use daemon-backed executable shims with process env overrides (`AGENCY_DATA_DIR`, `AGENCY_CONFIG_DIR`, `SHELL`) rather than adding broad production launcher seams unless implementation proves that approach unworkable.
- D-003 resolved: PR-03 will remove the post-daemon local `E_WORKTREE_BROKEN` target-resolution branch from `worktree open/shell` and align those surfaces to daemon-first navigation/read semantics for target-resolution errors.
- D-004 resolved: PR-03 will accept PR-02 kernel ambiguity normalization (`E_AMBIGUOUS`) for `worktree path/open/shell`, while keeping direct `worktree show` ambiguity under read semantics (`E_WORKTREE_ID_AMBIGUOUS`).
- D-005 resolved: PR-03 will require exactly two surface-level ambiguity regression tests (`worktree path` and `worktree open`) to prove PR-02 ambiguity semantics are adopted on both non-dispatch and dispatch worktree navigation surfaces without duplicating kernel internals already covered by PR-02 tests.
