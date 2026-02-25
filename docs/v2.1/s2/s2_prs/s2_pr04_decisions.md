# pr-04 decisions: canonical agent read + invocation navigation convergence

Last updated: 2026-02-25
Status: draft
Related spec: `docs/v2.1/s2/s2_prs/s2_pr04.md`

## decision ledger

| id | status | question | decision | rationale | owner | date |
|---|---|---|---|---|---|---|
| D-001 | resolved | Should `agent show` adopt daemonclient rich read (`GetInvocationRich`) in PR-04 so ambiguous single-target read failures preserve daemon-provided candidate details, or stay on `GetInvocation` and defer rich error preservation to a later PR? | Adopt `GetInvocationRich` for `agent show` in PR-04 while preserving direct invocation-read endpoint ambiguity code semantics (`E_INVOCATION_ID_AMBIGUOUS`). | Closes a PR-04-owned S2 ambiguity-detail preservation gap on a canonical direct read surface without changing PR-02 navigation-kernel ambiguity normalization semantics. | user + codex | 2026-02-25 |
| D-002 | resolved | How should canonical `agent enter` determine the tmux session name when daemon read `InvocationDTO` omits `tmux_session`? | Derive the tmux session name deterministically from the daemon-resolved invocation ID via `tmux.SessionName(invocationID)` in canonical `agent enter`. | Satisfies PR-04 daemon-first/no-local-discovery requirements without expanding daemon read DTO contracts and reuses the existing canonical tmux naming function. | user + codex | 2026-02-25 |
| D-003 | resolved | How should PR-04 make canonical `agent enter` attach dispatch testable while preserving real interactive tmux attach behavior in production? | Add a narrow canonical-`agent enter` attach-dispatch seam (option/helper scoped) that defaults to `realTmuxAttach` in production. | Enables deterministic PR-04 surface dispatch assertions without regressing real interactive tmux attach semantics or replacing production attach with non-interactive `tmux.Client.Attach`. | user + codex | 2026-02-25 |
| D-004 | resolved | Should PR-04 preserve legacy local `E_INVOCATION_BROKEN` target-resolution behavior on canonical `agent` navigation surfaces (`path/open/shell/enter`), or align fully to daemon-first navigation/read semantics and remove that branch? | Align canonical `agent path/open/shell/enter` target-resolution semantics to daemon-first navigation/read behavior and remove local `E_INVOCATION_BROKEN` target-resolution branches. | Satisfies the S2 no-local-target-discovery invariant on canonical surfaces while preserving local runtime checks only when they operate on daemon-resolved data. | user + codex | 2026-02-25 |
| D-005 | resolved | How should PR-04 define sandbox-path missing behavior after daemon-first resolution for canonical `agent open` and canonical `agent shell`? | Preserve explicit local runtime `E_SANDBOX_MISSING` on both canonical `agent open` and canonical `agent shell`, implemented using daemon-resolved `sandbox_path`; keep `agent path` as pure path printing with no local existence gating. | Preserves the existing `agent open` runtime contract while keeping canonical navigation daemon-first and providing consistent runtime failure semantics across canonical path-based dispatch surfaces. | user + codex | 2026-02-25 |
| D-006 | resolved | Should PR-04 accept PR-02 kernel ambiguity normalization (`E_AMBIGUOUS`) for canonical `agent path/open/shell/enter`, or translate back to entity-specific `E_INVOCATION_ID_AMBIGUOUS` on canonical `agent` navigation surfaces? | Accept PR-02 kernel ambiguity normalization for canonical `agent path/open/shell/enter` (`E_AMBIGUOUS`) with machine-readable candidate preservation; do not add a PR-04-only translation layer. | Aligns canonical `agent` navigation with the L2 navigation contract and PR-02 shared-kernel semantics while preserving the explicit read-vs-navigation split (`agent show` remains `E_INVOCATION_ID_AMBIGUOUS`). | user + codex | 2026-02-25 |
| D-007 | resolved | How many PR-04 surface-level ambiguity regression tests are required after D-006, and does canonical `agent shell` need its own ambiguity/no-dispatch test? | Require exactly three PR-04 surface ambiguity regressions (`agent path`, `agent open`, `agent enter`) and omit a dedicated `agent shell` ambiguity/no-dispatch test unless implementation divergence is discovered. | Covers all PR-04-changed surface categories without duplicating PR-02 kernel ambiguity tests; keeps shell coverage focused on PR-04-specific shell risks already covered elsewhere. | user + codex | 2026-02-25 |

