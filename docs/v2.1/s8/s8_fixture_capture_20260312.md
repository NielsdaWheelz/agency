# Slice S8: Direct Runner Fixture Capture - 2026-03-12

This note records the first real direct-runner corpus captured for Slice S8. It exists so later parser and projection work can rely on preserved evidence rather than chat history or memory.

## Capture Summary

- Capture date: 2026-03-12
- Temporary run root used during capture: `/tmp/agency-s8-fixtures/runs/20260312T184346Z`
- Durable imported fixture path: `internal/daemon/stream/testdata/s8_20260312`
- Mode: direct runner capture only
- Scenarios captured:
  - `D05` multi-file edit plus verification command
  - `D06` failure case

## Coverage Gap Snapshot

This capture revision intentionally focused on the highest-risk direct-runner shapes first.

- Captured in this revision: `D05`, `D06`
- Not yet captured in this revision: `D01`, `D02`, `D03`, `D04`
- Agency-managed scenarios still pending: `A01`, `A02`, `A03`, `A04`

## Runner Versions

- Claude: `2.1.72 (Claude Code)`
- Codex: `codex-cli 0.114.0`
- Cursor agent: `2026.02.27-e7d2ef6`

## Imported Fixture Files

- `internal/daemon/stream/testdata/s8_20260312/claude_d05_success.jsonl`
- `internal/daemon/stream/testdata/s8_20260312/claude_d06_failure.jsonl`
- `internal/daemon/stream/testdata/s8_20260312/codex_d05_success.jsonl`
- `internal/daemon/stream/testdata/s8_20260312/codex_d06_failure.jsonl`
- `internal/daemon/stream/testdata/s8_20260312/cursor_d05_success.jsonl`
- `internal/daemon/stream/testdata/s8_20260312/cursor_d06_failure.jsonl`

## Environment Notes

- Claude direct capture required `--verbose` together with `-p --output-format stream-json`.
- Codex direct capture inside the sandbox failed because the sandboxed process could not resolve or reach the Codex API endpoint. The successful fixture was captured outside the sandbox.
- Cursor direct capture inside the sandbox failed with `SecItemCopyMatching failed -50`. The successful fixture was captured outside the sandbox.
- Successful direct captures for all three runners produced empty stderr files in the final outside-sandbox runs.

## Scenario Results

### D05: multi-file edit plus verification command

- Claude: success
- Codex: success
- Cursor: success

Observed output families:

- Claude:
  - `system` init event
  - repeated `assistant` message events
  - `assistant` content blocks containing `tool_use`
  - `user` message events containing `tool_result`
  - `result` success event
- Codex:
  - `thread.started`
  - `turn.started`
  - `item.started` and `item.completed`
  - `agent_message`
  - `command_execution`
  - `file_change`
  - `turn.completed`
- Cursor:
  - `system` init event
  - echoed initial `user` prompt event
  - `tool_call` started and completed events
  - nested `args`
  - nested `result.success`
  - `assistant` message event
  - `result` success event

Important implications:

- Claude tool results include high-detail payloads such as file contents, structured patches, and command stdout.
- Codex now emits `agent_message.text`, `command_execution.aggregated_output`, and `file_change` item families that the current adapter work must model explicitly.
- Cursor still echoes the starting prompt as a top-level `user` message, which explains the current prompt-as-Tool-Result rendering bug if the normalization path does not filter or re-tag it.
- Cursor edit events include rich diff and full-file payloads under nested `result.success`, which should not be shown by default in human-readable history.

### D06: failure case

- Claude: success as a capture, with intentionally failing tool execution represented in-stream
- Codex: success as a capture, with intentionally failing tool execution represented in-stream
- Cursor: success as a capture, with intentionally failing tool execution represented in-stream

Observed failure-shape differences:

- Claude records the failed shell execution as a `user` message containing a `tool_result` with `is_error: true`, then still emits a final `result` success event for the assistant turn summary.
- Codex records the failed shell execution as `command_execution` with `status: "failed"` and `exit_code: 7`, then continues with more `agent_message` explanation before `turn.completed`.
- Cursor records the failed shell execution inside `tool_call` completion with nested `result.failure`, including `stdout`, `stderr`, and `exitCode`, then emits an assistant explanation and final `result` success event.

## Key Parser and Renderer Takeaways

- The direct corpus confirms that preserving raw stdout exactly is the right durability model. All three runners now expose real structured detail that the current adapters do not fully model.
- Human-readable defaults must not dump nested diff payloads, full file contents, or large command outputs. Those belong behind raw, JSON, or explicit expansion.
- Codex and Cursor adapter updates should be driven by these fixture files first, not by the older hand-written fixtures alone.
- The unified event layer must be able to distinguish:
  - assistant narrative
  - tool start and completion
  - file mutation summaries
  - command failure vs assistant failure
  - echoed prompt artifacts

## Follow-On Work Enabled

This corpus is sufficient to start:

- PR-01 validation for durable raw capture expectations
- PR-02 converter refresh for current Claude, Codex, and Cursor output shapes
- parser regression tests that preserve support for current runner versions

Agency-managed captures are still required after PR-01 so end-to-end invocation retention, checkpoints, follow-up prompts, and restart history can be validated against the same standards.
