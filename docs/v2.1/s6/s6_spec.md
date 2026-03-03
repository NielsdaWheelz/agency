# Slice S6: Reports v2 + CLI Ergonomics Cleanup — Spec

## Goal

Reduce friction in report and confirmation/flag ergonomics.

## Acceptance Criteria

### reports v2 supports json + markdown compatibility with deterministic precedence
- **given**: a workflow consumes report content for review/PR progression
- **when**: report artifacts are evaluated
- **then**: the system resolves one canonical report model from JSON and markdown sources; `report.json` is authoritative when present, markdown is deterministic compatibility input, and cross-format conflicts are handled deterministically with explicit diagnostics

### headless report contract is machine-parseable and strict
- **given**: an invocation runs in headless mode and report data is consumed by automation surfaces
- **when**: report state is read for `agent review`, `agent pr sync`, or `agent merge` progression
- **then**: the canonical report model is deterministic and machine-parseable, strict validation is enforced, and malformed/ambiguous input fails with typed deterministic errors

### markdown-only workflows remain backward compatible
- **given**: a repository has only markdown report content (no JSON report artifact)
- **when**: report-aware flows execute
- **then**: behavior remains backward compatible via deterministic markdown-to-model mapping without requiring report migration

### headed report behavior is best-effort and compatibility-oriented
- **given**: an invocation runs in headed mode or a compatibility command path consumes report data
- **when**: report input is incomplete, malformed, or absent
- **then**: progression remains available through deterministic compatibility fallback behavior with explicit diagnostics instead of brittle parse assumptions

### strictness is mode-aware and explicit
- **given**: report evaluation runs in headless strict mode (`agent review`/`agent pr sync`/`agent merge`) or headed compatibility mode
- **when**: report input is missing, malformed, oversized, or schema-incompatible
- **then**: headless strict mode returns typed deterministic errors; headed compatibility mode preserves progression with deterministic fallback behavior and explicit warning/diagnostic signals

### malformed and oversized report inputs fail deterministically
- **given**: report inputs exceed contract limits or cannot be parsed
- **when**: the input is processed
- **then**: outcomes use stable typed error codes/messages (or stable compatibility fallback signals by mode), and behavior does not depend on incidental parser/runtime differences

### serialization is deterministic across report formats
- **given**: equivalent report data in JSON and markdown forms
- **when**: the system reads or emits report-facing machine contracts
- **then**: normalized field semantics and serialization are deterministic, stable for scripts, and additive/backward-compatible for existing automation

### fallback PR body generation is bounded and deterministic
- **given**: fallback PR body generation is needed and commit/file history is large
- **when**: fallback content is assembled
- **then**: reads and generated sections are bounded by contract limits, truncation signaling is explicit and stable, and no path performs unbounded in-memory ingestion

### non-interactive confirmation policy is standardized with `--yes`
- **given**: a command path requires destructive/irreversible confirmation (including `agent merge`, `clean`, `resume --restart`, `worktree rm`, and other explicitly-confirmed compatibility flows)
- **when**: it runs in a non-interactive context without `--yes`
- **then**: the command fails with deterministic confirmation-required behavior and hints; with `--yes`, it proceeds without ad hoc tty scraping

### high-traffic flag ergonomics are normalized
- **given**: users run the most frequent lifecycle/navigation/progression commands
- **when**: they use common flags (repo selection, json output, confirmation, open-on-create/navigation)
- **then**: semantics are consistent across the command family, canonical long names are stable, and standard short aliases are predictable (`-r/--repo`, `-j/--json`, `-y/--yes`, `-o/--open`) without changing command meaning

### open-on-create ergonomics are available for creation flows
- **given**: a user creates a new working context (`worktree create` and compatibility run/create flows) and wants immediate editor entry
- **when**: they request open-on-create behavior
- **then**: the command opens the created target directly after successful creation with deterministic behavior in both interactive and scriptable contexts

## Key Decisions

**Report engine is a canonical normalized model, not format-specific parsing logic**: S6 defines one report domain model consumed by review/PR/merge progression flows. JSON and markdown are adapters into that model, preventing behavior drift between artifact formats.

**`report.json` is authoritative when present**: when both JSON and markdown artifacts exist, JSON precedence is deterministic; markdown remains compatibility input and cannot silently override canonical JSON fields.

**Headless is strict, headed is compatibility-first**: headless report consumption for `agent review`, `agent pr sync`, and `agent merge` is the reliability contract for automation and must be machine-parseable with fail-closed validation. Headed/compatibility paths remain best-effort and fallback-friendly.

**Reports v2 stay lightweight while preserving deterministic contracts**: minimum required structured signal is concise and stable for automation, while broader narrative/testing detail remains optional metadata.

**Bounded input/output is a product invariant for report and fallback paths**: size limits and truncation rules are part of the contract surface, not implementation detail, to keep automation safe under repository-scale inputs.

**`--yes` is the canonical non-interactive confirmation primitive**: confirmation semantics are unified across applicable canonical and compatibility command paths (including merge/clean/restart/remove confirmation paths) so automation has one script-safe contract instead of command-specific prompt behavior.

**Ergonomics normalization is additive and compatibility-preserving**: canonical high-traffic flag conventions (including `-r`, `-j`, `-y`, `-o`) become the documented default while legacy spellings/paths may remain as compatibility aliases that must not redefine behavior.

## Out of Scope

- Removing markdown report compatibility in S6 (JSON-only reporting is not required)
- Expanding reports into mandatory release-gate evidence enforcement beyond S6 progression ergonomics
- Re-architecting S5 invocation review/PR/merge core semantics beyond report/ergonomics contract updates
- Full command-family redesign or broad alias removal/migration breakage
- GUI/full-screen TUI workflow expansion
