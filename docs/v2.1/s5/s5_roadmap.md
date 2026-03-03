# Slice S5: Invocation-Centric Review + PR + Merge — PR Roadmap

Current status: **near-complete** (updated 2026-03-03).

S5 canonical flows are shipped (`agent review`, `agent pr sync`, `agent merge`), legacy safety parity landed, and S5 happy/failure e2e suites are wired in CI. One contract-closure gap remains before calling S5 fully complete: `POST /invocations/{ref}/pr/sync` request decoding is still permissive vs the strict JSON contract in `docs/contracts/daemon_api.md` (`all request bodies are strict json`).

### PR-05: PR Sync Strict-Decode Contract Closure
- **goal**: enforce strict request decoding and deterministic error behavior for `POST /invocations/{ref}/pr/sync` so S5 contracts are internally consistent.
- **builds on**: S5 PR-04 merged state.
- **acceptance**:
  - PR sync rejects unknown JSON fields and trailing/multi-object payloads with deterministic typed errors (`E_INVALID_ARGUMENT`) and stable hints.
  - PR sync parses request bodies consistently for known-length and unknown-length/chunked requests; no silent option drops based on `Content-Length`.
  - response correlation remains stable (`request_id` in payload + matching `X-Request-ID` header) across success and failure paths.
  - unit/integration tests cover strict-decode failure matrix and preserve existing `allow_dirty` / `force_with_lease` behavior.
- **non-goals**: no change to branch-scoped PR identity rules, merge flow behavior, or S6 reports-v2 scope.

### Next PRs
- if PR-05 merges cleanly with no new drift, mark S5 complete and continue planning in Slice S6 roadmap.
