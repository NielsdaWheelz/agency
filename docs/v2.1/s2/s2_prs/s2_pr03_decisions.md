# pr-03 decisions: worktree read + navigation convergence

Last updated: 2026-02-25
Status: draft
Related spec: `docs/v2.1/s2/s2_prs/s2_pr03.md`

## decision ledger

| id | status | question | decision | rationale | owner | date |
|---|---|---|---|---|---|---|
| D-001 | resolved | Should `worktree show` adopt daemonclient rich read (`GetWorktreeRich`) in PR-03 so ambiguous single-target read failures preserve candidate details, or stay on `GetWorktree` and defer rich error preservation to a later PR? | Adopt `GetWorktreeRich` for `worktree show` in PR-03 while preserving direct read endpoint ambiguity code semantics (`E_WORKTREE_ID_AMBIGUOUS`). | Closes a PR-03-owned S2 ambiguity-detail preservation gap on a shipped worktree read surface without changing PR-02 navigation-kernel ambiguity normalization semantics. | user + codex | 2026-02-25 |
| D-002 | resolved | How should PR-03 test `worktree open`/`worktree shell` dispatch path/cwd behavior (`os/exec`) without introducing broad production-only test seams? | Use daemon-backed executable shims + env overrides (`AGENCY_DATA_DIR`, `AGENCY_CONFIG_DIR`, `SHELL`) in `worktree_test.go`, and avoid new production launcher seams unless the shim approach proves unworkable. | Provides high-fidelity dispatch verification with low production blast radius and reuses existing env/path conventions. | user + codex | 2026-02-25 |
| D-003 | resolved | Should PR-03 preserve legacy local `E_WORKTREE_BROKEN` behavior in `worktree open/shell` by post-daemon local meta checks, or align fully to daemon-first navigation/read semantics and remove that local broken-target branch? | Align `worktree open/shell` fully to daemon-first navigation/read semantics in PR-03 and remove the post-daemon local `E_WORKTREE_BROKEN` target-resolution branch. | Preserves the S2 no-local-target-resolution invariant and keeps PR-03 navigation surfaces aligned with PR-02 kernel + L2 navigation contracts. | user + codex | 2026-02-25 |
| D-004 | resolved | Should PR-03 explicitly accept ambiguity-code normalization to `E_AMBIGUOUS` for `worktree path/open/shell` when migrating them onto the PR-02 navigation kernel, or preserve worktree-specific ambiguity codes for compatibility? | Accept PR-02 kernel ambiguity normalization to `E_AMBIGUOUS` for PR-03 `worktree path/open/shell`; preserve `E_WORKTREE_ID_AMBIGUOUS` on direct `worktree show`. | Aligns with the L2 navigation contract and preserves the PR-02 shared-kernel contract without PR-03-only ambiguity translation behavior. | user + codex | 2026-02-25 |
| D-005 | resolved | What is the minimum PR-03 surface-level ambiguity regression coverage needed to prove navigation error-code adoption (`E_AMBIGUOUS`) without overscoping duplicate tests across `worktree path/open/shell`? | Require exactly two PR-03 surface ambiguity regressions: `worktree path` (non-dispatch) and `worktree open` (dispatch/no-dispatch-on-ambiguity), while relying on PR-02 kernel tests for shared ambiguity translation internals. | Covers both PR-03 command categories with script-visible assertions without triplicating near-identical ambiguity tests. | user + codex | 2026-02-25 |