## D-001 context
- PR-04 owns canonical `agent` read + navigation convergence, including `agent ls/show` (`docs/v2.1/s2/s2_roadmap.md:100`, `docs/v2.1/s2/s2_roadmap.md:104`, `docs/v2.1/s2/s2_roadmap_ownership.md:13`).
- L2 invariant requires ambiguous target resolution to preserve candidate information when the daemon provides candidates (`docs/v2.1/s2/s2_spec.md:476`).
- L2 ambiguous-selection acceptance covers both single-target read and navigation commands (`docs/v2.1/s2/s2_spec.md:533`, `docs/v2.1/s2/s2_spec.md:535`, `docs/v2.1/s2/s2_spec.md:536`).
- `agent show` is a direct daemon read surface (`internal/commands/agent.go:452`) and currently calls `client.GetInvocation(...)` (`internal/commands/agent.go:481`).
- `daemonclient.GetInvocation` drops daemon read error `hint/details` and returns code+message only (`internal/daemonclient/client.go:825`, `internal/daemonclient/client.go:850`).
- PR-02 introduced `daemonclient.GetInvocationRich(...)` to preserve daemon read error `hint/details` for consumers that need candidate preservation (`internal/daemonclient/client.go:867`, `internal/daemonclient/client.go:870`, `internal/daemonclient/client.go:895`).
- PR-02 D-002 intentionally preserved the read-vs-navigation split:
  - navigation-kernel ambiguity normalizes to `E_AMBIGUOUS`
  - direct daemon read endpoint consumers keep entity-specific ambiguity codes unless explicitly routed through navigation semantics (`docs/v2.1/s2/s2_prs/s2_pr02.md:85`, `docs/v2.1/s2/s2_prs/s2_pr02.md:174`).
- `agent show` currently has no direct ambiguous-candidate preservation coverage in `internal/commands/agent_test.go`; existing `agent` tests are concentrated on `AgentAttach` mode/TTY/session behavior (`internal/commands/agent_test.go:211`, `internal/commands/error_codes_test.go:24`).

## gold-standard MVP recommendation (production-ready)
- Adopt `GetInvocationRich` for `agent show` in PR-04.
- Preserve direct invocation-read endpoint semantics on `agent show`:
  - keep entity-specific ambiguity code behavior (`E_INVOCATION_ID_AMBIGUOUS`)
  - do not route `agent show` through PR-02 navigation-kernel ambiguity normalization (`E_AMBIGUOUS`)
- Use the rich client path only to preserve daemon-provided `hint/details` (especially `details.candidates`) for ambiguous direct reads.
- Add explicit PR-04 tests for ambiguous `agent show` asserting:
  - top-level error code remains `E_INVOCATION_ID_AMBIGUOUS`
  - candidate details are machine-readable and present when daemon provides candidates
  - no parsing of human-readable error text is used as a substitute

## why this is the best MVP split
- Closes a real S2 contract gap on a PR-04-owned direct read surface without reopening PR-02 kernel semantics.
- Preserves the clean read-vs-navigation semantics split already established by PR-02 and adopted by PR-03:
  - direct `show` => entity-specific ambiguity code + rich details
  - navigation (`path/open/shell/enter`) => `E_AMBIGUOUS` via shared kernel
- Minimizes blast radius (`agent show` call-site change + tests) while improving correctness.
- Avoids pushing a known direct-read ambiguity-detail gap into PR-05, which owns compatibility adapters, not canonical read surfaces.

