# S1 Platform Hardening Gates Roadmap - Decisions

Last updated: 2026-02-18
Status: draft

## Decision ledger

| ID | Question | Decision | Rationale | Fallback/Default | Owner | Due |
|---|---|---|---|---|---|---|
| D-001 | Should S1 L3 decompose by issue-file buckets or by L2 contract clusters? | Decompose by L2 contract clusters. | SDLC L3 requires one-cluster-one-owner and contract-first ordering. | none | `@nnandal` + `Codex` | fixed in L3 |
| D-002 | How many merge-safe PRs are needed to cover S1 without ownership overlap? | Use 5 PRs. | Separates intake, item policy, aggregate readiness, change validation, and release operationalization cleanly. | none | `@nnandal` + `Codex` | fixed in L3 |
| D-003 | Should gate-item policy and aggregate readiness live in the same PR? | No, split them across PR-02 and PR-03. | Keeps state-transition policy isolated from aggregate gate/slice evaluation and reduces review coupling. | none | `@nnandal` + `Codex` | fixed in L3 |
| D-004 | Should gate-set change validation share ownership with aggregate readiness? | No, assign change-validation to PR-04. | Both touch drift semantics but represent different mutation domains; split avoids hidden dependency cycles. | none | `@nnandal` + `Codex` | fixed in L3 |
| D-005 | How should the L2 freeze-governance acceptance scenario be represented in L3 coverage? | Own it in PR-05 as release-governance and closure-reporting scope. | Scenario is process-facing and release-facing, not core gate-state mutation logic. | none | `@nnandal` + `Codex` | fixed in L3 |
| D-006 | Should L3 include file paths, function signatures, or test-case details? | No, keep roadmap strictly at decomposition/ownership granularity. | Prevents L4 leakage and keeps PR roadmap implementation-agnostic per SDLC. | none | `@nnandal` + `Codex` | fixed in L3 |
| D-007 | Should runner-capability behavior delivery be owned by this S1 roadmap because it appears in Gate B? | No. S1 L3 owns gate-policy and gate-evaluation behavior; runner-capability behavior may be delivered by downstream capability slices, and S1 evaluates closure evidence once delivered. | Keeps S1 scoped to hardening-gate contract ownership while preserving Gate B closure accountability. | none | `@nnandal` + `Codex` | fixed in L3 |
| D-008 | Should acceptance scenarios allow multiple equal owners? | No. Each scenario has one primary owner PR; supporting PRs are explicitly secondary. | Improves accountability and avoids ownership drift during L4 decomposition. | none | `@nnandal` + `Codex` | fixed in L3 |
| D-009 | Should S1 stop at policy definition, or include release-operational consumption of policy outputs? | Include operational consumption in PR-05. | S1 acceptance is release blocking; policy without release gate enforcement/reporting cannot deterministically gate ship readiness. | none | `@nnandal` + `Codex` | fixed in L3 |
| D-010 | Should temporary S1 namespace be allowed to carry into Slice S2? | No. PR-05 must migrate and delete it before S2 starts. | Prevents temporary-surface debt from becoming long-term architecture. | none | `@nnandal` + `Codex` | fixed in L3/L4 |
| D-011 | Should PR-05 remain directly coupled to markdown issue parsing once release orchestration is introduced? | No. PR-05 should introduce an issue-source boundary so GitHub issues can become canonical post-S1 while preserving S1 compatibility. | Avoids long-term coupling to local markdown artifacts and enables enterprise GH-native governance evolution without breaking S1 contracts. | Keep markdown adapter as compatibility source behind the boundary until GH provider lands. | `@nnandal` + `Codex` | fixed in L3/L4 |
