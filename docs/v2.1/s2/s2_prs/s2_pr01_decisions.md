# S2 PR-01 Daemon Read API Contract Hardening - Decisions

Last updated: 2026-02-25
Status: draft

## decision ledger

| ID | Problem | Decision | Rejected alternatives | Invariant impact | Test impact | Fallback/Default | Owner | Due |
|---|---|---|---|---|---|---|---|---|
| PR01-D-001 | Where should strict `state`/`mode` validation run for daemon list endpoints? | Validate at the `/worktrees` and `/invocations` list-handler boundary before repo enumeration/filter evaluation. | Leaving matcher helpers permissive; validating only after scans complete; CLI-only validation. | Enforces fail-closed semantics and prevents hidden result widening/extra IO. | Add handler tests for invalid `state`/`mode` returning `400 E_INVALID_ARGUMENT` with structured details. | none | `@nnandal` + `Codex` | fixed in PR-01 |
| PR01-D-002 | Should PR-01 expand strict validation to non-enum list params (`limit`, `cursor`, `repo_id`, `worktree_ref`)? | No. PR-01 only hardens L2/L3-owned enum strictness (`state`, `mode`) and preserves current behavior for other params. | Introducing new rejection semantics for `limit`/`cursor`; broad query validation framework in PR-01. | Preserves PR-01 scope purity and avoids unplanned contract changes. | Add regression coverage for current pagination stability (`default=100`, `max accepted=500`, cursor continuity). | Preserve current handling exactly. | `@nnandal` + `Codex` | fixed in PR-01 |
| PR01-D-003 | What details shape should daemon return for invalid list-filter enum inputs? | Return structured details with exact fields `param`, `value`, `allowed_values` (L2 `InvalidQueryArgumentDetails`). | Free-form message-only errors; endpoint-specific detail keys. | Supports machine-safe CLI/script handling and L2 contract consistency. | Add exact detail-field assertions in invalid-filter handler tests. | none | `@nnandal` + `Codex` | fixed in PR-01 |
| PR01-D-004 | How should PR-01 handle existing undocumented `worktree_id` support on `GET /invocations` while S2 canonically documents `worktree_ref`? | Preserve `worktree_id` as compatibility-only behavior in PR-01; keep `worktree_ref` canonical and lock deterministic precedence (`worktree_ref` over `worktree_id`) with regression tests. | Removing `worktree_id` in a hardening PR; newly documenting `worktree_id` as canonical S2 contract. | Preserves deterministic filtering while avoiding accidental canonical API-surface expansion or compatibility breakage. | Add compatibility and precedence tests for `worktree_id` + `worktree_ref`. | Future deprecation/removal requires an explicitly owned compatibility/API cleanup PR. | `@nnandal` + `Codex` | fixed in PR-01 |
| PR01-D-005 | When both `/invocations` enum filters are invalid, which invalid param should be surfaced in the single `E_INVALID_ARGUMENT` response? | Validate in deterministic order `state` then `mode`; first invalid wins. | Returning `mode` first; nondeterministic map/order-driven behavior; multi-error reporting in PR-01. | Preserves fail-closed behavior while keeping L2 singular invalid-detail schema deterministic. | Add combined-invalid-filter test asserting `details.param == "state"` and state allowed-values list. | If richer validation is needed later, add a versioned multi-error details schema in a dedicated contract change. | `@nnandal` + `Codex` | fixed in PR-01 |

## open decisions

| ID | Question | Temporary default | Owner | Due |
|---|---|---|---|---|
