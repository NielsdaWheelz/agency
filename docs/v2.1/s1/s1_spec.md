# Slice S1 - Platform Hardening Gates Spec

Last updated: 2026-02-16
Status: frozen
Upstream slice: `docs/v2.1/slice-roadmap.md` (Slice S1)

## 1. Goal & Scope

**Goal**: close release-blocking safety and contract integrity issues.

**In Scope**:
- Gate A (`p0` safety) and Gate B (`p1` parity-critical) closure semantics for v2.1.
- Deterministic definition of "closed with tests" for every issue referenced by
  `docs/v2.1/release-gates.md` sections A and B.
- Evidence requirements for issue closure and gate completion.
- Slice completion rules that unblock downstream Slice S2 work.

**Out of Scope**:
- Implementing Slice S2+ behavior changes (daemon read convergence, chat control
  plane, runner portability, review/pr/merge command expansion).
- GUI/TUI surfaces and stretch-scope parity features.
- New runtime product features not required to close Gate A/B items.

---

## 2. Domain Models

### GateSet

| Field | Type | Constraints |
|---|---|---|
| `slice` | string | must be `S1` |
| `gate_a_items` | []GateItemRef | exactly the issue paths listed in `release-gates.md` Gate A |
| `gate_b_items` | []GateItemRef | exactly the issue paths listed in `release-gates.md` Gate B |
| `source_ref` | string | must reference `docs/v2.1/release-gates.md` |

### GateItemRef

| Field | Type | Constraints |
|---|---|---|
| `issue_path` | string | existing `docs/issues/*.md` path |
| `priority` | enum | `p0` or `p1` derived from issue title/labels |
| `type` | enum | `tech-debt`, `bug`, `design`, `enhancement`, or `refactor` |
| `state` | enum | `open`, `in_progress`, `ready_for_verification`, `closed` |
| `acceptance_complete` | bool | true only when all issue acceptance checklist items are complete |
| `tests_complete` | bool | true only when required automated tests/checks passed for this issue |
| `requires_gh_e2e` | bool | true when gate item affects GitHub-integrated PR/merge mutation flows |
| `evidence_refs` | []string | non-empty when `state=closed` |

`priority` derivation rule:
- use `labels:` metadata when present.
- if label and title disagree, label value is authoritative.

`requires_gh_e2e` assignment rule:
- true when the issue's accepted behavior changes PR creation/sync, merge, or
  PR-close mutation behavior that depends on `gh` integration.
- false otherwise.

### TestEvidence

| Field | Type | Constraints |
|---|---|---|
| `issue_path` | string | must reference one Gate A/B issue |
| `command` | string | executable check command (example: `go test ./...`) |
| `scope` | enum | `targeted`, `suite`, `e2e_opt_in`, `lint`, `format`, `vet` |
| `result` | enum | `pass` or `fail` |
| `artifact_ref` | string | CI log, PR report, or test output reference |
| `recorded_at` | RFC3339 UTC string | required when `result=pass` |

Additional constraint:
- if `scope=suite`, `command` must be one of:
  - `go test ./...`
  - `make test`
  - `make check`
  - `make verify`

### ClosureEvidence

| Field | Type | Constraints |
|---|---|---|
| `issue_path` | string | must reference one Gate A/B issue |
| `implemented_refs` | []string | at least one merged PR/commit/report reference |
| `targeted_test_refs` | []TestEvidence | non-empty |
| `suite_test_refs` | []TestEvidence | must include at least one passing `go test ./...` equivalent |
| `notes` | string | optional |

Required issue-stub representation for closed gate items:
- section heading: `## closure evidence`
- required keys under that section:
  - `implemented_refs`
  - `targeted_test_refs`
  - `suite_test_refs`

### GateSetChange

| Field | Type | Constraints |
|---|---|---|
| `gate_id` | enum | `A` or `B` |
| `change_type` | enum | `add`, `remove`, `replace`, `reorder` |
| `issue_path` | string | required for `add`, `remove`, `replace` |
| `issue_paths` | []string | required for `reorder`, length `>= 2` |
| `reason` | string | required, non-empty |
| `approved_by` | string | required for `remove` and `replace` |
| `synced_issue_map` | bool | must be true for valid change |

`change_type=reorder` constraint:
- reorder is valid without `approved_by` only when gate membership is unchanged.

### GateTransition

