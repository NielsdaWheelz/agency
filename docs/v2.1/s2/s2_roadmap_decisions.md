# S2 Daemon Read Convergence + Sandbox Navigation Roadmap - Decisions

Last updated: 2026-02-25
Status: draft

## Decision ledger

| ID | Question | Decision | Rationale | Fallback/Default | Owner | Due |
|---|---|---|---|---|---|---|
| D-001 | Should S2 L3 isolate the shared CLI navigation kernel (read-routing lifecycle, selection lifecycle, shared daemon-first resolution contract) in its own PR before command-family migrations, or bundle it into the first command migration PR? | Isolate the shared CLI navigation kernel as its own PR (PR-02) before worktree/agent command-family migrations. | Prevents ownership overlap and hidden dependencies between `worktree` and `agent` convergence PRs, keeps shared invariants reviewable in one place, and supports a clean acyclic DAG. | none | `@nnandal` + `Codex` | fixed in L3 |
