# pr-02 decisions: shared cli navigation resolution kernel

Last updated: 2026-02-25
Status: draft
Related spec: `docs/v2.1/s2/s2_prs/s2_pr02.md`

## decision ledger

| id | status | question | decision | rationale | owner | date |
|---|---|---|---|---|---|---|
| D-001 | resolved | How should PR-02 preserve daemon read error `hint` and structured `details` (especially ambiguity `details.candidates`) through `daemonclient` into the shared navigation kernel? | Introduce a narrow, typed daemonclient read-API error passthrough path for PR-02 kernel consumers that preserves `error_code`, `message`, `hint`, and raw structured `details`, while keeping existing daemonclient call sites backward-compatible. | Satisfies PR-02 ambiguity-candidate preservation with low blast radius and avoids a broad CLI error-system refactor in the same PR. | user + codex | 2026-02-25 |
| D-002 | resolved | Should PR-02 navigation-kernel ambiguity failures normalize daemon/entity-specific ambiguity codes to `E_AMBIGUOUS`, or preserve entity-specific ambiguity codes? | Normalize ambiguity failures to `E_AMBIGUOUS` for navigation-resolution operations only; preserve entity-specific ambiguity codes for direct daemon read endpoint consumers unless they explicitly opt into navigation-kernel semantics. | Aligns PR-02 kernel behavior with the L2 CLI Navigation Resolution Contract while preserving daemon read/show endpoint semantics and avoiding downstream PR drift. | user + codex | 2026-02-25 |
| D-003 | resolved | Should PR-02 define a new opaque `machine_ref` token grammar for script-driven/fleet-safe selection, or standardize structured repo-scoped selector inputs and daemon DTO IDs without introducing a new token in S2? | Standardize structured repo-scoped selector inputs + daemon DTO IDs at the kernel boundary and defer any opaque machine token grammar to a later, explicitly scoped contract change. | Preserves fleet-safe determinism via explicit `repo_id`, avoids coupling automation to human output, and keeps PR-02 kernel command-family agnostic. | user + codex | 2026-02-25 |
| D-004 | resolved | Should PR-02 shared TTY preflight require a kernel-owned generic recovery hint string, or only guarantee `E_NOT_INTERACTIVE` while leaving hint text fully command-surface-owned? | Require PR-02 shared TTY preflight to return `E_NOT_INTERACTIVE` with a non-empty generic recovery hint from the kernel, while allowing downstream command surfaces to override/append wording. | Satisfies L2 acceptance at the shared-kernel layer and avoids copy-coupling PR-02 to command-family UX text. | user + codex | 2026-02-25 |
| D-005 | resolved | Should PR-02 encode the bootstrap-only fallback boundary as an explicit kernel policy/input, or leave fallback-boundary enforcement to downstream command surfaces? | Encode bootstrap-only fallback as an explicit kernel policy/input (fallback-disabled by default for normal navigation/single-target read intents) and require an explicit fallback callback when boundary fallback is enabled. | Mirrors the L2 `ReadSurface.bootstrap_fallback_allowed` contract and prevents downstream drift by making fallback eligibility visible and testable at the shared seam. | user + codex | 2026-02-25 |
| D-006 | resolved | Should PR-02 include a kernel-level positive fallback-boundary test path now, or defer positive boundary coverage to a later bootstrap/health command migration PR? | Include one kernel-level positive fallback-boundary test in PR-02 now using a synthetic boundary-eligible routing intent/policy plus injected fallback callback (no bootstrap/health command migration). | D-005 made fallback policy explicit; positive guarded-callback coverage is required in PR-02 to fully validate the kernel seam and avoid false confidence from negative-only no-fallback tests. | user + codex | 2026-02-25 |

