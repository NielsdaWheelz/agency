# S1 Platform Hardening Gates Spec - Decisions

Last updated: 2026-02-16
Status: frozen

## Decision Ledger

| ID | Question | Decision | Rationale | Fallback/Default | Owner | Due |
|---|---|---|---|---|---|---|
| D-001 | What does "closed with tests" mean for S1 gate items? | Closure requires all issue acceptance items complete, required automated tests passing, and non-empty evidence refs. | L1 acceptance and binding/testing standards require enforceable, test-backed closure. | none | `@nnandal` + `Codex` | fixed in L2 |
| D-002 | How should Gate A/B sets be sourced to avoid drift? | Gate sets are exactly those in `release-gates.md` sections A/B; drift against `issue-map.md` is treated as contract error. | Keeps one canonical source while enforcing consistency checks. | none | `@nnandal` + `Codex` | fixed in L2 |
| D-003 | Should S1 define a machine-readable evaluation contract? | Yes. Define normative gate-evaluation request/response/error shapes independent of implementation surface. | Enables deterministic L3/L4 execution and CI/CLI tooling without binding to one mechanism. | none | `@nnandal` + `Codex` | fixed in L2 |
| D-004 | Are design-type gate issues allowed to close without runtime changes? | Yes, when closure includes enforceable contract evidence (tests/lint/CI checks). | Preserves contract-first work while preventing unenforced paper specs. | none | `@nnandal` + `Codex` | fixed in L2 |
| D-005 | Is GH e2e mandatory for every gate item? | No. GH e2e is mandatory only for gate items marked `requires_gh_e2e=true`. | Keeps evidence proportional to blast radius while protecting GH mutation flows. | none | `@nnandal` + `Codex` | fixed in L2 |
| D-006 | How should closure evidence be represented for machine-checkable gate evaluation? | Require a closure evidence block with implementation refs, targeted test refs, and suite-test refs for each closed gate item. | Current issue stubs have acceptance checklists but no normalized closure evidence schema. | none | `@nnandal` + `Codex` | fixed in L2 |
| D-007 | How do we prevent release-gate list drift across docs? | Treat `release-gates.md` as canonical membership and require synchronized `issue-map.md` updates in the same change set. | Prevents ambiguous gate scope and inconsistent execution tracking. | none | `@nnandal` + `Codex` | fixed in L2 |
| D-008 | Does gate-item reordering require explicit approval? | Reorder-only changes do not require explicit approver when gate membership is unchanged. | Reordering is non-semantic and should not add process friction. | none | `@nnandal` + `Codex` | fixed in L2 |
| D-009 | Who can close Gate A (`p0`) items? | Require maintainer role for `ready_for_verification -> closed` on Gate A items. | Gate A is release-blocking safety scope and maintainer owns release/security decisions. | none | `@nnandal` + `Codex` | fixed in L2 |
| D-010 | What is required to reopen a previously closed gate item? | Reopen requires explicit regression reason and supporting evidence reference. | Prevents silent churn and preserves auditable gate history. | none | `@nnandal` + `Codex` | fixed in L2 |
| D-011 | Can contributors self-close Gate B (`p1`) items? | No. Gate B closure requires `reviewer` or `maintainer` role. | Release-critical hardening needs reviewer-level signoff even without full subsystem ownership map. | none | `@nnandal` + `Codex` | fixed in L2 |
| D-012 | When can S1 L2 be frozen? | Freeze is blocked until unresolved-default table is empty. | SDLC requires unresolved/default section to be empty before freeze handoff. | none | `@nnandal` + `Codex` | fixed in L2 |
| D-013 | How are gate-set reorder targets represented? | `reorder` uses `issue_paths` (`>=2`), while `add/remove/replace` use `issue_path`. | Removes ambiguity in change validation semantics and supports deterministic parsing. | none | `@nnandal` + `Codex` | fixed in L2 |
| D-014 | Where must closure evidence live for closed gate items? | Closed gate items must include a `## closure evidence` section with required keys. | Makes gate evaluation machine-checkable and auditable from issue stubs alone. | none | `@nnandal` + `Codex` | fixed in L2 |
