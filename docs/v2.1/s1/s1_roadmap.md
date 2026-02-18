# Slice S1 - PR Roadmap

Last updated: 2026-02-18
Status: draft
Upstream spec: `docs/v2.1/s1/s1_spec.md`

## 0. Contract inventory

| cluster id | l2 cluster (normative surface) |
|---|---|
| C1 | Gate corpus and closure-evidence intake (`Domain Models`: `GateSet`, `GateItemRef`, `TestEvidence`, `ClosureEvidence`) |
| C2 | Gate-item lifecycle policy (`State Machines`: gate-item lifecycle + closure/reopen guards) |
| C3 | Aggregate readiness policy (`State Machines`: gate lifecycle + slice readiness behavior) |
| C4 | Gate-set change policy (`Domain Models`: `GateSetChange`; `State Machines`: gate-set lifecycle) |
| C5 | Release gate enforcement and closure reporting (`API Contracts`, `Error Codes`, `Invariants`, acceptance-governance linkage) |

## 1. Dependency graph

```text
PR-01 Gate Corpus + Evidence Intake
  | \
  |  \
  v   v
PR-02 Gate Item Lifecycle + Closure Policy   PR-04 Gate Set Change Validation
  |
  v
PR-03 Gate + Slice Readiness Evaluation
  \                                   /
   \                                 /
    v                               v
      PR-05 Release Gate Enforcement + Closure Reporting
```

## 2. Ownership matrix

| contract cluster (from l2) | owning pr |
|---|---|
| C1: GateSet and GateItemRef intake, closure-evidence schema intake, and gate-item precondition evaluation semantics (`Domain Models`, `API Contracts` gate-item evaluate, item-level error model) | PR-01 |
| C2: Gate-item lifecycle transitions, closure guards, role policy, reopen policy, GH e2e requirement, and design-item enforcement policy (`State Machines`, gate-item transition API, transition errors/invariants) | PR-02 |
| C3: Gate readiness and slice readiness aggregation, re-block behavior on reopen, and release-gate/issue-map drift detection during evaluation (`State Machines`, gates evaluate API, aggregate errors/invariants) | PR-03 |
| C4: Gate-set change validation policy for add/remove/replace/reorder and synchronization constraints (`Domain Models` GateSetChange, change-validate API, change-validation errors/invariants) | PR-04 |
| C5: Release-facing gate enforcement and closure-evidence reporting, including freeze-readiness governance linkage (`Goal & Scope`, `Invariants`, acceptance-governance scenario) | PR-05 |

## 3. Acceptance coverage map

| l2 acceptance scenario | primary owner pr | supporting pr(s) |
|---|---|---|
| scenario: gate item cannot close without tests | PR-02 | PR-01 |
| scenario: gate becomes ready when all items are closed | PR-03 | PR-02 |
| scenario: slice remains blocked while any Gate B item is open | PR-03 | none |
| scenario: reopened issue re-blocks the gate | PR-02 | PR-03 |
| scenario: gate-set drift is rejected | PR-03 | PR-04 |
| scenario: p0 closure requires maintainer role | PR-02 | none |
| scenario: reopen requires reason and evidence | PR-02 | none |
| scenario: gate b closure by contributor is rejected | PR-02 | none |
| scenario: gh e2e is required for gh mutation items | PR-02 | none |
| scenario: design item closes with contract enforcement evidence | PR-02 | PR-05 |
| scenario: l2 freeze blocked by unresolved defaults | PR-05 | none |

## 4. PRs

### PR-01: Gate Corpus + Evidence Intake
- **Goal**: establish deterministic gate-item intake and closure-evidence parsing as the foundation for all S1 evaluations.
- **Dependencies**: none.
- **Acceptance**:
  - Gate A and Gate B membership is resolved deterministically from the canonical gate source with stable issue references.
  - Gate-item metadata and closure-evidence blocks are normalized into one machine-checkable model for downstream evaluation.
  - Invalid or incomplete gate-item artifacts are surfaced through the item-level S1 error model.
- **Non-goals**:
  - No gate-item state transition mutation rules.
  - No aggregate gate or slice readiness evaluation.

### PR-02: Gate Item Lifecycle + Closure Policy
- **Goal**: enforce gate-item state machine and closure-policy rules for transition and closure decisions.
- **Dependencies**: PR-01.
- **Acceptance**:
  - Legal and illegal gate-item state transitions are enforced exactly per S1 lifecycle contract.
  - Closure guards enforce acceptance completeness, test completeness, evidence requirements, and role requirements (Gate A vs Gate B).
  - Reopen transitions require explicit regression rationale/evidence, and GH e2e or design-enforcement requirements are enforced when applicable.
- **Non-goals**:
  - No gate-level or slice-level aggregate readiness decisions.
  - No gate-set membership change validation.

### PR-03: Gate + Slice Readiness Evaluation
- **Goal**: provide deterministic aggregate evaluation for Gate A, Gate B, and Slice S1 readiness with explicit blocker reporting.
- **Dependencies**: PR-01, PR-02.
- **Acceptance**:
  - Aggregate evaluation returns per-gate status, closed/total counts, blocking item references, and slice readiness.
  - Gate readiness transitions reflect reopen behavior immediately (ready to blocked).
  - Drift and blocked-completion conditions are surfaced with the correct aggregate error semantics for evaluation flows.
- **Non-goals**:
  - No policy for changing gate membership.
  - No release-reporting integration workflow.

### PR-04: Gate Set Change Validation
- **Goal**: validate gate-set change proposals deterministically without introducing ownership overlap with readiness evaluation.
- **Dependencies**: PR-01.
- **Acceptance**:
  - Change validation enforces required reason and target fields by change type.
  - Approval requirements are enforced for remove/replace, and reorder semantics are validated against membership-preserving rules.
  - Synchronization requirements between release-gate and issue-map tracking are enforced for valid change proposals.
- **Non-goals**:
  - No gate-item lifecycle transitions.
  - No aggregate readiness computation.

### PR-05: Release Gate Enforcement + Closure Reporting
- **Goal**: make S1 gate-evaluation outputs operationally binding for release-readiness and closure evidence visibility.
- **Dependencies**: PR-03, PR-04.
- **Acceptance**:
  - Release-readiness flows consume S1 gate-evaluation outcomes to decide blocked vs ready state without ad-hoc interpretation.
  - Closure evidence for closed gate items is surfaced through one consistent reporting contract for implementation/test proof.
  - Freeze-readiness governance includes unresolved-default blocking semantics so incomplete defaults cannot be treated as frozen.
  - Temporary S1 namespace implementation is fully migrated and cleaned up (legacy slice-scoped namespace removed; durable release-gates namespace remains).
  - Release-gate orchestration is issue-source abstracted for GH-native migration, with markdown issue stubs retained only as S1 compatibility provider.
- **Non-goals**:
  - No S2+ product feature delivery (daemon read convergence, chat continuation, runner-capability expansion, review/merge expansion).
  - No GUI/full-TUI additions.
  - No full GitHub issue provider delivery in PR-05; only boundary abstraction plus markdown compatibility adapter.

## 5. L3 hardening checks

1. Ownership completeness: C1-C5 each have exactly one owning PR.
2. Ordering correctness: no PR depends on behavior from an unmerged PR.
3. Acceptance completeness: every S1 L2 acceptance scenario has a primary owner.
4. Scope purity: roadmap content avoids file paths, signatures, and test-case detail.