## alternatives considered (defer/reject for MVP)

### 1. Keep `agent show` on `GetInvocation` and defer candidate-preserving ambiguity details
- Reject.
- Reason: leaves a PR-04-owned S2 invariant/acceptance gap on a canonical surface.

### 2. Route `agent show` through PR-02 navigation kernel to reuse ambiguity normalization
- Reject for PR-04.
- Reason: `agent show` is a direct read surface and should preserve read endpoint semantics (`E_INVOCATION_ID_AMBIGUOUS`), not navigation semantics.

### 3. Parse candidates from daemon error message text in `agent show`
- Reject.
- Reason: brittle, non-machine-safe, and unnecessary given the merged rich daemonclient read path.

## D-002 context
- PR-04 owns canonical invocation navigation rollout under `agent`, including new canonical `agent enter` (`docs/v2.1/s2/s2_roadmap.md:100`, `docs/v2.1/s2/s2_roadmap.md:105`, `docs/v2.1/s2/s2_roadmap_ownership.md:13`).
- L2 command policy makes `agent enter` canonical and leaves `agent attach` as a v2.1 compatibility alias (`docs/v2.1/s2/s2_spec.md:432`, `docs/v2.1/s2/s2_spec.md:434`).
- L2 invariants require daemon-first navigation target resolution (no local store discovery) and TTY preflight on attach/enter flows (`docs/v2.1/s2/s2_spec.md:477`, `docs/v2.1/s2/s2_spec.md:479`).
- PR-02 kernel resolves invocation navigation to daemon-derived `resolved_id` and `resolved_path` and normalizes navigation errors, but it does not define tmux-session naming (`internal/commands/navigation_kernel.go:161`, `internal/commands/navigation_kernel.go:201`).
- Daemon read `InvocationDTO` includes `invocation_id`, `mode`, and `sandbox_path`, but does not include `tmux_session` (`internal/daemon/read_types.go:61`, `internal/daemon/read_types.go:67`, `internal/daemon/read_types.go:91`).
- Existing `AgentAttach` derives session name from local metadata (`record.Meta.TmuxSession`) and falls back to computed `tmux.SessionName(record.InvocationID)` if empty (`internal/commands/agent.go:641`, `internal/commands/agent.go:645`).
- The canonical tmux session naming function is deterministic (`tmux.SessionName(runID)` => `agency_<run_id>`) (`internal/tmux/capture.go:93`).
- Reusing local `invocation.NewService(...).Resolve(...)` to obtain `TmuxSession`/meta in canonical `agent enter` would reintroduce a forbidden local target-discovery path after daemon routing begins (`internal/commands/agent.go:608`, `docs/v2.1/s2/s2_spec.md:479`).

## gold-standard MVP recommendation (production-ready)
- Canonical `agent enter` in PR-04 should derive the tmux session name deterministically from the daemon-resolved invocation ID using `tmux.SessionName(invocationID)`.
- Use daemon-resolved invocation DTO fields for PR-04 canonical enter semantics:
  - `mode` for headed-only validation (`E_INVOCATION_INVALID_MODE`)
  - `invocation_id` for tmux session-name derivation
  - `sandbox_path` only if needed for messaging/debug output (not for target discovery)
- Do not perform local `invocation` service resolution in canonical `agent enter` to recover `tmux_session`.
- Keep `agent attach` compatibility behavior (including any local-meta nuances) out of PR-04; PR-05 owns compatibility alias rollout/policy decisions.
- If a future contract revision adds `tmux_session` to daemon read DTOs, canonical `agent enter` may prefer daemon-provided session name with deterministic fallback to `tmux.SessionName`, but that is not required for PR-04.

## why this is the best MVP split
- Satisfies PR-04’s daemon-first/no-local-discovery contract without requiring daemon DTO expansion in this PR.
- Uses an existing canonical naming function already relied on by current attach fallback behavior.
- Keeps PR-04 scoped to canonical surfaces while preserving PR-05 freedom to address compatibility alias semantics separately.
- Minimizes blast radius and implementation risk for `agent enter` while preserving deterministic attach targeting.