## D-001 context
- daemon read endpoints already emit structured error details (`internal/daemon/read_types.go:21`, `internal/daemon/read_types.go:24`).
- CLI read client methods currently collapse daemon API errors to code+message only (`internal/daemonclient/client.go:624`, `internal/daemonclient/client.go:750`).
- PR-02 L2/L3 acceptance requires ambiguity failures to preserve candidate data for deterministic retry (`docs/v2.1/s2/s2_spec.md:533`, `docs/v2.1/s2/s2_roadmap.md:81`).
- existing CLI `AgencyError` details shape is `map[string]string` (`internal/errors/errors.go:220`), which is not a direct fit for daemon candidate arrays.

## gold-standard MVP recommendation (production-ready)
- Introduce a narrow, typed daemonclient read-API error passthrough path for PR-02 kernel consumers.
- Preserve at least:
  - `error_code`
  - `message`
  - `hint`
  - raw structured `details` (typed field or `json.RawMessage`/`map[string]any`)
- Keep existing `daemonclient` methods backward-compatible for current callers:
  - either add parallel helper(s) used only by the PR-02 kernel, or
  - add opt-in behavior without changing existing call-site return contracts.
- In the PR-02 kernel, translate that passthrough error into CLI-level ambiguity/daemon failures while preserving candidate data in a deterministic machine-readable form.
- Avoid broadening `internal/errors.AgencyError` in PR-02 unless unavoidable:
  - a repo-wide error-shape refactor is high blast radius and not required to land the shared kernel MVP.
- Explicitly prohibit message-text parsing as the primary ambiguity transport mechanism:
  - it is brittle, localization/format dependent, and not enterprise-grade for automation contracts.

## alternatives considered (defer/reject for MVP)

### 1. Parse candidates from daemon error message text
- Reject for primary path.
- Reason: brittle and violates contract-driven error handling.

### 2. Widen `AgencyError.Details` from `map[string]string` to generic JSON in PR-02
- Defer unless D-001 cannot be solved with daemonclient passthrough + kernel-local translation.
- Reason: high blast radius across CLI formatting and many tests; not required for MVP kernel contract if passthrough stays local.

### 3. Drop candidate preservation until PR-03/PR-04
- Reject.
- Reason: violates PR-02 ownership and blocks downstream command-family specs from consuming a complete shared ambiguity contract.

## D-002 context
- L2 CLI Navigation Resolution Contract (PR-02-owned) names generic `E_AMBIGUOUS` for ambiguous repo selection or target selection (`docs/v2.1/s2/s2_spec.md:425`).
- daemon read endpoint contracts and current daemonclient methods expose entity-specific ambiguity codes for show/read endpoints (`E_WORKTREE_ID_AMBIGUOUS`, `E_INVOCATION_ID_AMBIGUOUS`) (`docs/v2.1/s2/s2_spec.md:308`, `docs/v2.1/s2/s2_spec.md:393`).
- PR-02 kernel is shared across navigation flows and may also be consumed by downstream single-target read command routing paths, so ambiguity normalization must be explicit to avoid PR-03/PR-04 drift.

## D-002 gold-standard MVP recommendation (production-ready)
- Normalize ambiguity failures inside the PR-02 navigation kernel to `E_AMBIGUOUS` for navigation-resolution operations.
- Preserve candidate data (and target kind/repo context as available) in deterministic machine-readable details for retry/disambiguation.
- Keep entity-specific ambiguity codes for direct daemon read endpoint consumers (for example show/read flows) unless a command explicitly opts into the navigation-kernel ambiguity contract.
- In practice for PR-02:
  - navigation-resolution helpers return generic kernel ambiguity semantics (`E_AMBIGUOUS`)
  - kernel routing helpers used only for repo/daemon setup must not rewrite errors from downstream command-specific daemon read calls unless explicitly in navigation-resolution mode
- Why this is the best MVP split:
  - aligns with the L2 PR-02 contract (`CLI Navigation Resolution Contract`)
  - preserves daemon/read endpoint semantics for PR-03/PR-04 show commands
  - avoids cross-surface ambiguity behavior drift
  - keeps future extensions (typed ambiguity categories) compatible without breaking scripts

