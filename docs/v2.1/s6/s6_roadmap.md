# Slice S6: Reports v2 + CLI Ergonomics Cleanup — PR Roadmap

Last updated: 2026-03-03
Status: active
Upstream spec: `docs/v2.1/s6/s6_spec.md`

Current state: PR-01 and PR-02 are merged. One closure pass remains to finish spec-level ergonomics parity and acceptance-test coverage.

### PR-01: reports v2 canonical model + mode-aware progression contract
- **goal**: deliver one deterministic reports-v2 contract for review/PR/merge progression across JSON and markdown artifacts.
- **builds on**: S5 complete baseline (`agent review`, `agent pr sync`, `agent merge` canonical flows merged).
- **acceptance**:
  - report resolution uses one canonical model from JSON + markdown inputs; `report.json` is authoritative when present and cross-format conflicts are surfaced deterministically.
  - equivalent report content serializes to stable machine-readable fields regardless of source format.
  - headless progression paths (`agent review`, `agent pr sync`, `agent merge`) enforce strict machine-parseable validation with typed deterministic failures for missing, malformed, oversized, or schema-incompatible report inputs.
  - markdown-only repositories remain backward compatible through deterministic markdown-to-model mapping; migration to JSON is optional.
  - headed/compatibility report consumers remain progression-capable via deterministic fallback behavior with explicit diagnostics instead of brittle parse assumptions.
  - fallback PR-body generation and report ingestion remain bounded under large repository inputs with explicit stable truncation/fallback signals.
  - strict-vs-compatibility behavior is contract-documented and covered by deterministic success/failure tests.
- **non-goals**: no broad CLI flag alias normalization; no command-family redesign.

### PR-02: CLI ergonomics normalization for `--yes`, high-traffic flags, and open-on-create
- **goal**: standardize script-safe confirmation and high-frequency flag ergonomics across canonical and compatibility command paths.
- **builds on**: PR-01 merged report contracts.
- **acceptance**:
  - destructive/irreversible confirmation flows (including `agent merge`, `clean`, `resume --restart`, and `worktree rm`) use one non-interactive confirmation contract: without `--yes`, deterministic confirmation-required failure; with `--yes`, deterministic non-interactive progression.
  - high-traffic lifecycle/navigation/progression commands use consistent canonical long-flag semantics and predictable short aliases for repo selection, JSON output, confirmation, and open-on-create/navigation behavior (`-r/--repo`, `-j/--json`, `-y/--yes`, `-o/--open`).
  - open-on-create behavior is available for canonical creation flows and compatibility run/create flows with deterministic outcomes in interactive and scriptable contexts.
  - legacy spellings remain additive compatibility aliases and do not redefine command meaning.
  - command help and automated coverage assert that human-facing and automation-facing flag/confirmation behavior stay aligned.
- **non-goals**: no removal of legacy aliases, no full command taxonomy rewrite, no GUI/full-screen TUI scope.

### PR-03: S6 contract closure for remaining short-alias parity + open-on-create test guarantees (planned after PR-02 merges)
- **goal**: close remaining S6 ergonomics gaps so progression/navigation alias behavior and open-on-create contracts are fully spec-aligned and test asserted.
- **builds on**: PR-02 merged.
- **acceptance**:
  - core invocation progression surfaces with repo/json flags (including `agent review`) expose additive canonical short aliases (`-r/--repo`, `-j/--json`) without changing existing long-flag semantics.
  - high-traffic compatibility navigation aliases (`path`, `open`, `attach`) accept additive `-r/--repo` for parity with canonical invocation navigation flows.
  - `worktree create --open` and `run --open` behaviors are test-asserted for deterministic open-first behavior and explicit `open_status` signaling on editor-launch failure after successful creation.
  - command help coverage asserts alias visibility for covered commands, and confirmation behavior coverage remains deterministic for non-interactive flows requiring `--yes`.
- **non-goals**: no new command families, no legacy alias removals, no report-contract redesign beyond existing PR-01 semantics.