## D-001 context
- L2 acceptance explicitly covers ambiguous failures for single-target read or navigation commands, with candidate detail preservation when daemon candidates are available (`docs/v2.1/s2/s2_spec.md:533`, `docs/v2.1/s2/s2_spec.md:536`).
- L2 invariant also requires ambiguous target resolution to preserve candidate information when the daemon provides candidates (`docs/v2.1/s2/s2_spec.md:476`).
- `worktree show` is a PR-03-owned single-target read surface (`docs/v2.1/s2/s2_roadmap.md:93`) and currently uses `client.GetWorktree(...)` (`internal/commands/worktree.go:305`).
- `daemonclient.GetWorktree` collapses daemon read endpoint errors to code+message and drops `hint/details` (`internal/daemonclient/client.go:677`, `internal/daemonclient/client.go:678`), which loses daemon `details.candidates` on ambiguous worktree refs.
- PR-02 already introduced `daemonclient.GetWorktreeRich(...)` for read consumers that need daemon error `hint/details` preservation (`internal/daemonclient/client.go:697`, `internal/daemonclient/client.go:700`, `internal/daemonclient/client.go:723`).
- PR-02 D-002 intentionally preserved entity-specific ambiguity codes for direct daemon read endpoint consumers unless they explicitly opt into navigation-kernel semantics; this means PR-03 can improve candidate preservation on `worktree show` without normalizing the error code to `E_AMBIGUOUS` (`docs/v2.1/s2/s2_prs/s2_pr02.md:85`, `docs/v2.1/s2/s2_prs/s2_pr02.md:98`).

## gold-standard MVP recommendation (production-ready)
- Adopt `GetWorktreeRich` for `worktree show` in PR-03.
- Preserve direct daemon read endpoint semantics for `worktree show`:
  - keep entity-specific ambiguity code behavior (`E_WORKTREE_ID_AMBIGUOUS`) for show/read surfaces
  - do not route `worktree show` through PR-02 navigation-kernel ambiguity normalization
- Use the rich client path solely to preserve daemon-provided `hint/details` (especially `details.candidates`) so `worktree show` satisfies S2 ambiguous single-target read behavior.
- Add explicit PR-03 tests for ambiguous `worktree show` asserting:
  - ambiguous error code remains worktree-read semantics (not navigation `E_AMBIGUOUS`)
  - candidate details are machine-readable and present when daemon provides candidates
  - no message-text parsing is used as a substitute

## why this is the best MVP split
- It closes a real S2 behavior gap on a PR-03-owned surface without reopening PR-02 semantics.
- It preserves the clean separation PR-02 established:
  - navigation-kernel ambiguity => normalized `E_AMBIGUOUS`
  - direct read/show ambiguity => entity-specific code, with preserved daemon details
- It minimizes blast radius (`worktree show` call-site swap + tests) while improving contract correctness.
- It avoids deferring a known L2 acceptance gap into PR-04/PR-05, where it does not belong.

## alternatives considered (defer/reject for MVP)

### 1. Keep `worktree show` on `GetWorktree` and defer candidate-preserving ambiguity details to a later PR
- Reject.
- Reason: leaves a PR-03-owned S2 acceptance/invariant gap open on a shipped worktree surface.

### 2. Route `worktree show` through the PR-02 navigation kernel to reuse ambiguity normalization
- Reject for PR-03.
- Reason: `worktree show` is a direct read surface and should preserve read endpoint error semantics (entity-specific ambiguity code) while improving detail preservation.

### 3. Parse candidate data from the daemon error message text in `worktree show`
- Reject.
- Reason: brittle and violates the structured error transport PR-02 introduced.

## D-002 context
- `worktree open` dispatches via raw `osexec.Command(editorCmd, treePath)` and sets `cmd.Dir = treePath` (`internal/commands/worktree.go:453`, `internal/commands/worktree.go:454`).
- `worktree shell` dispatches via raw `osexec.Command(shell, "-l")` and sets `cmd.Dir = treePath` (`internal/commands/worktree.go:533`, `internal/commands/worktree.go:534`).
- `worktree` command opts for `ls/show/path/open/shell` do not currently expose `DataDirOverride`, `ConfigDirOverride`, or injected launcher callbacks (unlike some other command surfaces).
- `paths.ResolveDirs` already supports process-level environment overrides for `AGENCY_DATA_DIR` and `AGENCY_CONFIG_DIR` (`internal/paths/xdg.go:26`, `internal/paths/xdg.go:32`, `internal/paths/xdg.go:75`, `internal/paths/xdg.go:96`), and commands use `paths.ResolveDirs(osEnv{}, homeDir)` (`internal/commands/worktree.go:400`, `internal/commands/worktree.go:485`).
- `config.ResolveEditorCmd` supports editor paths relative to config dir (`internal/config/runner.go:50`, `internal/config/runner.go:53`), enabling a test-local executable shim without PATH mutation.
- `worktree shell` honors the `SHELL` environment variable with `/bin/sh` fallback (`internal/commands/worktree.go:527`, `internal/commands/worktree.go:529`), enabling a test-local shell shim.
- PR-03 needs exact assertions for launch argv/cwd and daemon-resolved path usage, but should avoid unnecessary production-surface expansion if a stable black-box test pattern is available.