## D-003 context
- L2 `NavigationSelection` includes `selector_source=machine_ref|list_row|explicit_ref`, but the slice spec does not define an opaque machine token grammar (`docs/v2.1/s2/s2_spec.md:120`).
- L3 PR-02 owns the `select` acceptance via deterministic list-row/script-driven selection inputs, without adding a dedicated `select` verb (`docs/v2.1/s2/s2_roadmap.md:84`).
- L2 IDs are repo-scoped, not globally scoped:
  - `worktree_id` unique within repo (`docs/v2.1/s2/s2_spec.md:63`)
  - `invocation_id` unique within repo (`docs/v2.1/s2/s2_spec.md:79`)
- Current JSON list outputs preserve daemon DTO fields (including `repo_id`) (`internal/commands/worktree.go:239`, `internal/commands/agent.go:390`).
- Current human list rows omit `repo_id`:
  - `worktree ls` human rows print `worktree_id name branch` only (`internal/commands/worktree.go:258`)
  - `agent ls` human rows print `invocation_id runner mode display_status...` only (`internal/commands/agent.go:424`)
- PR-02 must define whether machine/script selection relies on a new opaque token grammar or on structured repo-scoped selector inputs (for example `repo_id` + ID/ref) consumed by the kernel.

## D-003 gold-standard MVP recommendation (production-ready)
- Do not introduce a new opaque `machine_ref` token grammar in PR-02.
- Define the PR-02 machine-selection contract as structured repo-scoped selector input at the kernel boundary:
  - target kind
  - selector source (`machine_ref` / `list_row` / `explicit_ref`)
  - selector ref string
  - repo scope
  - explicit `repo_id` when the source is machine/list-row and fleet/global ambiguity must be eliminated
- Treat JSON list outputs (daemon DTOs with `repo_id` + entity IDs) as the canonical script-safe source for machine selection in S2.
- Treat human list rows as human-oriented only; PR-03/PR-04 may improve row affordances, but PR-02 should not depend on parsing human output.
- For `list_row` source in PR-02, define an in-memory identity payload contract (repo ID + target ID + target kind) that downstream command surfaces pass to the kernel, without requiring a user-visible token.
- Reserve opaque token grammar design for a later, explicitly scoped contract change if needed (versioned and mapped to the same kernel structured input).

## D-004 context
- L2 acceptance scenario states interactive navigation fails before dispatch with a TTY-required error and a recovery hint (`docs/v2.1/s2/s2_spec.md:543`, `docs/v2.1/s2/s2_spec.md:546`).
- PR-02 owns shared TTY preflight semantics (`docs/v2.1/s2/s2_roadmap.md:83`, `docs/v2.1/s2/s2_roadmap_ownership.md:11`).
- Existing `agent attach` has command-specific hint wording embedded in command logic (`internal/commands/agent.go:567`), but PR-02 must centralize preflight semantics without prematurely locking all command-family UX copy.
- Before D-004 resolution, PR-02 acceptance tests only noted hint presence and left hint exactness ambiguous (`docs/v2.1/s2/s2_prs/s2_pr02.md:151`).

## D-004 gold-standard MVP recommendation (production-ready)
- Require PR-02 shared TTY preflight to return `E_NOT_INTERACTIVE` with a non-empty generic recovery hint from the kernel.
- Define the kernel hint contract at the semantic level (for example: "run in an interactive terminal" and optionally a non-interactive alternative), not command-family-specific wording.
- Allow downstream PR-04/PR-05 command surfaces to override or append command-specific hints while preserving the kernel default when no override is provided.
- In PR-02 tests:
  - assert `E_NOT_INTERACTIVE`
  - assert failure occurs before dispatch
  - assert a non-empty hint containing an interactive-terminal recovery cue
  - do not assert exact user-facing wording owned by downstream command surfaces
- Why this is the best MVP split:
  - satisfies L2 acceptance ("recovery hint") at the shared-kernel layer
  - avoids copy-coupling PR-02 to command-family UX text
  - ensures a consistent baseline behavior across future kernel consumers