| Field | Type | Constraints |
|---|---|---|
| `issue_path` | string | must reference one Gate A/B issue |
| `from_state` | enum | `open`, `in_progress`, `ready_for_verification`, `closed` |
| `to_state` | enum | `open`, `in_progress`, `ready_for_verification`, `closed` |
| `actor_role` | enum | `maintainer`, `reviewer`, `contributor` |
| `reason` | string | required for reopen (`closed -> in_progress`) |
| `evidence_refs` | []string | required when `to_state=closed` |

### GateStatus

| Field | Type | Constraints |
|---|---|---|
| `gate_id` | enum | `A` or `B` |
| `total_items` | int | `>= 1` |
| `closed_items` | int | `0..total_items` |
| `blocking_items` | []string | issue paths with `state != closed` |
| `status` | enum | `blocked`, `ready` |

`status=ready` if and only if `closed_items == total_items`.

---

## 3. State Machines

### Gate Item Lifecycle

States:
- `open`
- `in_progress`
- `ready_for_verification`
- `closed`

Legal transitions:
1. `open -> in_progress`
2. `in_progress -> ready_for_verification`
3. `ready_for_verification -> closed`
4. `closed -> in_progress` (regression or reopening)

Illegal transitions:
1. `open -> closed`
2. `open -> ready_for_verification`
3. `in_progress -> closed`
4. `closed -> ready_for_verification`

Guard conditions:
1. `in_progress -> ready_for_verification` requires all declared issue acceptance
   criteria to be implemented in merged code/docs.
2. `ready_for_verification -> closed` requires:
   - `acceptance_complete=true`
   - `tests_complete=true`
   - closure evidence block present
   - `evidence_refs` non-empty
3. `ready_for_verification -> closed` for Gate A (`p0`) requires
   `actor_role=maintainer`.
4. `ready_for_verification -> closed` for Gate B (`p1`) requires
   `actor_role in {maintainer, reviewer}`.
5. if `requires_gh_e2e=true`, closure requires at least one passing
   `scope=e2e_opt_in` evidence entry.
6. if `type=design`, closure may be contract-only (no runtime code changes)
   only when enforcement evidence exists (tests, lint, or CI checks proving the
   contract is enforced).
7. `closed -> in_progress` requires non-empty regression reason and at least one
   supporting evidence reference.

### Gate Lifecycle

States:
- `blocked`
- `ready`

Legal transitions:
1. `blocked -> ready` when every GateItemRef in that gate is `closed`.
2. `ready -> blocked` when any GateItemRef is reopened.

### Gate Set Lifecycle

States:
- `stable`
- `pending_change`
- `stable_with_new_version`

Legal transitions:
1. `stable -> pending_change` when a GateSetChange proposal exists.
2. `pending_change -> stable_with_new_version` when change reason is approved
   and `release-gates.md` and `issue-map.md` remain consistent.
3. `stable_with_new_version -> pending_change` on subsequent proposals.

Illegal transitions:
1. `stable -> stable_with_new_version` without a GateSetChange.
2. `pending_change -> stable_with_new_version` when `synced_issue_map=false`.

---

## 4. API Contracts

This slice defines a normative gate-evaluation interface contract. It may be
implemented as CLI tooling, CI checks, or library calls. Behavior and schemas
below are normative regardless of implementation surface.

### POST /spec/v2.1/s1/gate-item/evaluate

**request**:
```json
{
  "issue_path": "docs/issues/daemon-p0-08-unsafe-deletes-in-landing.md"
}
```

**response 200**:
```json
{
  "ok": true,
  "issue_path": "docs/issues/daemon-p0-08-unsafe-deletes-in-landing.md",
  "state": "ready_for_verification",
  "requires_gh_e2e": false,
  "acceptance_complete": true,
  "tests_complete": false,
  "closure_evidence_present": false,
  "missing_requirements": [
    "closure_evidence_block",
    "suite_test_evidence"
  ],
  "evidence_refs": []
}
```

**errors**:
- `E_GATE_ITEM_NOT_FOUND` (404): `issue_path` does not exist.
- `E_GATE_ITEM_INVALID` (400): malformed issue metadata or missing acceptance section.
- `E_GATE_ITEM_ACCEPTANCE_INCOMPLETE` (409): acceptance checklist is incomplete.
- `E_GATE_ITEM_TESTS_INCOMPLETE` (409): test evidence is missing or failing.
- `E_GATE_ITEM_EVIDENCE_MISSING` (409): closure requested without non-empty `evidence_refs`.
- `E_GATE_ITEM_CLOSURE_BLOCK_MISSING` (409): required closure evidence block is absent.

### POST /spec/v2.1/s1/gates/evaluate