## alternatives considered (defer/reject for MVP)

### 1. Reuse local `invocation` service resolution in canonical `agent enter` to read `meta.json` and obtain `TmuxSession`
- Reject.
- Reason: violates the S2 no-local-target-discovery invariant on a canonical navigation surface.

### 2. Expand daemon read `InvocationDTO` to include `tmux_session` in PR-04
- Defer/reject for PR-04.
- Reason: this is a daemon read-contract change outside PR-04’s command-surface ownership (belongs with daemon contract work, not canonical CLI adoption).

### 3. Infer tmux session name from human-readable output or ad hoc string construction in `agent enter`
- Reject.
- Reason: brittle and unnecessary given the canonical `tmux.SessionName(invocationID)` helper.

## D-003 context
- PR-04 acceptance requires canonical `agent enter` to resolve via the shared daemon-first navigation contract before local dispatch (`docs/v2.1/s2/s2_roadmap.md:105`, `docs/v2.1/s2/s2_prs/s2_pr04.md:111`).
- PR-04 tests need to prove canonical `agent enter` dispatch behavior (positive dispatch on headed invocation and no-dispatch on ambiguity/invalid-mode/non-interactive cases) at the surface layer, not just kernel behavior (`docs/v2.1/s2/s2_prs/s2_pr04.md:176`, `docs/v2.1/s2/s2_prs/s2_pr04.md:191`, `docs/v2.1/s2/s2_prs/s2_pr04.md:198`, `docs/v2.1/s2/s2_prs/s2_pr04.md:212`).
- Current `AgentAttach` uses `tmuxClient.HasSession(...)` for preflight but bypasses `tmux.Client.Attach` for actual dispatch and calls `realTmuxAttach(sessionName)` directly (`internal/commands/agent.go:653`, `internal/commands/agent.go:671`, `internal/commands/agent.go:676`).
- `realTmuxAttach` is stdio-coupled (`os.Stdin`, `os.Stdout`, `os.Stderr`) and executes real `tmux attach`, which is not suitable for deterministic unit/integration tests in `agent_test.go` (`internal/commands/agent.go:678`, `internal/commands/agent.go:679`, `internal/commands/agent.go:680`).
- `tmux.ExecClient.Attach` is not a drop-in replacement for real interactive attach semantics because it runs through `exec.CommandRunner`, which captures stdout/stderr and uses non-interactive stdin (`internal/tmux/client_exec.go:72`, `internal/exec/runner.go:113`, `internal/exec/runner.go:129`).
- `testutil.FakeTmuxClient` already records `AttachCalls`, which is useful for testing attach dispatch intent if the command can route attach dispatch through an injectable seam in tests (`internal/testutil/fake_tmux.go:55`, `internal/testutil/fake_tmux.go:97`).
- Existing command option patterns support narrow test seams (`IsInteractive`, `SleepFn`) without requiring package-global mutable hooks (`internal/commands/agent.go:305`, `internal/commands/agent.go:547`).

## gold-standard MVP recommendation (production-ready)
- Add a narrow attach-dispatch seam for canonical `agent enter` only (for example an `AttachFn func(sessionName string) error` field on `AgentEnterOpts`, or an equivalent internal helper dependency).
- Default behavior in production must remain real interactive attach semantics (delegate to `realTmuxAttach(sessionName)` when no override is provided).
- Keep `tmuxClient` usage for tmux session preflight (`HasSession`) and error handling; do not switch production canonical `agent enter` dispatch to `tmux.Client.Attach`.
- In PR-04 tests:
  - use fake/injected attach function to assert positive dispatch session name (`tmux.SessionName(daemon_resolved_invocation_id)`)
  - assert attach function is not called on `E_NOT_INTERACTIVE`, `E_INVOCATION_INVALID_MODE`, or `E_AMBIGUOUS`
