# Slice S5: Invocation-Centric Review + PR + Merge — PR Roadmap

Current status: canonical daemon-backed `agent review`, `agent pr sync`, and `agent merge` flows are implemented; PR-01 and PR-02 behavior is largely present in `main`.

### PR-03: compatibility-path safety parity (`push`/`merge` hardening)
- **goal**: close remaining S5 safety debt in legacy compatibility commands so non-canonical paths cannot violate S5 invariants.
- **builds on**: merged PR-02 state.
- **acceptance**:
  - legacy `agency push` report/body handling is bounded (no unbounded report reads or fallback generation from unbounded git output).
  - legacy `agency merge` surfaces merge-log write failures as typed errors and persists merge logs with private permissions.
  - legacy merge verify environment uses correct root semantics (`AGENCY_REPO_ROOT` = repository root, `AGENCY_WORKSPACE_ROOT` = integration worktree path).
  - targeted unit/integration tests cover oversized-input handling and merge-log persistence failure behavior in compatibility flows.
- **non-goals**: no removal of legacy `push`/`merge` commands; no reports-v2 migration (Slice S6).

### PR-04: contract closure + failure-path e2e matrix (planned after PR-03 merges)
- **goal**: complete S5 contract/test closure for automation-safe invocation review -> PR -> merge workflows.
- **builds on**: PR-03.
- **acceptance**:
  - `docs/contracts/daemon_api.md` explicitly documents invocation review/PR-sync/merge endpoints and deterministic error envelopes.
  - invocation-scoped mutation responses include daemon-issued `request_id` correlation alongside typed `error_code`/`message`/`hint` contracts; `client_request_id` remains reserved for idempotency semantics.
  - e2e coverage expands beyond happy path to assert key failures: not-ready invocation, missing/closed PR, mergeability failure, confirmation failure, bounded-input handling, and merge-log persistence failure.
  - CI/docs wiring for S5 e2e clearly distinguishes happy-path vs failure-matrix runs.
- **non-goals**: no new product surface beyond S5 command family; no merge queue/TUI/GUI expansion.
