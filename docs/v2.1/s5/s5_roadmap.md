# Slice S5: Invocation-Centric Review + PR + Merge — PR Roadmap

Current status: **complete** (updated 2026-03-03).

S5 canonical flows are shipped (`agent review`, `agent pr sync`, `agent merge`), legacy safety parity landed, and S5 happy/failure e2e suites are wired in CI. `POST /invocations/{ref}/pr/sync` now enforces strict JSON request decoding (`unknown` fields and trailing/multi-object payloads reject with deterministic `E_INVALID_ARGUMENT`), aligning with `docs/contracts/daemon_api.md`.

### PR-05: PR Sync Strict-Decode Contract Closure
- **goal**: enforce strict request decoding and deterministic error behavior for `POST /invocations/{ref}/pr/sync` so S5 contracts are internally consistent.
- **builds on**: S5 PR-04 merged state.
- **acceptance** (completed):
  - PR sync rejects unknown JSON fields and trailing/multi-object payloads with deterministic typed errors (`E_INVALID_ARGUMENT`) and stable hints.
  - strict-decode normalization is scoped to JSON body parsing; missing required query params (including `repo_id`) remain `E_INVALID_REQUEST` in this PR for compatibility.
  - PR sync parses request bodies consistently for known-length and unknown-length/chunked requests; no silent option drops based on `Content-Length`.
  - response correlation remains stable (`request_id` in payload + matching `X-Request-ID` header) across success and failure paths.
  - unit/integration tests cover strict-decode failure matrix and preserve existing `allow_dirty` / `force_with_lease` behavior.
- **non-goals**: no change to branch-scoped PR identity rules, merge flow behavior, or S6 reports-v2 scope.

### Next PRs
- continue planning and execution in Slice S6 roadmap.