- Do not introduce package-global mutable attach hooks; keep the seam option-scoped or helper-scoped to preserve test isolation.

## why this is the best MVP split
- Preserves real interactive tmux attach behavior in production (no regression from `agent attach` semantics).
- Enables deterministic canonical `agent enter` dispatch assertions without invoking real tmux in tests.
- Keeps the test seam narrow and localized to PR-04’s new canonical surface.
- Avoids abusing `tmux.Client.Attach` in production where `ExecClient.Attach` does not provide real TTY attach semantics.

## alternatives considered (defer/reject for MVP)

### 1. Use `tmuxClient.Attach` for canonical `agent enter` production dispatch and tests
- Reject for PR-04 MVP.
- Reason: `ExecClient.Attach` uses non-interactive `exec.CommandRunner` semantics and does not preserve real interactive attach behavior.

### 2. Keep `realTmuxAttach` hardcoded with no seam and skip positive `agent enter` dispatch assertions
- Reject.
- Reason: PR-04 needs surface-level proof that canonical `agent enter` reaches attach dispatch on the headed happy path.

### 3. Add package-global mutable attach function variables for tests
- Reject.
- Reason: broad global hooks are brittle and risk test interference; an option-scoped seam is sufficient and cleaner.

## D-004 context
- S2 invariants forbid local store filesystem discovery for read/navigation target resolution after daemon routing begins (`docs/v2.1/s2/s2_spec.md:479`).
- L2 CLI Navigation Resolution Contract for invocation navigation names daemon/navigation error semantics (`E_DAEMON_*`, `E_NO_REPO_CONTEXT`, `E_AMBIGUOUS`, `E_INVOCATION_NOT_FOUND`) and does not include `E_INVOCATION_BROKEN` (`docs/v2.1/s2/s2_spec.md:421`, `docs/v2.1/s2/s2_spec.md:427`).
- Current canonical-adjacent `AgentOpen` returns `E_INVOCATION_BROKEN` from local post-daemon invocation resolution (`internal/commands/agent.go:1109`, `internal/commands/agent.go:1119`) and then `E_SANDBOX_MISSING` from local sandbox existence checks (`internal/commands/agent.go:1130`).
- Existing `AgentAttach` compatibility surface also returns `E_INVOCATION_BROKEN` via local meta resolution (`internal/commands/agent.go:608`, `internal/commands/agent.go:618`), but PR-05 owns compatibility alias rollout and compatibility behavior preservation.
- PR-04 canonical navigation surfaces (`agent path/open/shell/enter`) will consume PR-02 kernel daemon-first resolution, so preserving `E_INVOCATION_BROKEN` during canonical target resolution would require reintroducing local invocation discovery/meta reads.
- Daemon invocation read DTOs provide `invocation_id`, `mode`, and `sandbox_path` for canonical navigation resolution (`internal/daemon/read_types.go:61`, `internal/daemon/read_types.go:67`, `internal/daemon/read_types.go:91`), which is sufficient for path/open/shell/enter target identity/path and mode checks without local metadata discovery.

## gold-standard MVP recommendation (production-ready)
- Align canonical `agent path/open/shell/enter` target-resolution error semantics fully to daemon-first navigation/read behavior in PR-04.
- Remove the local `E_INVOCATION_BROKEN` target-resolution branch from canonical navigation surfaces.
- Preserve local post-resolution runtime checks only where they validate execution against daemon-resolved data, not target discovery:
  - `agent open` / `agent shell`: may return `E_SANDBOX_MISSING` if the daemon-resolved `sandbox_path` no longer exists at dispatch time
  - `agent enter`: may return tmux session/runtime errors (for example `E_SESSION_ENDED`) after daemon-first resolution and headed-mode validation
- Do not add a replacement local meta-validation branch solely to preserve `E_INVOCATION_BROKEN` in canonical navigation.
- Leave compatibility `agent attach` behavior unchanged in PR-04; PR-05 can intentionally decide compatibility preservation/rewiring.

