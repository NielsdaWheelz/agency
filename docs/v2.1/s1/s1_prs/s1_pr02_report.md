# PR-02 Implementation Report: Gate Item Lifecycle + Closure Policy

## 1. Summary of Changes

### New files
- `internal/s1gates/transition.go` — core transition engine implementing `TransitionGateItem`, closure guards, reopen guards, gate membership resolution, and state persistence.
- `internal/s1gates/transition_test.go` — 18 test functions covering all acceptance scenarios plus fixture helpers.

### Modified files
- `internal/errors/errors.go` — added 4 PR-02 error codes: `E_GATE_TRANSITION_INVALID`, `E_GATE_APPROVAL_REQUIRED`, `E_GATE_REOPEN_REASON_REQUIRED`, `E_GATE_E2E_REQUIRED`.
- `internal/errors/errors_test.go` — added `TestS1PR02ErrorCodesExist` and `TestS1PR02ErrorFormat` (compile-time + format stability assertions for all 4 codes).
- `internal/s1gates/types.go` — added actor-role constants (`maintainer`, `reviewer`, `contributor`), `ValidActorRoles` set, `EnforcementScopes` set, `GateTransition` request model, `GateTransitionResult` response model, and `CanonicalGateSourcePath` constant.
- `internal/s1gates/issue_parser.go` — added transition-critical metadata validation: missing priority → `E_GATE_ITEM_INVALID`, missing type → `E_GATE_ITEM_INVALID`, invalid explicit state → `E_GATE_ITEM_INVALID`, invalid evidence scope/result enums → `E_GATE_ITEM_INVALID`.
- `internal/s1gates/issue_parser_test.go` — added 5 new tests (`InvalidScope`, `InvalidResult`, `InvalidExplicitState`, `MissingPriority`, `MissingType`) and fixed `TestParseIssue_PriorityFallsBackToTitleTag` to include a type label (required by new validation).

## 2. Problems Encountered

1. **parseStateLine silent fallthrough**: The original `parseStateLine` returned `StateOpen` for both "no state: line" and "invalid explicit state: value". PR-02 requires distinguishing these. Solved by changing return type to `(string, error)`.

2. **Existing test fixture missing type label**: `TestParseIssue_PriorityFallsBackToTitleTag` used an inline fixture with no `labels:` line, meaning `deriveType` returned `""`. With PR-02's strict type validation, this broke. Fixed by adding `labels: type:tech-debt` to the fixture while keeping priority absent from labels (preserving the title-tag fallback behavior being tested).

3. **Transition tests need file writes**: `TransitionGateItem` persists state to disk, so tests can't use static testdata. Solved with `setupTransitionRepo` helper that creates full temp fixture repos, and `copyRepo` for parallel test isolation.

4. **Gate membership resolution requires full repo context**: `ParseGateSet` validates that all listed issue paths exist via `FileExistsFn`. Test fixtures must create ALL gate items, not just the one under test. The `setupTransitionRepo` helper handles this.

## 3. Solutions Implemented

- **Deterministic error precedence**: Transition validation follows a strict 5-step precedence (parse → from_state → edge → role → membership). Closure guards follow a strict 5-step precedence (evaluator → evidence → role policy → e2e → design). Both orders are documented in code and tested in `TestTransition_ErrorPrecedenceIsDeterministic`.

- **State persistence**: Implemented 3-tier insert strategy: (1) replace existing `state:` line, (2) insert after `labels:`, (3) insert after title. Verified with two dedicated persistence tests.

- **Reuse of PR-01 evaluator**: Closure guards call `EvaluateGateItem` and check its `BlockingCode` before running PR-02-specific guards. This avoids duplicating acceptance/tests/closure-block/evidence-ref validation.

- **Gate membership via canonical source**: `resolveGateMembership` reads `docs/v2.1/constitution.md` relative to `repoRoot`, parses with `ParseGateSet`, and classifies the issue as Gate A, B, or non-member. Follows the decision ledger: "use canonical gate membership from constitution.md via PR-01 source parser; do not infer from priority alone."

## 4. Decisions Made (and Why)

| Decision | Rationale |
|---|---|
| Changed `parseStateLine` return to `(string, error)` | Needed to surface invalid explicit state metadata as `E_GATE_ITEM_INVALID` per spec. Only one caller (`ParseIssue`), so blast radius is zero. |
| Added priority/type validation in `ParseIssue` | PR-02 spec requires "transition-critical metadata validity". These fields are needed for gate classification and design-item enforcement checks. |
| Evidence scope/result enum validation before semantic checks | Prevents invalid enum values from silently bypassing closure guards. Consistent with spec: "unknown evidence scope/result are invalid artifacts." |
| Fixture repos use temp dirs exclusively | `TransitionGateItem` writes to disk. Temp dirs ensure test isolation and avoid modifying committed testdata. |
| `EnforcementScopes` includes all valid scopes | Spec says "scope in {targeted, suite, e2e_opt_in, lint, format, vet}" for design enforcement evidence. All 6 scopes qualify. |
| `CanonicalGateSourcePath` as package-level constant | Hardcoded to `docs/v2.1/constitution.md`. Could be parameterized later, but the spec defines a fixed canonical source and the decision ledger confirms this. |

## 5. Deviations from L4/L3/L2

None. All behavior matches the L2 spec (s1_spec.md) state machines, guard conditions, and error codes exactly. All L3 roadmap acceptance items are covered. No scope creep beyond PR-02 ownership.

