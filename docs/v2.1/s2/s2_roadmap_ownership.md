# S2 Daemon Read Convergence + Sandbox Navigation Roadmap - Ownership Ledger

Last updated: 2026-02-25
Status: draft

## Ownership matrix detail

| Cluster | Contract cluster | Owning PR | Boundary guard |
|---|---|---|---|
| C1 | Daemon read API contract alignment for S2 read surfaces (envelope, list/get endpoint error semantics, strict list-filter validation, pagination bounds/cursors) | PR-01 | PR-01 owns daemon read contract hardening only; no CLI command-surface migration or alias rollout behavior. |
| C2 | Shared CLI read-routing + navigation selection/resolution kernel (routing lifecycle, fallback boundary, ambiguity/daemon-unavailable/TTY preflight semantics) | PR-02 | PR-02 owns shared cross-surface resolution behavior only; no command-family-specific canonical/compatibility UX rollout. |
| C3 | `worktree` command-family convergence onto daemon-first reads/navigation (`ls/show/path/open/shell`) | PR-03 | PR-03 owns worktree surface adoption only; it must consume PR-02 shared kernel semantics and must not redefine them. |
| C4 | Canonical `agent` read + invocation navigation convergence (`ls/show/path/open/shell/enter`) | PR-04 | PR-04 owns canonical agent surface adoption only; no compatibility alias rollout and no canonical `agent restart` semantics. |
| C5 | Compatibility adapters + command-policy enforcement (`agent attach`, legacy top-level `path/open/attach/resume`, compatibility dispatch semantics, deprecation-safe deterministic behavior) | PR-05 | PR-05 owns compatibility routing/policy enforcement only; no new canonical behavior or checkpoint-aware restart semantics. |

## Coordination rules

1. Any shared navigation behavior discovered during PR-03 or PR-04 implementation that applies across command families must be reassigned to the PR-02 contract boundary rather than duplicated.
2. PR-05 may preserve documented compatibility dispatch behavior, but any target-resolution step must route through canonical/shared daemon-first resolution; it must not become a backdoor for local-store target re-resolution or S3 restart semantics.
3. PR-01 owns daemon-side enum validation and list-filter fail-closed semantics; downstream PRs may improve CLI UX messaging but may not weaken daemon contract strictness.
4. Cross-surface navigation-selection scriptability invariants (the S2 `select` acceptance via deterministic list-row/script-driven target identity) are owned by PR-02 at the contract level and implemented/consumed by PR-03 and PR-04 without changing identifier semantics.

## Residual coordination risks

- Compatibility `resume` behavior can drift toward canonical restart semantics; PR-05 must keep restart behavior explicitly compatibility-scoped and non-checkpoint-aware.
- Scriptability acceptance spans daemon list contracts and both command families; primary ownership is explicit in the roadmap coverage map to avoid blame shifting during L4 planning.
- PR-03 and PR-04 are parallelizable at the contract level after PR-02, but both should avoid broad shared refactors that blur the PR-02 boundary.