**request**:
```json
{
  "gate_set_source": "docs/v2.1/release-gates.md",
  "slice": "S1"
}
```

**response 200**:
```json
{
  "ok": true,
  "slice": "S1",
  "gate_a": {
    "status": "blocked",
    "total_items": 3,
    "closed_items": 1,
    "blocking_items": [
      "docs/issues/store-p0-08-remove-paths-use-raw-osremoveall-without-safety-checks.md",
      "docs/issues/daemon-p0-08-unsafe-deletes-in-landing.md"
    ]
  },
  "gate_b": {
    "status": "blocked",
    "total_items": 24,
    "closed_items": 4,
    "blocking_items": [
      "docs/issues/cli-p1-01-all-commands-drop-cmdcontext.md"
    ]
  },
  "slice_ready": false
}
```

**errors**:
- `E_GATE_SET_INVALID` (400): Gate A/B issue list cannot be resolved deterministically.
- `E_GATE_SET_DRIFT` (409): release-gates and issue-map references diverge.
- `E_GATE_BLOCKED` (409): `slice_ready` requested as true while any gate is blocked.

### POST /spec/v2.1/s1/gates/change-validate

**request**:
```json
{
  "gate_id": "B",
  "change_type": "remove",
  "issue_path": "docs/issues/example.md",
  "reason": "Superseded by split issues",
  "approved_by": "@owner",
  "synced_issue_map": true
}
```

**response 200**:
```json
{
  "ok": true,
  "valid": true
}
```

**errors**:
- `E_GATE_CHANGE_REASON_REQUIRED` (400): `reason` missing/empty.
- `E_GATE_CHANGE_TARGET_REQUIRED` (400): change target (`issue_path` or `issue_paths`) is missing.
- `E_GATE_CHANGE_APPROVAL_REQUIRED` (409): removal/replacement missing explicit approval.
- `E_GATE_SET_DRIFT` (409): change leaves `release-gates.md` and `issue-map.md` unsynchronized.

### POST /spec/v2.1/s1/gate-item/transition

**request**:
```json
{
  "issue_path": "docs/issues/events-p0-event-system-hardening.md",
  "from_state": "ready_for_verification",
  "to_state": "closed",
  "actor_role": "maintainer",
  "reason": "",
  "evidence_refs": [
    "pr:123",
    "ci:build-456"
  ]
}
```

**response 200**:
```json
{
  "ok": true,
  "issue_path": "docs/issues/events-p0-event-system-hardening.md",
  "state": "closed"
}
```

**errors**:
- `E_GATE_TRANSITION_INVALID` (409): illegal lifecycle transition requested.
- `E_GATE_APPROVAL_REQUIRED` (409): role requirements for transition are not satisfied.
- `E_GATE_REOPEN_REASON_REQUIRED` (400): reopen transition is missing reason/evidence.
- `E_GATE_E2E_REQUIRED` (409): required GH e2e evidence is missing for closure.

---

## 5. Error Codes

| code | http | meaning |
|---|---:|---|
| `E_GATE_ITEM_NOT_FOUND` | 404 | referenced issue path does not exist |
| `E_GATE_ITEM_INVALID` | 400 | issue stub shape is invalid for gate evaluation |
| `E_GATE_ITEM_ACCEPTANCE_INCOMPLETE` | 409 | issue acceptance checklist is not fully complete |
| `E_GATE_ITEM_TESTS_INCOMPLETE` | 409 | required automated test evidence is missing or failing |
| `E_GATE_ITEM_EVIDENCE_MISSING` | 409 | closure attempted without non-empty `evidence_refs` |
| `E_GATE_ITEM_CLOSURE_BLOCK_MISSING` | 409 | required closure evidence block is absent |
| `E_GATE_SET_INVALID` | 400 | gate set cannot be parsed/resolved from source docs |
| `E_GATE_SET_DRIFT` | 409 | `release-gates.md` and `issue-map.md` are inconsistent |
| `E_GATE_CHANGE_REASON_REQUIRED` | 400 | gate-set change omits non-empty reason |
| `E_GATE_CHANGE_TARGET_REQUIRED` | 400 | gate-set change omits required target fields |
| `E_GATE_CHANGE_APPROVAL_REQUIRED` | 409 | remove/replace gate-set change lacks explicit approver |
| `E_GATE_TRANSITION_INVALID` | 409 | requested gate item state transition is illegal |
| `E_GATE_APPROVAL_REQUIRED` | 409 | transition caller role does not satisfy policy |
| `E_GATE_REOPEN_REASON_REQUIRED` | 400 | reopen transition is missing reason or evidence |
| `E_GATE_E2E_REQUIRED` | 409 | closure requires GH e2e evidence for this gate item |
| `E_GATE_BLOCKED` | 409 | gate or slice attempted to complete while blockers remain |

