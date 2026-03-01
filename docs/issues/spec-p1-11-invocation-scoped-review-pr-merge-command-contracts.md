# [p1][spec][design] invocation-scoped review/pr/merge command contracts

labels: `p1`, `type:design`, `area:spec`

## summary
define explicit command and contract behavior for invocation-scoped
`agent review`, `agent pr ...`, and `agent merge` workflows.

## context
- section: v2.1 parity baseline
- source: docs/v2.1/constitution.md
- details:
  - v2.1 requires deterministic invocation-centric PR/review/merge operations.
  - current docs list target commands but do not yet define shared response/error contracts.
  - parity depends on scriptable JSON outputs and policy-aware merge readiness behavior.

## acceptance criteria
- [ ] define CLI command set and required flags for `agent review`, `agent pr`, `agent merge`
- [ ] define JSON output schema and typed error code behavior for each command
- [ ] define daemon API/DTO expectations for invocation-scoped review/pr/merge operations
- [ ] add tests covering review -> pr -> merge happy path and key failure modes