## 6. Commands to Run New/Changed Behavior

```bash
# run all PR-02 transition tests
go test ./internal/s1gates/... -run "TestTransition" -v -count=1

# run all PR-02 parser validation tests
go test ./internal/s1gates/... -run "TestParseIssue_Invalid|TestParseIssue_Missing" -v -count=1

# run PR-02 error code tests
go test ./internal/errors/... -run "TestS1PR02" -v -count=1

# run full s1gates suite (includes PR-01 regression)
go test ./internal/s1gates/... -v -count=1

# run all error tests
go test ./internal/errors/... -v -count=1
```

## 7. Commands Used to Verify Correctness

```bash
# targeted tests (37 tests, 0 failures)
go test ./internal/s1gates/... -v -count=1

# error tests (all pass including PR-02)
go test ./internal/errors/... -v -count=1

# race detector (0 races)
go test ./internal/s1gates/... ./internal/errors/... -race -count=1

# go vet (clean)
go vet ./internal/s1gates/... ./internal/errors/...

# gofmt (clean after auto-format)
gofmt -l ./internal/s1gates/ ./internal/errors/

# full verify pipeline (lint 0 issues, all tests pass, build succeeds)
make verify
```

## 8. Traceability Table

| Acceptance Item | Files | Tests | Status |
|---|---|---|---|
| Legal and illegal gate-item state transitions enforced per S1 lifecycle | `transition.go`, `types.go`, `errors.go` | `TestTransition_EnforcesLegalAndIllegalTransitions`, `TestTransition_RejectsNoOpTransition`, `TestTransition_FromStateMustMatchCurrentState`, `TestTransition_InvalidActorRoleReturnsTransitionInvalid`, `TestTransition_NonReopenAllowsEmptyReason`, `TestTransition_RejectsIssueOutsideGateMembership` | PASS |
| Closure guards enforce acceptance, tests, evidence, role requirements | `transition.go`, `issue_parser.go`, `errors.go` | `TestTransition_CloseRequiresAcceptanceAndTestsAndEvidence`, `TestTransition_P0CloseRequiresMaintainer`, `TestTransition_GateBCloseByContributorRejected`, `TestTransition_CloseSucceedsForGateBReviewer`, `TestParseIssue_InvalidExplicitStateReturnsEGateItemInvalid`, `TestParseIssue_MissingPriorityReturnsEGateItemInvalid`, `TestParseIssue_MissingTypeReturnsEGateItemInvalid` | PASS |
| Reopen requires rationale/evidence; GH e2e and design enforcement when applicable | `transition.go`, `types.go`, `errors.go` | `TestTransition_ReopenRequiresReasonAndEvidence`, `TestTransition_RequiresGHE2EWhenFlagged`, `TestTransition_DesignCloseRequiresEnforcementEvidence`, `TestTransition_PersistsStateByReplacingExistingStateLine`, `TestTransition_PersistsStateByInsertingAfterLabelsWhenStateMissing`, `TestTransition_ErrorPrecedenceIsDeterministic` | PASS |
| PR-02 error codes stable and formatted | `errors.go`, `errors_test.go` | `TestS1PR02ErrorCodesExist`, `TestS1PR02ErrorFormat` | PASS |
| Evidence scope/result enum validity enforced | `issue_parser.go`, `issue_parser_test.go` | `TestParseIssue_InvalidScopeReturnsEGateItemInvalid`, `TestParseIssue_InvalidResultReturnsEGateItemInvalid` | PASS |

## 9. Commit Message

```
feat(s1gates): implement gate-item lifecycle transitions and closure policy (PR-02)

Add TransitionGateItem engine enforcing the S1 gate-item state machine
with deterministic validation and closure-guard precedence:

- Legal transitions: open→in_progress, in_progress→rfv, rfv→closed,
  closed→in_progress. All other edges rejected (E_GATE_TRANSITION_INVALID).
- Closure guards enforce PR-01 evaluator preconditions, evidence-ref
  non-empty, Gate A (maintainer-only) / Gate B (maintainer or reviewer)
  role policy, GH e2e evidence requirement, and design enforcement
  evidence requirement — in documented precedence order.
- Reopen guard requires non-empty reason and evidence_refs.
- State persistence replaces existing state: line or inserts after
  labels: (or title if labels absent).

Error codes added:
- E_GATE_TRANSITION_INVALID: illegal lifecycle edge or validation failure
- E_GATE_APPROVAL_REQUIRED: role policy violation on closure
- E_GATE_REOPEN_REASON_REQUIRED: missing regression reason/evidence
- E_GATE_E2E_REQUIRED: missing GH e2e evidence on flagged items

Parser hardening (issue_parser.go):
- Reject missing priority (E_GATE_ITEM_INVALID)
- Reject missing type (E_GATE_ITEM_INVALID)
- Reject invalid explicit state metadata (E_GATE_ITEM_INVALID)
- Validate evidence scope/result enums (E_GATE_ITEM_INVALID)

Types added (types.go):
- Actor role constants: maintainer, reviewer, contributor
- GateTransition request and GateTransitionResult response models
- ValidActorRoles and EnforcementScopes validation sets

Tests: 23 new test functions (18 transition + 5 parser validation),
all existing tests pass without regression. make verify green.

Refs: docs/v2.1/s1/s1_prs/s1_pr02.md
```