## why this is the best MVP split
- Satisfies the S2 daemon-first/no-local-discovery invariant on canonical navigation surfaces.
- Keeps PR-04 aligned with PR-02 kernel semantics and the L2 navigation contract error set.
- Preserves useful local runtime safety checks (`E_SANDBOX_MISSING`, session-ended) without conflating them with target discovery.
- Keeps compatibility behavior changes out of PR-04 and inside PR-05 ownership.

## alternatives considered (defer/reject for MVP)

### 1. Preserve `E_INVOCATION_BROKEN` on canonical `agent open/shell/enter` via local post-daemon meta resolution
- Reject.
- Reason: conflicts directly with PR-04 convergence goal and S2 no-local-target-discovery invariant.

### 2. Expand daemon read endpoints/DTOs to expose a new broken-target semantic in PR-04
- Reject for PR-04.
- Reason: daemon read-contract changes are outside PR-04 command-surface ownership.

### 3. Leave canonical navigation `E_INVOCATION_BROKEN` behavior unspecified and rely on implementation choice
- Reject.
- Reason: this is script-visible behavior and needs explicit L4 policy.

## D-005 context
- D-004 resolved canonical navigation target-resolution semantics to daemon-first behavior and allowed local runtime checks only when operating on daemon-resolved data (`docs/v2.1/s2/s2_prs/s2_pr04.md:47`, `docs/v2.1/s2/s2_prs/s2_pr04.md:50`).
- Current `AgentOpen` explicitly returns `E_SANDBOX_MISSING` when local sandbox path is gone, but it derives the path from local invocation metadata (`internal/commands/agent.go:1109`, `internal/commands/agent.go:1130`).
- PR-04 canonical `agent open` will use daemon-first invocation resolution and therefore must decide whether to preserve this explicit sandbox-missing runtime semantic using the daemon-resolved `sandbox_path`, or let editor launch failures surface generically.
- Canonical `agent shell` is a new PR-04 surface with no prior behavior to preserve, so PR-04 must decide whether it should match `agent open` on missing-sandbox runtime semantics.
- L2 S2 navigation contract covers target-resolution errors (`E_AMBIGUOUS`, `E_INVOCATION_NOT_FOUND`, daemon connection/version issues) and does not exhaustively enumerate local post-resolution dispatch/runtime failures (`docs/v2.1/s2/s2_spec.md:421`, `docs/v2.1/s2/s2_spec.md:427`).
- L2 still requires authoritative `sandbox_path` from daemon invocation read data before local editor execution (`docs/v2.1/s2/s2_spec.md:508`, `docs/v2.1/s2/s2_spec.md:511`), which makes daemon-resolved path-based local runtime checks compatible with the contract.

## gold-standard MVP recommendation (production-ready)
- Preserve explicit `E_SANDBOX_MISSING` runtime semantics for canonical `agent open`, but reimplement the check using the daemon-resolved `sandbox_path` (not local invocation metadata).
- Standardize the same explicit `E_SANDBOX_MISSING` runtime behavior for canonical `agent shell` for consistency across path-based canonical dispatch surfaces.
- Keep `agent path` as a pure path-printing surface:
  - print daemon-resolved `sandbox_path`
  - do not fail solely because the path no longer exists locally at print time
- In PR-04 tests:
  - add explicit `agent open` + `agent shell` missing-sandbox runtime tests asserting `E_SANDBOX_MISSING`
  - assert details include the daemon-resolved `sandbox_path`
  - assert no local invocation target discovery is used

## why this is the best MVP split
- Preserves a useful existing `agent open` user-facing/runtime contract while removing forbidden local target discovery.
- Gives the new canonical `agent shell` a consistent, predictable runtime failure mode instead of generic shell-launch errors.
- Keeps the distinction clear:
  - daemon-first target resolution errors
  - local runtime execution errors on daemon-resolved paths
- Improves debuggability and scriptability for canonical path-based navigation surfaces.

