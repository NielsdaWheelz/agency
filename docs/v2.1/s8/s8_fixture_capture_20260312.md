# Slice S8: Fixture Capture Corpus - 2026-03-12 and 2026-03-18

This note records the real fixture corpus used for Slice S8 parser and projection validation. It exists so future changes rely on preserved runner evidence, not memory.

## Capture Summary

- Capture dates:
  - 2026-03-12: direct-runner baseline (`D05`, `D06`, cursor `D07`)
  - 2026-03-18: corpus closure for direct `D01`-`D04` and agency-managed `A01`-`A04`
- Temporary run roots:
  - `/tmp/agency-s8-fixtures/runs/20260312T184346Z`
  - `/tmp/agency-s8-fixtures-20260318`
- Durable fixture path: `internal/daemon/stream/testdata/s8_20260312`
- Modes captured:
  - direct runner capture
  - agency-managed headless capture
- Closure status: complete for the S8 gate in `docs/v2.1/s8/s8_roadmap.md`

## Coverage Snapshot

- Direct scenarios (`D01`-`D06`) captured across Claude, Codex, Cursor.
- Supplemental direct scenario (`D07`) captured for Cursor tool-family expansion.
- Agency-managed scenarios (`A01`-`A04`) captured across Claude, Codex, Cursor.
- No remaining fixture gaps for the S8 closure gate.

## Runner Versions

- Claude: `2.1.72 (Claude Code)`
- Codex: `codex-cli 0.114.0`
- Cursor agent: `2026.02.27-e7d2ef6`

## Imported Fixture Artifacts

Direct captures:

- `internal/daemon/stream/testdata/s8_20260312/claude_d01_assistant_only.jsonl`
- `internal/daemon/stream/testdata/s8_20260312/claude_d02_read_search_no_edit.jsonl`
- `internal/daemon/stream/testdata/s8_20260312/claude_d03_command_long_output.jsonl`
- `internal/daemon/stream/testdata/s8_20260312/claude_d04_single_edit.jsonl`
- `internal/daemon/stream/testdata/s8_20260312/claude_d05_success.jsonl`
- `internal/daemon/stream/testdata/s8_20260312/claude_d06_failure.jsonl`
- `internal/daemon/stream/testdata/s8_20260312/codex_d01_assistant_only.jsonl`
- `internal/daemon/stream/testdata/s8_20260312/codex_d02_read_search_no_edit.jsonl`
- `internal/daemon/stream/testdata/s8_20260312/codex_d03_command_long_output.jsonl`
- `internal/daemon/stream/testdata/s8_20260312/codex_d04_single_edit.jsonl`
- `internal/daemon/stream/testdata/s8_20260312/codex_d05_success.jsonl`
- `internal/daemon/stream/testdata/s8_20260312/codex_d06_failure.jsonl`
- `internal/daemon/stream/testdata/s8_20260312/cursor_d01_assistant_only.jsonl`
- `internal/daemon/stream/testdata/s8_20260312/cursor_d02_read_search_no_edit.jsonl`
- `internal/daemon/stream/testdata/s8_20260312/cursor_d03_command_long_output.jsonl`
- `internal/daemon/stream/testdata/s8_20260312/cursor_d04_single_edit.jsonl`
- `internal/daemon/stream/testdata/s8_20260312/cursor_d05_success.jsonl`
- `internal/daemon/stream/testdata/s8_20260312/cursor_d06_failure.jsonl`
- `internal/daemon/stream/testdata/s8_20260312/cursor_d07_tool_family_coverage.jsonl`

Agency-managed captures:

- `internal/daemon/stream/testdata/s8_20260312/agency_claude_a01_seed/`
- `internal/daemon/stream/testdata/s8_20260312/agency_claude_a02_followup/`
- `internal/daemon/stream/testdata/s8_20260312/agency_claude_a03_turn_restart/`
- `internal/daemon/stream/testdata/s8_20260312/agency_claude_a04_retention/`
- `internal/daemon/stream/testdata/s8_20260312/agency_codex_a01_seed/`
- `internal/daemon/stream/testdata/s8_20260312/agency_codex_a02_followup/`
- `internal/daemon/stream/testdata/s8_20260312/agency_codex_a03_turn_restart/`
- `internal/daemon/stream/testdata/s8_20260312/agency_codex_a04_retention/`
- `internal/daemon/stream/testdata/s8_20260312/agency_cursor_a01_seed/`
- `internal/daemon/stream/testdata/s8_20260312/agency_cursor_a02_followup/`
- `internal/daemon/stream/testdata/s8_20260312/agency_cursor_a03_turn_restart/`
- `internal/daemon/stream/testdata/s8_20260312/agency_cursor_a04_retention/`

## Environment Notes

- Claude direct CLI required `--verbose` together with `-p --output-format stream-json`.
- Codex direct capture inside the sandbox failed because the sandboxed process could not resolve or reach the Codex API endpoint; successful fixtures were captured outside the sandbox.
- Cursor direct capture inside the sandbox failed with `SecItemCopyMatching failed -50`; successful fixtures were captured outside the sandbox.
- Claude headless agency runs emitted `final` but occasionally did not terminate promptly; capture closure used an explicit post-final `agency agent kill` for deterministic completion while preserving raw outputs.

## Scenario Results Summary

Direct scenarios:

- `D01` assistant-only text: captured for Claude/Codex/Cursor.
- `D02` read/search without edits: captured for Claude/Codex/Cursor.
- `D03` command with long output: captured for Claude/Codex/Cursor.
- `D04` single-file edit: captured for Claude/Codex/Cursor.
- `D05` multi-file edit + verification command: captured for Claude/Codex/Cursor.
- `D06` deterministic failure capture: captured for Claude/Codex/Cursor.
- `D07` tool-family supplement: captured for Cursor.

Agency-managed scenarios:

- `A01` seed prompt with edits/checkpoints: captured for Claude/Codex/Cursor.
- `A02` follow-up prompt continuation: captured for Claude/Codex/Cursor.
- `A03` history/checkpoint/turn-diff selector path: captured for Claude/Codex/Cursor.
- `A04` post-discard retention reads (`history`, `logs`, checkpoints): captured for Claude/Codex/Cursor.

## Key Takeaways

- Invocation-owned durability is now validated by real `A04` post-cleanup artifacts, not only by unit/integration assertions.
- Direct captures continue to show runner-shape divergence, reinforcing canonical action-family normalization as the right downstream contract.
- Large payload handling and failure semantics are now covered by both direct (`D03`, `D06`) and agency-managed (`A03`, `A04`) evidence.
- Future converter or renderer regressions should be validated against this corpus first.
