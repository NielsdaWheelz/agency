# S1 PR-02 Gate Item Lifecycle + Closure Policy - Worklog

Last updated: 2026-02-17
Status: draft

## evidence log

| Date | Source | Evidence | Relevance |
|---|---|---|---|
| 2026-02-16 | `docs/sdlc/README.md:56` | Dispatch logic says run L4 when next PR has no L4 spec. | Confirms current layer and sequencing. |
| 2026-02-16 | `docs/sdlc/L4-pr-spec.md:33` | L4 must process each acceptance bullet in a micro-loop. | Drives cluster-by-cluster PR-02 drafting. |
| 2026-02-16 | `docs/sdlc/L4-pr-spec.md:42` | L4 hardening requires completeness, dependency sanity, boundary cleanup, ambiguity cleanup. | Defines final PR-02 validation pass. |
| 2026-02-16 | `docs/v2.1/s1/s1_roadmap.md:73` | PR-02 owns gate-item lifecycle + closure policy. | Scope anchor. |
| 2026-02-16 | `docs/v2.1/s1/s1_roadmap.md:75` | PR-02 depends on PR-01. | Dependency constraint. |
| 2026-02-16 | `docs/v2.1/s1/s1_roadmap.md:77` | PR-02 acceptance includes legal/illegal transition enforcement. | Cluster 1 requirement. |
| 2026-02-16 | `docs/v2.1/s1/s1_roadmap.md:78` | PR-02 acceptance includes closure guards + role policy. | Cluster 2 requirement. |
| 2026-02-16 | `docs/v2.1/s1/s1_roadmap.md:79` | PR-02 acceptance includes reopen + GH e2e + design enforcement. | Cluster 3 requirement. |
| 2026-02-16 | `docs/v2.1/s1/s1_spec.md:144` | L2 defines exact legal and illegal lifecycle transitions. | Cluster 1 contract surface. |
| 2026-02-16 | `docs/v2.1/s1/s1_spec.md:156` | L2 guard conditions define acceptance/tests/evidence/role/e2e/design/reopen requirements. | Cluster 2 and 3 contract surface. |
| 2026-02-16 | `docs/v2.1/s1/s1_spec.md:315` | L2 defines transition request/response contract and transition error family. | Transition API behavior target. |
| 2026-02-16 | `docs/v2.1/s1/s1_spec.md:324` | L2 transition request example shows empty `reason` for close transition. | Supports non-empty reason being reopen-specific. |
| 2026-02-17 | `docs/v2.1/s1/s1_spec.md:117` and `docs/v2.1/s1/s1_spec.md:387` | L2 now explicitly states reason field presence for all transitions and non-empty requirement only for reopen transitions. | Closes transition-reason ambiguity before implementation. |
| 2026-02-16 | `docs/v2.1/s1/s1_spec.md:37` | L2 `GateItemRef` constrains `priority` and `type` to required enums for gate items. | Requires strict metadata validation so transition policy is not based on malformed fields. |
| 2026-02-16 | `docs/v2.1/s1/s1_spec.md:364` | L2 error table includes PR-02 transition error codes. | Confirms constants needed in `internal/errors`. |
| 2026-02-16 | `docs/v2.1/s1/s1_spec.md:387` | L2 invariants require actor role + transition reason and reopen evidence. | Reopen and role guard decisions. |
| 2026-02-16 | `docs/v2.1/s1/s1_spec.md:426` | L2 acceptance scenarios include Gate A maintainer requirement and contributor rejection for Gate B closure. | Role-policy tests. |
| 2026-02-16 | `docs/v2.1/s1/s1_spec.md:441` | L2 acceptance scenario requires `e2e_opt_in` evidence when `requires_gh_e2e=true`. | GH e2e guard tests. |
| 2026-02-16 | `docs/v2.1/s1/s1_spec.md:446` | L2 acceptance scenario allows design closure only with enforcement evidence. | Design guard tests. |
| 2026-02-16 | `docs/v2.1/s1/s1_prs/s1_pr01.md:282` | PR-01 deferred evaluate transport behavior pending PR-02 start. | Required dependency default to resolve. |
| 2026-02-16 | `internal/s1gates/types.go:65` | PR-01 already defines legal state constants and validation maps. | Transition engine should build on existing state model. |
| 2026-02-16 | `internal/s1gates/evaluate_item.go:85` | PR-01 already computes deterministic `blocking_code` from missing requirements. | Reuse for closure guard error semantics. |
| 2026-02-16 | `internal/s1gates/issue_parser.go:36` | PR-01 parser exposes `priority`, `type`, `state`, `requires_gh_e2e`, and closure evidence data. | Input source for transition policy checks. |
| 2026-02-16 | `internal/s1gates/issue_parser.go:127` | Current parser defaults invalid/unknown explicit `state:` values to `open`. | Motivates strict-invalid rule for explicit malformed state metadata in PR-02. |
| 2026-02-16 | `internal/s1gates/source_parser.go:33` | PR-01 source parser resolves canonical Gate A/B membership. | Membership source for role policies. |
| 2026-02-16 | `internal/errors/errors.go:190` | PR-01 S1 errors exist; PR-02 transition errors are not yet present. | PR-02 error-constant deliverable required. |
| 2026-02-16 | `internal/errors/errors_test.go:309` | Existing pattern already asserts S1 code stability via compile-time/string tests. | Reuse for PR-02 code tests. |
| 2026-02-16 | `internal/daemon/server.go:207` | No S1 transition route is currently registered. | Supports PR-02 library-first non-goal for transport adapters. |
| 2026-02-16 | `docs/testing.md:19` | Every new error code must be tested. | Requires PR-02 error test coverage. |
| 2026-02-16 | `docs/testing.md:13` | Unit tier is for parsers/validators. | PR-02 transition policy test strategy. |