---

## 6. Invariants

1. Gate A and Gate B item sets are exactly the issue paths in
   `docs/v2.1/release-gates.md` sections A and B.
2. A gate is `ready` if and only if every item in its set is `closed`.
3. An item is `closed` if and only if acceptance criteria are complete, required
   tests pass, and evidence references are present.
4. Slice S1 is complete if and only if Gate A and Gate B are both `ready`.
5. Any reopened gate item immediately returns its gate status to `blocked`.
6. Any behavior-changing closure must include at least one automated test update
   and one suite-level pass record (`go test ./...` equivalent).
7. Any closed Gate A/B issue must include a closure evidence block with
   implementation references and test evidence.
8. Any GateSet change must update `release-gates.md` and `issue-map.md` in the
   same change set; partial updates are invalid.
9. Every gate-item state transition must include actor role and transition reason.
10. Any reopen transition (`closed -> in_progress`) must include regression evidence.
11. Slice S1 L2 is not freeze-eligible while Section 9 has unresolved rows.
12. Gate B item closure requires reviewer or maintainer role.
13. `change_type=reorder` does not require approver only when membership is unchanged.
14. `requires_gh_e2e=true` items require passing GH e2e evidence before closure.
15. `type=design` items can close without runtime changes only when enforcement
    evidence demonstrates contract compliance.

---

## 7. Acceptance Scenarios

### scenario: gate item cannot close without tests
- **given**: a Gate B item has implemented code changes but no passing automated
  test evidence
- **when**: the item is evaluated for closure
- **then**: closure is rejected with `E_GATE_ITEM_TESTS_INCOMPLETE`

### scenario: gate becomes ready when all items are closed
- **given**: all Gate A items are `closed`
- **when**: gate evaluation runs
- **then**: Gate A status is `ready` and `blocking_items` is empty

### scenario: slice remains blocked while any Gate B item is open
- **given**: Gate A is `ready` and Gate B has at least one non-closed item
- **when**: slice readiness is evaluated
- **then**: `slice_ready=false` and `E_GATE_BLOCKED` semantics apply

### scenario: reopened issue re-blocks the gate
- **given**: Gate B is `ready`
- **when**: one previously closed Gate B item is reopened
- **then**: Gate B transitions to `blocked`

### scenario: gate-set drift is rejected
- **given**: `release-gates.md` changed but `issue-map.md` was not updated
- **when**: gate evaluation runs
- **then**: evaluation fails with `E_GATE_SET_DRIFT`

### scenario: p0 closure requires maintainer role
- **given**: a Gate A item is in `ready_for_verification`
- **when**: a non-maintainer actor attempts `ready_for_verification -> closed`
- **then**: transition is rejected with `E_GATE_APPROVAL_REQUIRED`

### scenario: reopen requires reason and evidence
- **given**: a gate item is in `closed`
- **when**: transition to `in_progress` is requested without regression reason
- **then**: transition is rejected with `E_GATE_REOPEN_REASON_REQUIRED`

### scenario: gate b closure by contributor is rejected
- **given**: a Gate B item is in `ready_for_verification`
- **when**: a contributor attempts `ready_for_verification -> closed`
- **then**: transition is rejected with `E_GATE_APPROVAL_REQUIRED`

### scenario: gh e2e is required for gh mutation items
- **given**: a Gate B item has `requires_gh_e2e=true`
- **when**: closure is requested without passing `e2e_opt_in` evidence
- **then**: closure is rejected with `E_GATE_E2E_REQUIRED`

### scenario: design item closes with contract enforcement evidence
- **given**: a Gate B `type=design` item updates contract behavior without runtime code
- **when**: closure includes test/lint/CI enforcement evidence
- **then**: closure is allowed

### scenario: l2 freeze blocked by unresolved defaults
- **given**: Section 9 has at least one unresolved question row
- **when**: freeze readiness is evaluated
- **then**: slice spec remains `draft` and cannot advance to frozen state

---

## 8. Traceability Map

| l1 acceptance item | spec section(s) |
|---|---|
| all gates listed in `release-gates.md` section A and B are closed with tests | 1, 2, 3, 4, 5, 6, 7 |

---

## 9. Unresolved Questions + Temporary Defaults (must be empty before freeze)

| question | temporary default behavior | owner | due |
|---|---|---|---|