## alternatives considered (defer/reject for MVP)

### 1. Preserve `E_SANDBOX_MISSING` on `agent open` only; let `agent shell` fail generically on missing cwd
- Defer/reject for MVP.
- Reason: creates avoidable inconsistency across canonical path-based dispatch surfaces in the same PR.

### 2. Remove explicit `E_SANDBOX_MISSING` entirely and rely on editor/shell launch failures
- Reject.
- Reason: regresses a useful existing `agent open` runtime error contract and reduces clarity.

### 3. Reintroduce local invocation meta reads to preserve exact existing `AgentOpen` missing-sandbox checks
- Reject.
- Reason: conflicts with D-004 and S2 no-local-target-discovery semantics.

## D-006 context
- PR-04 migrates canonical `agent path/open/shell/enter` to the PR-02 shared navigation kernel (`docs/v2.1/s2/s2_roadmap.md:100`, `docs/v2.1/s2/s2_prs/s2_pr04.md:40`, `docs/v2.1/s2/s2_prs/s2_pr04.md:44`).
- PR-02 kernel explicitly normalizes navigation ambiguity to `E_AMBIGUOUS` while preserving candidate details (`internal/commands/navigation_kernel.go:223`, `internal/commands/navigation_kernel.go:234`, `docs/v2.1/s2/s2_prs/s2_pr02.md:85`).
- Current canonical-adjacent `AgentOpen` uses local invocation resolution and therefore can surface entity-specific ambiguity (`E_INVOCATION_ID_AMBIGUOUS`) from `invocation.Service.Resolve(...)` (`internal/commands/agent.go:1109`, `internal/invocation/service.go:427`).
- `agent path`, `agent shell`, and canonical `agent enter` are new or rewritten canonical PR-04 navigation surfaces, so PR-04 is the first slice where their ambiguity code contract becomes script-visible.
- D-001 already fixed the direct-read surface split for `agent show`:
  - direct `agent show` ambiguity remains `E_INVOCATION_ID_AMBIGUOUS`
  - this D-006 decision is only about canonical navigation surfaces.
- L2 CLI Navigation Resolution Contract for invocation navigation names `E_AMBIGUOUS` (generic) for navigation-resolution ambiguity and does not require entity-specific ambiguity codes on navigation surfaces (`docs/v2.1/s2/s2_spec.md:421`, `docs/v2.1/s2/s2_spec.md:425`).

## gold-standard MVP recommendation (production-ready)
- Accept PR-02 kernel ambiguity normalization for canonical PR-04 navigation surfaces:
  - canonical `agent path/open/shell/enter` ambiguity returns `E_AMBIGUOUS`
  - machine-readable candidate details remain preserved per PR-02 kernel semantics
- Do not add a PR-04-only translation layer that maps kernel navigation ambiguity back to `E_INVOCATION_ID_AMBIGUOUS`.
- Keep the read-vs-navigation split explicit and stable:
  - direct `agent show` => `E_INVOCATION_ID_AMBIGUOUS` (D-001)
  - canonical navigation (`agent path/open/shell/enter`) => `E_AMBIGUOUS` (PR-02/PR-04)
- In PR-04 surface tests, assert only PR-04-owned outcomes:
  - `agent path`, `agent open`, and `agent enter` ambiguity return `E_AMBIGUOUS`
  - no dispatch on ambiguous `agent open` / `agent enter`
  - candidate details are machine-readable/preserved

## why this is the best MVP split
- Aligns canonical `agent` navigation with the L2 navigation contract and PR-02 shared-kernel semantics.
- Avoids PR-04-specific exception logic that PR-05 compatibility surfaces would have to work around or duplicate.
- Keeps the read-vs-navigation semantic split consistent across both command families (`worktree` in PR-03 and `agent` in PR-04).
- Reduces long-term drift risk by making the canonical behavior match the shared kernel directly.

## alternatives considered (defer/reject for MVP)