## D-005 context
- PR-02 owns the CLI Read Routing Lifecycle, including the `bootstrap_fallback` state and its guard (`docs/v2.1/s2/s2_spec.md:169`, `docs/v2.1/s2/s2_spec.md:197`).
- The L2 `ReadSurface` model includes `bootstrap_fallback_allowed` as an explicit surface property (`docs/v2.1/s2/s2_spec.md:37`).
- Current code has no shared routing kernel; daemon ensure/connect behavior is repeated in command handlers (`internal/commands/worktree.go:359`, `internal/commands/agent.go:590`).
- If PR-02 leaves fallback-boundary enforcement implicit in downstream command code, PR-03/PR-04/PR-05 can diverge while still "using" a shared resolver.
- At the D-005 draft checkpoint, PR-02 acceptance already tested the negative path (no local fallback outside boundary), but the kernel API shape for expressing boundary-allowed mode was not yet specified (`docs/v2.1/s2/s2_prs/s2_pr02.md:97`, `docs/v2.1/s2/s2_prs/s2_pr02.md:124`).

## D-005 gold-standard MVP recommendation (production-ready)
- Encode bootstrap-only fallback boundary as an explicit kernel policy/input.
- Add a routing policy field (bool or enum) to the shared kernel request/options, owned by PR-02, that makes fallback eligibility explicit (for example `bootstrap_fallback_allowed` / `routing_mode`).
- Default/expected mode for PR-02 navigation and single-target read consumers is fallback-disabled for local discovery after daemon routing begins.
- If a caller enables boundary fallback, require an explicit fallback callback/handler to be supplied; the kernel should not silently infer local fallback behavior.
- In PR-02 tests:
  - keep negative assertions (no local fallback for normal navigation/read intents)
  - add one kernel-level positive boundary-mode test that exercises the guarded fallback callback path without migrating health/bootstrap command surfaces
- Why this is the best MVP split:
  - directly reflects the L2 `ReadSurface.bootstrap_fallback_allowed` contract
  - prevents downstream PR drift by making policy visible at the shared seam
  - preserves PR-02 scope (kernel semantics) without forcing PR-03/PR-04 surface rollout

## D-006 context
- PR-02 L3 acceptance explicitly names bootstrap-only fallback boundaries as part of the routing lifecycle (`docs/v2.1/s2/s2_roadmap.md:80`).
- At the D-006 draft checkpoint, D-005 had made fallback eligibility an explicit kernel policy/input, but the draft still had strong negative-path tests (`no fallback outside boundary`) plus a not-yet-finalized positive-path test row (`docs/v2.1/s2/s2_prs/s2_pr02.md:97`, `docs/v2.1/s2/s2_prs/s2_pr02.md:130`).
- If PR-02 defers positive boundary coverage entirely, downstream PRs can accidentally break the guarded fallback callback path while still passing normal-intent no-fallback tests.
- PR-02 should avoid pulling actual health/bootstrap command migrations forward; the question is whether a kernel-level synthetic positive test is enough (recommended) or deferral is acceptable.

## D-006 gold-standard MVP recommendation (production-ready)
- Include one kernel-level positive fallback-boundary test in PR-02 now.
- Use a synthetic boundary-eligible routing intent/policy plus an injected fallback callback in the kernel test harness:
  - no bootstrap/health command migration required
  - no command-surface rollout required
- Test should prove all four properties:
  - fallback callback is invoked only when explicit boundary policy is enabled
  - fallback callback is not invoked for normal navigation/read intents
  - kernel does not infer fallback when callback is absent
  - lifecycle ordering remains repo resolution -> daemon attempt -> guarded fallback -> render/dispatch handoff
- Why this is the best MVP split:
  - fully validates the D-005 policy seam inside the PR-02-owned kernel
  - avoids phantom confidence from negative-only tests
  - preserves PR-02 scope while protecting downstream PRs from regression
