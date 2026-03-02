# Slice S4: Runner Capability Model + Agent Mutation JSON — Spec

## Goal

Remove hard-coded runner assumptions and normalize automation outputs.

## Acceptance Criteria

### capability-driven runner selection replaces allowlists
- **given**: a user starts an invocation through canonical `agent` start/control-plane surfaces with one of the v2.1 runner targets (`claude-code`, `codex`, `amp`, `opencode`, `cursor`, `droid`)
- **when**: runner validation and launch planning are evaluated
- **then**: acceptance/rejection is driven by a shared runner capability contract (not hardcoded runner-name conditionals), and accepted requests resolve to one canonical runner identity

### legacy runner naming remains compatible while canonical identity is stable
- **given**: existing automation or config still sends `claude`
- **when**: the request is resolved by runner identity logic
- **then**: the request is accepted as a compatibility alias to canonical `claude-code`, and downstream metadata/JSON surfaces use canonical runner identity deterministically

### unknown runner ids fail deterministically
- **given**: a start/restart/control-plane request references a runner outside the canonical target set
- **when**: the request reaches validation
- **then**: the command fails with a typed, deterministic unknown-runner error contract suitable for scripts

### runner launch behavior is capability-scoped, not per-call ad hoc
- **given**: a runner request includes user-supplied runner args and standard invocation context
- **when**: command argv and reserved-flag validation are computed
- **then**: behavior follows one capability-defined policy for that runner (required base args, reserved-flag conflicts, launch mode support), consistently across start and restart paths

### semantic-parser fallback is explicit and non-breaking
- **given**: a runner without semantic stream adapter support is used
- **when**: the invocation runs headless and logs are consumed
- **then**: invocation lifecycle, transcript/log reads, and automation contracts still work via explicit raw-log-first fallback behavior, without pretending semantic events exist

### stream ingestion stays bounded and failure-visible
- **given**: runner output includes malformed/oversized/high-volume lines or encounters log-write failures
- **when**: output ingestion and normalized-event writing run
- **then**: memory usage is bounded by contract limits, write failures are surfaced as operation failures/attention signals (not silently dropped), and sequence ordering remains monotonic for persisted stream events

### environment merge is deterministic across runner flows
- **given**: base process environment, required non-interactive defaults, and user overrides are all present
- **when**: runtime environment is assembled for headless start/restart
- **then**: merge precedence is deterministic, duplicate keys are eliminated, and output ordering is stable for reproducible behavior and tests

### agent mutation commands expose stable json contracts
- **given**: a user runs invocation mutation commands with `--json` (`agent start`, `agent stop`, `agent kill`, `agent land`, `agent discard`, plus existing `agent chat`/`agent restart`)
- **when**: the command succeeds or fails
- **then**: each command returns a stable machine-readable response shape with deterministic success/error fields and no dependence on human-formatted text parsing

## Key Decisions

**Canonical runner identity uses v2.1 target IDs with explicit compatibility aliases**: canonical IDs are `claude-code`, `codex`, `amp`, `opencode`, `cursor`, and `droid`. Legacy `claude` remains a compatibility input alias to `claude-code` during v2.1 migration.

**Runner capabilities are a single source of truth across CLI + daemon**: validation, supported modes, reserved-flag policy, launch argument shaping, and semantic-adapter availability derive from one shared capability model to prevent drift between start/control-plane/restart paths.

**Raw-log-first fallback is the contract for runners without semantic adapters**: missing semantic parsing support must not block lifecycle or detached operation. The system must surface that semantic enrichment is unavailable while preserving invocation continuity and scriptable reads.

**Mutation JSON is a first-class compatibility contract**: mutation commands under canonical `agent` surfaces must return stable machine-readable envelopes in `--json` mode. Human-readable output remains a rendering layer, not the automation contract.

**Deterministic runtime environment is a hard invariant**: environment merge behavior (including non-interactive safety defaults and user overrides) is standardized to one deterministic precedence/order rule across runner flows.

**Stream durability and bounded ingestion are parity-critical**: normalized-event sequencing and log writes are contract data, not best-effort telemetry. Silent drops and unbounded ingestion are treated as correctness failures.

## Out of Scope

- Invocation-scoped `agent review` / `agent pr ...` / `agent merge` command family and workflow contracts (-> Slice S5)
- Reports v2 artifact/strictness transition and broader CLI ergonomics cleanup (-> Slice S6)
- Full semantic parser parity for every target runner beyond S4 fallback guarantees (incremental post-S4 hardening)
- GUI/full-screen TUI parity work
