# S1 PR-01 Gate Corpus + Evidence Intake - Decisions

Last updated: 2026-02-16
Status: draft

## decision ledger

| ID | Problem | Decision | Rejected alternatives | Invariant impact | Test impact | Fallback/Default | Owner | Due |
|---|---|---|---|---|---|---|---|---|
| PR01-D-001 | Which document defines authoritative Gate A/B membership? | Use `docs/v2.1/release-gates.md` Gate A and Gate B sections only. | Scanning `issue-map.md` directly; merging multiple gate sources. | Preserves invariant that gate sets are exactly release-gates A/B. | Add parser tests for A/B extraction only and order stability. | none | `@nnandal` + `Codex` | fixed in PR-01 |
| PR01-D-002 | How to parse gate memberships deterministically? | Accept numbered list entries with backtick-wrapped issue paths under Gate A/B headings; preserve order; reject duplicates. | Free-form bullet parsing; unordered set parsing. | Ensures stable, reproducible corpus for downstream evaluation. | Add tests for deterministic order and duplicate rejection. | none | `@nnandal` + `Codex` | fixed in PR-01 |
| PR01-D-003 | How should missing issue references be surfaced? | Missing/non-`docs/issues/` gate paths fail corpus load with `E_GATE_SET_INVALID`. | Defer missing-path detection to later PRs. | Enforces early contract integrity at intake boundary. | Add fixture with missing path and assert `E_GATE_SET_INVALID`. | none | `@nnandal` + `Codex` | fixed in PR-01 |
| PR01-D-004 | What is the machine-checkable closure-evidence format? | `## closure evidence` contains first fenced `json` block with required keys `implemented_refs`, `targeted_test_refs`, `suite_test_refs`. | Parsing ad-hoc markdown bullets; YAML-like free text. | Makes closure evidence schema enforceable and auditable. | Add parser tests for valid/invalid closure evidence JSON blocks. | Missing/invalid block maps to `E_GATE_ITEM_CLOSURE_BLOCK_MISSING` or `E_GATE_ITEM_INVALID` depending on failure type. | `@nnandal` + `Codex` | fixed in PR-01 |
| PR01-D-005 | How are item metadata fields derived from current issue stubs? | `priority` from labels first; `type` from `type:*` label; optional `state:` metadata with default `open`. | Title-only parsing for priority/type; requiring `state:` on all stubs. | Keeps current stubs parseable while enabling deterministic normalized fields. | Add tests for label precedence and missing-state default behavior. | `state=open` when absent | `@nnandal` + `Codex` | fixed in PR-01 |
| PR01-D-006 | How is `requires_gh_e2e` assigned deterministically in PR-01? | Set true only when labels include `requires:gh-e2e`; otherwise false. | Heuristic keyword matching in summary/context text; area-based inference. | Prevents non-deterministic policy drift from text heuristics. | Add tests for label-present true and label-absent false. | false | `@nnandal` + `Codex` | fixed in PR-01 |
| PR01-D-007 | How should incomplete requirements map to item-level error model? | Return ordered `missing_requirements` plus deterministic primary `blocking_code` precedence: acceptance incomplete -> closure block missing -> evidence missing -> tests incomplete. | Returning arbitrary first failure by map iteration; returning all errors without primary code. | Produces stable downstream behavior for closure/report consumers. | Add evaluation tests with multi-failure fixtures to assert precedence. | precedence order above | `@nnandal` + `Codex` | fixed in PR-01 |
| PR01-D-008 | Should PR-01 include daemon route wiring for `/spec/v2.1/s1/gate-item/evaluate`? | No; PR-01 delivers library-level intake/evaluation contract only. Transport adapter selection is deferred. | Adding partial daemon endpoint in PR-01. | Preserves L3 ownership boundaries and keeps PR-01 focused on intake/evaluation semantics. | Ensure tests focus on pure parser/evaluator behavior with no daemon route dependencies. | none | `@nnandal` + `Codex` | fixed in PR-01 |

## open decisions

| ID | Question | Temporary default | Owner | Due |
|---|---|---|---|---|
| PR01-OD-001 | L2 `gate-item/evaluate` shows both a success shape (with `missing_requirements`) and incomplete-state `E_GATE_ITEM_*` errors; what is canonical transport behavior? | PR-01 canonicalizes evaluator-library output (`missing_requirements` + `blocking_code`) and defers final transport mapping until downstream surface selection. | `@nnandal` + `Codex` | before PR-02 implementation start |
