# Slice S4: Runner Capability Model + Agent Mutation JSON — PR Roadmap

### PR-03: bounded stream-ingestion hardening for oversized runner lines
- **goal**: close the remaining stream-ingestion safety gap by enforcing a true per-line hard cap before full allocation during parsing.
- **builds on**: PR-02 merged state.
- **acceptance**:
  - stream ingestion no longer relies on unbounded full-line reads for newline-delimited output.
  - oversized lines are handled deterministically (raw log preserved, bounded parse/error signaling emitted) without unbounded memory growth risk.
  - subsequent valid lines continue to parse/write normally after oversized-line handling.
  - monotonic normalized-event sequencing remains intact across start/restart append flows after the reader change.
- **non-goals**: no runner capability target/matrix changes; no mutation-command JSON envelope changes.

### PR-04: mutation `--json` parity completion for `agent chat` + `agent restart` (planned after PR-03 merges)
- **goal**: finish mutation-contract parity so `agent chat --json` and `agent restart --json` are deterministic machine-readable contracts on both success and failure.
- **builds on**: PR-03.
- **acceptance**:
  - `agent chat --json` and `agent restart --json` return structured JSON for validation failures, daemon-declared failures, and transport failures.
  - automation does not need to parse human-formatted stderr/stdout to classify failure outcomes for these commands.
  - success-path JSON remains backward compatible for existing scripts (or ships with explicit, documented transition rules).
  - tests cover success/failure JSON behavior for all mutation commands (`start`, `stop`, `kill`, `land`, `discard`, `chat`, `restart`).
- **non-goals**: no invocation-scoped `agent review`/`agent pr`/`agent merge` behavior (Slice S5); no reports-v2 scope (Slice S6).
