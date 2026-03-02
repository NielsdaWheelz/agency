# Slice S4: Runner Capability Model + Agent Mutation JSON — PR Roadmap

### PR-01: runner capability engine + launch-contract convergence
- **goal**: replace hardcoded runner-name branching with a shared capability model that drives validation, canonical identity resolution, and launch behavior across start/restart control-plane paths.
- **builds on**: Slice S3 merged state.
- **acceptance**:
  - `agent` start/control-plane/restart accepts the v2.1 runner target set (`claude-code`, `codex`, `amp`, `opencode`, `cursor-cli`, `droid`) via capability-driven validation, not hardcoded allowlists.
  - `claude` remains accepted as a compatibility alias, but canonical runner identity is emitted/stored as `claude-code`.
  - unknown runner ids fail with deterministic typed errors suitable for scripts.
  - runner argument shaping and reserved-flag conflicts are capability-defined and consistent across start and restart flows.
  - runners without semantic adapters execute successfully with explicit raw-log-first fallback semantics (no fake semantic guarantees).
  - runtime environment assembly for headless start/restart follows one deterministic merge rule (stable precedence, no duplicate-key ambiguity).
- **non-goals**: no mutation `--json` parity rollout for `agent start|stop|kill|land|discard`; no stream-ingestion hardening changes beyond what is strictly required for capability fallback behavior.

### PR-02: stream durability hardening + agent mutation json parity (planned after PR-01 merges)
- **goal**: make stream ingestion contract-safe under failure/volume and complete stable `--json` outputs for invocation mutation commands.
- **builds on**: PR-01.
- **acceptance**:
  - stream ingestion enforces bounded line processing and avoids unbounded memory growth on oversized runner output.
  - raw/normalized stream write failures are surfaced deterministically (not silently dropped), with operation outcomes/attention signaling aligned to contract expectations.
  - normalized stream event sequencing remains monotonic and durable across restart/append boundaries.
  - `agent start`, `agent stop`, `agent kill`, `agent land`, and `agent discard` support stable machine-readable `--json` responses with deterministic success/error fields.
  - existing S3 mutation JSON surfaces (`agent chat`, `agent restart`) remain compatible with the S4 mutation-envelope expectations.
- **non-goals**: no new invocation-scoped review/PR/merge command family behavior (Slice S5); no reports-v2 ergonomics scope (Slice S6).