## D-002 gold-standard MVP recommendation (production-ready)
- Test `worktree open` and `worktree shell` dispatch with daemon-backed executable shims plus process env overrides (`AGENCY_DATA_DIR`, `AGENCY_CONFIG_DIR`, `SHELL`) in `worktree_test.go`.
- Do not add new production option fields or broad injected launcher seams in PR-03 unless this approach proves unworkable in implementation.
- Use file-writing shim scripts to record:
  - argv
  - cwd
  - selected environment cues if needed
- Keep these dispatch tests non-parallel (or serialize the env-mutating subtests) because they rely on process-wide env overrides.
- In PR-03 tests, assert dispatch behavior and daemon-resolved path usage behaviorally; combine with code-level removal of local target resolve calls in `worktree.go` to enforce the no-post-daemon-local-resolution invariant.

## why this is the best MVP split
- Low blast radius: no new production test hooks or public option-field expansion.
- High fidelity: validates the real `os/exec` path and cwd behavior, not a test double approximation.
- Leverages existing project patterns (daemon-backed command tests, env-based dir overrides) rather than introducing a one-off seam.
- Keeps PR-03 focused on surface adoption while preserving the option to add a narrower launcher seam later if implementation evidence justifies it.

## alternatives considered (defer/reject for MVP)

### 1. Add injected launcher callbacks/fields to `WorktreeOpenOpts` and `WorktreeShellOpts` now
- Defer unless executable-shim tests prove unworkable.
- Reason: expands production command surface for testability before proving necessity.

### 2. Add package-level global `execCommand` variables for tests
- Reject for MVP.
- Reason: global mutable hooks are brittle under parallel tests and easy to misuse across command surfaces.

### 3. Skip exact dispatch-path/cwd assertions and test only high-level success
- Reject.
- Reason: too weak for PR-03’s core convergence contract (`tree_path` must be daemon-resolved before local dispatch).

## D-003 context
- S2 invariant forbids local store filesystem discovery for read/navigation target resolution after daemon routing begins (`docs/v2.1/s2/s2_spec.md:479`).
- S2 navigation contract for `worktree` path/open/shell lists daemon/navigation error semantics (`E_DAEMON_*`, `E_NO_REPO_CONTEXT`, `E_AMBIGUOUS`, `E_WORKTREE_NOT_FOUND`) and does not include `E_WORKTREE_BROKEN` (`docs/v2.1/s2/s2_spec.md:395`, `docs/v2.1/s2/s2_spec.md:421`).
- Current `worktree open` and `worktree shell` return `E_WORKTREE_BROKEN` by re-resolving local store records and inspecting `record.Broken` / `record.Meta` after daemon repo resolution (`internal/commands/worktree.go:426`, `internal/commands/worktree.go:431`, `internal/commands/worktree.go:511`, `internal/commands/worktree.go:516`).
- Daemon read handlers skip broken worktree records when listing and resolving refs (`internal/daemon/read_handlers.go:110`, `internal/daemon/read_handlers.go:578`), so daemon-first `GetWorktree`/navigation resolution treats those records as non-resolvable (not found/ambiguous among valid candidates).
- PR-03’s primary convergence requirement is to remove post-daemon local target re-resolution in `worktree open/shell`; preserving `E_WORKTREE_BROKEN` via local meta checks would retain exactly the forbidden local target-resolution branch.
- `worktree rm` retains local broken-record handling (`internal/commands/worktree.go:591`, `internal/commands/worktree.go:600`), but `worktree rm` is outside PR-03 scope and does not constrain PR-03 navigation/read semantics.