## acceptance-cluster notes

### cluster 1: legal and illegal lifecycle transitions
- Scope chosen: implement deterministic legal-transition matrix validation and current-state match checks.
- Forced decision: require `from_state` to match parsed issue `state` and reject mismatches with `E_GATE_TRANSITION_INVALID`.
- Forced decision: for library-level transitions, non-reopen empty `reason` is accepted; reopen requires non-empty reason.
- Forced decision: malformed `actor_role` values fail shape validation with `E_GATE_TRANSITION_INVALID`.
- Forced decision: lifecycle validation includes no-op/self-transition rejection and deterministic transition-validation precedence.
- Deliverables mapped: `internal/s1gates/types.go`, `internal/s1gates/transition.go`, `internal/s1gates/transition_test.go`, `internal/errors/errors.go`.

### cluster 2: closure guards and role policy
- Scope chosen: reuse PR-01 evaluator blocking-code output for acceptance/tests/closure-evidence prerequisites.
- Forced decisions:
  - classify Gate A/B by canonical release-gates membership, not priority labels alone.
  - enforce closure role policy (`maintainer` for Gate A; `maintainer|reviewer` for Gate B).
  - require non-empty closure evidence refs in both request and parsed evidence.
  - enforce deterministic closure-guard error precedence when multiple policy guards fail.
  - enforce strict transition-critical metadata validity (`priority`, `type`, explicit `state`) before applying transition policy.
- Deliverables mapped: `internal/s1gates/transition.go`, `internal/s1gates/source_parser.go` consumer logic, `internal/errors/errors.go`.

### cluster 3: reopen + gh e2e + design enforcement
- Scope chosen: add explicit reopen guard and evidence-scoped closure guards.
- Forced decisions:
  - reopen requires non-empty `reason` and `evidence_refs` (`E_GATE_REOPEN_REASON_REQUIRED`).
  - `requires_gh_e2e=true` requires passing `scope=e2e_opt_in` evidence (`E_GATE_E2E_REQUIRED`).
  - design closure enforcement is evidence-based in PR-02 (runtime-diff detection deferred).
  - close PR-01 deferred default by defining canonical library-level error behavior and deferring transport adapter mapping.
- Deliverables mapped: `internal/s1gates/transition.go`, `internal/s1gates/issue_parser.go`, tests in `internal/s1gates/transition_test.go` and `internal/s1gates/issue_parser_test.go`.

## hardening pass notes

1. Completeness: all PR-02 L3 acceptance bullets are mapped in traceability.
2. Consistency: transition guards, role policy, and error mappings align with L2 lifecycle and error tables.
3. Traceability: every behavior-changing decision is linked to at least one test, including non-reopen empty-reason acceptance, malformed actor-role rejection, deterministic error-precedence ordering, strict metadata validation, and state-line persistence placement semantics.
4. Boundary cleanup: PR-02 excludes aggregate gate/slice readiness, gate-set change validation, and release reporting.
5. Ambiguity cleanup: PR-01 transport default is resolved for PR-02 implementation start by explicit library-level policy.
