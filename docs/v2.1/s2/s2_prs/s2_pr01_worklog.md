# S2 PR-01 Daemon Read API Contract Hardening - Worklog

Last updated: 2026-02-25
Status: draft

## evidence log

| Date | Source | Evidence | Relevance |
|---|---|---|---|
| 2026-02-25 | `docs/sdlc/L4-pr-spec.md:25` | L4 requires skeleton-first, acceptance-cluster micro-loop authoring, then hardening passes. | Defines authoring sequence for PR-01 spec package. |
| 2026-02-25 | `docs/sdlc/L4-pr-spec.md:42` | L4 hardening requires complete L3 acceptance coverage and dependency sanity. | Defines PR-01 completion criteria. |
| 2026-02-25 | `docs/v2.1/s2/s2_roadmap.md:64` | PR-01 goal is daemon read API contract hardening for deterministic envelopes, validation, and pagination/error behavior. | PR-01 ownership scope anchor. |
| 2026-02-25 | `docs/v2.1/s2/s2_roadmap.md:67` | PR-01 acceptance includes strict `state`/`mode` validation, stable pagination, and deterministic worktree-ref filtering. | Defines PR-01 acceptance clusters. |
| 2026-02-25 | `docs/v2.1/s2/s2_roadmap_ownership.md:10` | PR-01 owns daemon contract hardening only; no CLI command migration or alias rollout. | Prevents scope smuggling into PR-02+. |
| 2026-02-25 | `docs/v2.1/s2/s2_roadmap_ownership.md:20` | PR-01 owns daemon-side enum validation/fail-closed semantics; downstream PRs may not weaken it. | Confirms D-006 is implemented here. |
| 2026-02-25 | `docs/v2.1/s2/s2_spec.md:99` | L2 `ListQuery` requires strict rejection of unknown `state`/`mode` with `E_INVALID_ARGUMENT`. | Primary PR-01 contract change. |
| 2026-02-25 | `docs/v2.1/s2/s2_spec.md:110` | L2 defines structured invalid-argument details (`param`, `value`, `allowed_values`). | Drives details type + error assertions. |
| 2026-02-25 | `docs/v2.1/s2/s2_spec.md:236` and `docs/v2.1/s2/s2_spec.md:310` | S2 list endpoint contracts specify default/max pagination and invalid-argument error semantics. | PR-01 list endpoint hardening requirements. |
| 2026-02-25 | `docs/v2.1/s2/s2_spec.md:277` and `docs/v2.1/s2/s2_spec.md:358` | S2 show endpoint contracts define 404/409 semantics and envelope continuity. | PR-01 must preserve show endpoint error/envelope behavior. |
| 2026-02-25 | `docs/v2.1/s2/s2_spec.md:475` | Pagination defaults and max (`100`/`500`) are invariant across worktree/invocation lists. | PR-01 must preserve stability while hardening filters. |
| 2026-02-25 | `docs/v2.1/s2/s2_spec.md:482` | Invalid `state`/`mode` must fail closed and never silently broaden/default the result set. | Rejects current matcher-default behavior. |
| 2026-02-25 | `docs/v2.1/s2/s2_spec_worklog.md:103` | Current daemon list handlers coerce unknown `state`/`mode` instead of rejecting. | Concrete implementation drift to close in PR-01. |
| 2026-02-25 | `internal/daemon/read_handlers.go:71` | `/worktrees` list handler currently parses params and scans repos before filtering. | PR-01 list-handler hardening target. |
| 2026-02-25 | `internal/daemon/read_handlers.go:170` | `/invocations` list handler currently parses params, resolves `worktree_ref`, and scans invocations. | PR-01 list-handler hardening target. |
| 2026-02-25 | `internal/daemon/read_handlers.go:876` and `internal/daemon/read_handlers.go:898` | List param parsers copy raw `state`/`mode` strings without validation. | Validation placement decision point. |
| 2026-02-25 | `internal/daemon/read_handlers.go:993`, `internal/daemon/read_handlers.go:1004`, `internal/daemon/read_handlers.go:1022` | Matcher helpers default unknown filters to permissive behavior (`present` or `true`). | Root cause of silent widening bug. |
| 2026-02-25 | `internal/daemon/read_types.go:9` | `APIResponse` envelope already matches L2 read-envelope shape for PR-01 scope. | Confirms no envelope redesign needed. |
| 2026-02-25 | `internal/daemon/read_types.go:224` and `internal/daemon/read_types.go:232` | List param structs include `WorktreeRef` and an extra compatibility `WorktreeID` field. | Drove PR01-D-004 compatibility decision (`worktree_id` preserved as non-normative input). |
| 2026-02-25 | `internal/daemon/read_handlers_test.go:1195` and `internal/daemon/read_handlers_test.go:1217` | Existing tests already assert matched and unresolved `worktree_ref` filtering behavior. | PR-01 can preserve and tighten deterministic filter coverage. |

## acceptance-cluster notes

### cluster 1: daemon read envelope + endpoint error semantics alignment
- Scope chosen: preserve existing `APIResponse` envelope and show-endpoint 404/409 behavior while hardening list endpoint invalid-filter handling.
- No forced decision yet; current envelope shape already aligns for PR-01 scope.
- PR-01 spec traceability maps to `read_handlers.go`, `read_types.go`, and `read_handlers_test.go`.

### cluster 2: strict enum list-filter validation (`state` / `mode`)
- Forced decision resolved: validate at list-handler boundary before repo enumeration/filter loops.
- Forced decision resolved: invalid enum inputs return `400 E_INVALID_ARGUMENT` with structured details (`param`, `value`, `allowed_values`).
- Forced sub-decision resolved: `/invocations` validates `state` before `mode`; first invalid wins within the single-error L2 details shape.

### cluster 3: pagination defaults/max + cursor continuity stability
- Scope chosen: preserve current parser/pagination behavior for `limit`/`cursor`; add regression coverage around `limit=500` acceptance and cursor continuity.
- Boundary note: no new `limit`/`cursor` strictness contract is introduced in PR-01 (not in L2/L3 PR-01 acceptance).

### cluster 4: deterministic invocation `worktree_ref` filtering
- Existing code already preserves unresolved `worktree_ref` => empty list via sentinel filter.
- Forced boundary decision resolved: preserve undocumented `worktree_id` as compatibility-only behavior in PR-01, keep `worktree_ref` canonical, and lock `worktree_ref` precedence when both are supplied.
- PR-01 spec now includes compatibility/regression tests without expanding the S2 canonical query contract.

## hardening pass notes

1. Completeness: all four PR-01 L3 acceptance bullets are mapped in the traceability matrix, including combined-invalid-filter precedence coverage under the invalid-filter acceptance row.
2. Dependency sanity: no dependency on PR-02+ behavior is introduced in the deliverables, tests, or decisions.
3. Boundary cleanup: CLI migration/alias work remains explicitly out of scope; no PR-02+ navigation-kernel behavior is specified.
4. Ambiguity cleanup: combined-invalid-filter precedence is now explicit (`state` then `mode`) and asserted in the planned test matrix.
5. Implementation readiness: open-questions/defaults table is empty; deliverables, test names, and behavior constraints are explicit and scoped to PR-01 daemon read contract hardening.