## D-003 gold-standard MVP recommendation (production-ready)
- Align `worktree open` and `worktree shell` fully to daemon-first navigation/read semantics in PR-03 and remove the post-daemon local `E_WORKTREE_BROKEN` target-resolution branch.
- For PR-03-owned worktree navigation surfaces:
  - target resolution errors come from daemon read/navigation semantics (`E_WORKTREE_NOT_FOUND`, `E_AMBIGUOUS`, daemon connection/version errors)
  - local dispatch/runtime failures remain local dispatch/runtime errors (for example editor/shell launch failures)
- Do not add a replacement local meta-validation branch solely to preserve `E_WORKTREE_BROKEN` in `open/shell`.
- Add explicit PR-03 regression tests/assertions that:
  - `open/shell` do not call local `integrationworktree` resolver after daemon routing begins
  - ambiguous/not-found errors follow daemon-first navigation semantics
  - no `E_WORKTREE_BROKEN` expectation remains on these PR-03 surfaces

## why this is the best MVP split
- It satisfies the S2 daemon-first/no-local-resolution invariant cleanly.
- It avoids a hidden compatibility backdoor where `open/shell` still depend on local metadata for target identity/path.
- It keeps PR-03 semantics aligned with PR-02 kernel behavior and L2 navigation contracts.
- It confines `E_WORKTREE_BROKEN` semantics to the command surfaces that still intentionally use local metadata (outside PR-03 scope).

## alternatives considered (defer/reject for MVP)

### 1. Preserve `E_WORKTREE_BROKEN` in `worktree open/shell` by reintroducing a local post-daemon meta check
- Reject.
- Reason: directly conflicts with the PR-03 convergence goal and S2 no-local-target-resolution invariant.

### 2. Ask daemon read endpoints to expose broken worktrees as a new navigation/read semantic in PR-03
- Reject for PR-03.
- Reason: daemon read contract changes belong to PR-01/another daemon-contract PR, not a command-surface convergence PR.

### 3. Preserve behavior by making `open/shell` special-case direct local fallback on daemon `not found`
- Reject.
- Reason: reintroduces silent local discovery and creates cross-surface drift versus PR-02/PR-04.

## D-004 context
- PR-02 navigation kernel normalizes entity-specific ambiguity errors (`E_WORKTREE_ID_AMBIGUOUS`, `E_INVOCATION_ID_AMBIGUOUS`) to generic navigation error `E_AMBIGUOUS` for navigation-resolution operations (`internal/commands/navigation_kernel.go:223`, `internal/commands/navigation_kernel.go:235`, `docs/v2.1/s2/s2_prs/s2_pr02.md:85`).
- L2 CLI Navigation Resolution Contract explicitly names `E_AMBIGUOUS` for navigation commands (`docs/v2.1/s2/s2_spec.md:425`).
- PR-03 migrates `worktree path/open/shell` onto the PR-02 navigation kernel, so those commands will inherit kernel ambiguity normalization unless PR-03 adds compatibility translation.
- Current worktree command behavior is mixed:
  - `worktree path` currently uses direct daemon `GetWorktree` (`internal/commands/worktree.go:377`) and can surface `E_WORKTREE_ID_AMBIGUOUS`
  - `worktree open/shell` currently use local `integrationworktree.Service.Resolve` (`internal/commands/worktree.go:426`, `internal/commands/worktree.go:511`), which also returns `E_WORKTREE_ID_AMBIGUOUS` on ambiguous prefixes (`internal/integrationworktree/service.go:372`)
- Preserving worktree-specific ambiguity codes on navigation surfaces after kernel migration would require PR-03 to wrap/translate PR-02 kernel errors locally, creating a surface-specific exception to shared navigation semantics.

## D-004 gold-standard MVP recommendation (production-ready)
- Accept PR-02 kernel ambiguity normalization to `E_AMBIGUOUS` for PR-03 `worktree path/open/shell` navigation surfaces.
- Do not add a PR-03 compatibility translation layer that rewrites navigation-kernel `E_AMBIGUOUS` back to `E_WORKTREE_ID_AMBIGUOUS`.
- Preserve candidate details through the kernel error path (per PR-02 semantics) so scripts/users can deterministically retry even as the top-level ambiguity code changes.
- Make the change explicit in PR-03 tests/assertions:
  - `worktree path/open/shell` ambiguity => `E_AMBIGUOUS`
  - direct `worktree show` ambiguity remains `E_WORKTREE_ID_AMBIGUOUS` (D-001)