### 1. Translate canonical `agent` navigation ambiguity back to `E_INVOCATION_ID_AMBIGUOUS` in PR-04
- Reject for PR-04 MVP.
- Reason: introduces a PR-04-only exception to shared-kernel semantics, conflicts with L2 navigation contract direction, and creates needless cross-surface inconsistency with PR-03.

### 2. Keep ambiguity code behavior unspecified and let canonical `agent` implementation choose based on convenience
- Reject.
- Reason: ambiguity codes are script-visible and must be explicitly locked in the L4 contract.

## D-007 context
- D-006 resolved the canonical `agent` navigation ambiguity code contract to PR-02 kernel `E_AMBIGUOUS` and made the change script-visible on `agent path/open/shell/enter` (`docs/v2.1/s2/s2_prs/s2_pr04.md:101`, `docs/v2.1/s2/s2_prs/s2_pr04.md:184`, `docs/v2.1/s2/s2_prs/s2_pr04.md:190`, `docs/v2.1/s2/s2_prs/s2_pr04.md:197`).
- PR-02 already owns and tests kernel ambiguity normalization + candidate preservation semantics (`docs/v2.1/s2/s2_prs/s2_pr02.md:171`, `docs/v2.1/s2/s2_prs/s2_pr02.md:184`), so PR-04 surface tests should prove adoption and no-dispatch behavior, not duplicate kernel internals exhaustively.
- PR-04 canonical navigation includes two distinct dispatch categories plus one non-dispatch surface:
  - path printing (`agent path`)
  - path-based local process dispatch (`agent open`, `agent shell`)
  - interactive tmux attach dispatch (`agent enter`)
- PR-04 already requires shell-specific coverage for daemon-path/no-local-resolve and daemon-resolved sandbox-missing runtime semantics (`docs/v2.1/s2/s2_prs/s2_pr04.md:167`, `docs/v2.1/s2/s2_prs/s2_pr04.md:230`).
- PR-03 established the precedent of selecting representative ambiguity surface regressions instead of requiring every migrated surface to repeat the same ambiguity assertions (`docs/v2.1/s2/s2_prs/s2_pr03_decisions.md:183`).

## gold-standard MVP recommendation (production-ready)
- Require exactly three PR-04 surface-level ambiguity regression tests:
  1. `agent path` (non-dispatch surface)
  2. `agent open` (path-based dispatch surface; assert no editor dispatch on ambiguity)
  3. `agent enter` (interactive/tmux dispatch surface; assert no attach dispatch on ambiguity)
- Do not require a separate `agent shell` ambiguity/no-dispatch regression in PR-04 by default.
- Keep `agent shell` coverage focused on PR-04-specific risks already not covered by `agent open`:
  - daemon-path/no-local-resolve adoption
  - daemon-resolved sandbox-missing runtime semantics
- If implementation reveals `agent shell` has a distinct ambiguity-handling path (not shared with `agent open`), add a targeted `agent shell` ambiguity regression and revise this L4 spec.

## why this is the best MVP split
- Covers each relevant surface category changed by PR-04 (non-dispatch, path-based dispatch, interactive dispatch) without redundant ambiguity test duplication.
- Preserves PR-04 focus on surface adoption while relying on PR-02 for shared kernel ambiguity semantics.
- Keeps the test suite smaller and easier to diagnose while still protecting script-visible behavior.
- Mirrors the pragmatic test-scope pattern already used successfully in PR-03.

## alternatives considered (defer/reject for MVP)

### 1. Require ambiguity/no-dispatch regressions for all four canonical navigation surfaces (`path/open/shell/enter`)
- Defer/reject for MVP.
- Reason: low additional signal if `agent open` and `agent shell` share the same path-based dispatch ambiguity path; increases maintenance cost and test runtime.

### 2. Require only one ambiguity regression (for example `agent path`) and rely on PR-02 for everything else
- Reject.
- Reason: does not prove PR-04 surface adoption of no-dispatch behavior on editor/tmux dispatch surfaces, which is PR-04-owned behavior.
