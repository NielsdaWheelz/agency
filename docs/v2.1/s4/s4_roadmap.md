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
  - success-path JSON remains strictly backward compatible for existing scripts (additive-only changes; no removals or renames).
  - tests cover success/failure JSON behavior for all mutation commands (`start`, `stop`, `kill`, `land`, `discard`, `chat`, `restart`).
- **non-goals**: no invocation-scoped `agent review`/`agent pr`/`agent merge` behavior (Slice S5); no reports-v2 scope (Slice S6).

### PR-05: runner launch matrix parity for headed/headless `agent start` (planned after PR-04 merges)
- **goal**: make all v2.1 target runners launch reliably from one capability model across headed and headless start flows.
- **builds on**: PR-04.
- **acceptance**:
  - canonical targets (`claude-code`, `codex`, `amp`, `opencode`, `cursor`, `droid`) launch through capability-defined runner plans, not per-call ad hoc branching.
  - compatibility aliases resolve deterministically to canonical runner IDs in metadata and automation-facing outputs.
  - headless `agent start --prompt` applies runner-specific documented non-interactive subcommands/flags/parameters for each target runner.
  - headed/headless launch paths enforce the same reserved-arg conflict policy and deterministic typed unknown-runner errors.
  - tests cover runner identity resolution (including aliases), launch planning across all target runners, and deterministic failure contracts.
- **non-goals**: no semantic-adapter expansion beyond existing raw-log fallback; no additional mutation `--json` surface changes beyond PR-04 parity.