## why this is the best MVP split
- It preserves the PR-02 shared-kernel contract without introducing PR-03-only ambiguity semantics.
- It aligns exactly with the L2 navigation contract (`E_AMBIGUOUS` for navigation).
- It avoids duplicative error translation logic that PR-04 would then have to mirror or intentionally diverge from.
- It keeps the read-vs-navigation distinction clean:
  - read/show surfaces retain entity-specific ambiguity
  - navigation surfaces use generic ambiguity

## alternatives considered (defer/reject for MVP)

### 1. Preserve `E_WORKTREE_ID_AMBIGUOUS` on `worktree path/open/shell` for compatibility by wrapping PR-02 kernel errors in PR-03
- Reject.
- Reason: creates a PR-03-only exception to the shared navigation kernel contract and increases PR-04 drift risk.

### 2. Change PR-02 kernel to support optional per-surface ambiguity-code passthrough
- Reject for PR-03.
- Reason: reopens PR-02 kernel contract and weakens the explicit L2 navigation error policy.

### 3. Leave ambiguity-code behavior unspecified in PR-03 tests
- Reject.
- Reason: ambiguity-code changes are script-visible and must be explicit at L4.

## D-005 context
- D-004 makes ambiguity-code normalization (`E_AMBIGUOUS`) on `worktree path/open/shell` a normative PR-03 behavior change.
- PR-02 already has kernel-level ambiguity translation and candidate-preservation tests (`internal/commands/navigation_kernel_test.go:706` and related ambiguity tests), so PR-03 does not need to re-prove kernel translation internals.
- PR-03 still needs surface-adoption proof that commands actually route through the kernel semantics after migration:
  - `worktree path` is the simplest non-dispatch surface to assert error code/output behavior
  - `worktree open`/`worktree shell` are dispatch surfaces with heavier harness/setup (env overrides + shims)
- Requiring all three commands to have full ambiguity regression tests may add duplicate fixture complexity with limited additional contract value, but requiring only one may leave dispatch-surface wiring unproven.

## D-005 gold-standard MVP recommendation (production-ready)
- Require two PR-03 ambiguity regression tests at the surface layer:
  - one non-dispatch navigation surface: `worktree path`
  - one dispatch navigation surface: `worktree open` (chosen because the editor shim harness is already required by D-002)
- In both tests, assert only PR-03-owned surface outcomes:
  - top-level error code is `E_AMBIGUOUS`
  - no dispatch occurs on ambiguous target for the dispatch-surface test
  - candidate details are present/machine-readable (without re-testing kernel internal translation formats beyond observable behavior)
- Do not require three near-duplicate ambiguity tests across `path/open/shell` in PR-03.
- Keep `worktree show` ambiguity read-semantics test separate (`E_WORKTREE_ID_AMBIGUOUS`) per D-001.

## why this is the best MVP split
- Covers both command categories PR-03 is changing (non-dispatch + dispatch) without overfitting duplicate tests.
- Proves surface adoption of PR-02 ambiguity semantics, not just kernel internals.
- Keeps the PR-03 test suite tractable while still protecting script-visible error-code behavior.

## alternatives considered (defer/reject for MVP)

### 1. Require ambiguity regression tests for all three surfaces (`path`, `open`, `shell`)
- Defer unless implementation history shows frequent per-command divergence after shared-kernel wiring.
- Reason: high duplicate fixture cost relative to contract coverage gain.

### 2. Require only one ambiguity regression test (for `worktree path`) and rely entirely on happy-path dispatch tests for `open/shell`
- Reject.
- Reason: leaves dispatch-surface ambiguity/no-dispatch wiring insufficiently proven.

### 3. Do not assert candidate details in PR-03 ambiguity tests because PR-02 already covers kernel behavior
- Reject.
- Reason: PR-03 still needs to prove surface-level preservation of machine-readable ambiguity details after command integration.
