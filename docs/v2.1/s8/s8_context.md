# Slice S8: Invocation History and Runner Log Convergence - Context and Findings

This document is intentionally more detailed than the L2 or L3 artifacts. It captures the concrete problem statement, evidence gathered during discovery, the architectural model for the slice, and the validation plan so future sessions do not need to reconstruct the rationale from chat history.

Companion operational docs:

- `docs/v2.1/s8/s8_fixture_matrix.md`
- `docs/v2.1/s8/s8_fixture_capture_20260312.md`

## Scope

- Supported runners in scope: Claude, Codex, and Cursor (`agent`) only.
- Out of scope for this slice: `amp`, `opencode`, `droid`, GUI surfaces, and web dashboard work.
- `agent logs` stays in scope as the raw or debug lane, even though its role is different from the default human-readable surfaces.

## Problem Summary

The current system has three overlapping failures:

- Capture is not fully durable. Some invocation history still depends on sandbox-owned artifacts that can disappear after cleanup.
- Runner normalization is stale for current Codex and Cursor outputs, so valid output can become blank, misclassified, or excessively noisy.
- Human-facing surfaces derive different truths from the same invocation, because history, restart, checkpoint, diff, list, watch, show, and review do not share one projection model.

The user-visible result is:

- blank or partially blank history
- prompt text rendered as `Tool Result`
- Codex history dominated by large shell-command or diff text
- confusing `agent history --last`
- restart history not matching normal history
- `agent ls` and watch surfaces that do not clearly answer what an invocation is doing now, what it last did, or what happened recently

## Concrete Findings from Discovery

### durable-history gap

- Runner raw and normalized logs are currently stored under sandbox-owned paths rather than invocation-owned paths.
- Successful lifecycle cleanup can delete sandbox directories, which removes the logs needed for later history rendering.
- In the sampled historical invocations, invocation metadata and event logs remained, but raw or normalized runner logs were already gone.

### timeline and parser mismatch

- The parser accepts larger lines than the timeline reader currently scans.
- Timeline reading currently has a smaller scanner limit and does not surface scanner errors, which can silently drop later entries after a large line.
- This means capture can succeed while later history reads still degrade or truncate unexpectedly.

### stale Codex conversion

- Current Codex output shape has drifted from the adapter assumptions.
- Discovery observed current Codex output including:
  - agent message text in `item.text`
  - file-edit events represented as `file_change`
  - command execution events with aggregated output
- The current adapter does not model these shapes correctly, so assistant text and file-change semantics can be lost.

### stale Cursor conversion

- Current Cursor output shape also drifted from the adapter assumptions.
- Discovery observed current Cursor output including:
  - an echoed initial prompt as a `user` message
  - tool call arguments nested under `args`
  - result data nested under `result.success`
- The current parser and renderer path can misclassify the echoed prompt as a tool result and can lose structured tool metadata.

### duplicated turn and rendering logic

- `agent history` uses one transcript renderer.
- `agent restart --history` uses a separate history-picker grouping path.
- `agent diff --turn` resolves selectors against the flat timeline independently.
- `checkpoint ls`, `agent ls`, `agent ls --watch`, `agency watch`, `agent show`, and `agent review` each summarize invocation state differently.
- Because there is no shared turn or activity projection, the same invocation can look different depending on the command used.

### current raw or debug lane still matters

- Raw logs are still required for debugging, replay, future parser fixes, and inspection of unknown output.
- The right answer is not to remove raw access. The right answer is to stop dumping raw detail by default in human-friendly surfaces.

## Sample Historical Invocations Examined

- `20260308200107-57e2`
- `20260308200145-4506`

These samples were useful because they showed the durability gap directly: invocation metadata and event logs remained available while some runner-log artifacts had already disappeared.

## Invocation Surfaces That Must Converge

These surfaces should ultimately consume the same canonical invocation event and projection model:

- `agency agent history`
- `agency agent history --last`
- `agency agent restart --history`
- `agency checkpoint ls --invocation`
- `agency agent diff --turn`
- `agency agent diff --turn-range`
- `agency agent ls`
- `agency agent ls --watch`
- `agency watch`
- `agency agent show`
- `agency agent review`
- `agency agent checks`

This surface stays related but remains the raw or debug lane:

- `agency agent logs`

The main consistency requirement is that these surfaces agree on:

- latest meaningful activity
- turn boundaries
- prompt and follow-up semantics
- checkpoint association
- turn selector identity
- concise human summary vs explicit raw expansion

## Architectural Model for S8

The slice should converge on five layers.

### layer 1: raw capture

- Preserve invocation-owned runner stdout and stderr as the source of truth.
- Raw capture must survive landing, discard, and later replay.
- Raw capture should remain useful even if the current parser is wrong or the runner output format changes later.

### layer 2: runner conversion

- Keep one converter per supported runner: Claude, Codex, Cursor.
- Converters are replayable from preserved raw capture.
- Unknown output must not disappear silently. It should be preserved and surfaced with diagnostics.
- Converters should normalize into stable action families rather than depending on a brittle whitelist of exact tool names.
- Example action families include:
  - message
  - tool call started
  - tool call completed
  - command execution
  - file read
  - file change
  - search
  - web action
  - status
  - final
  - error
  - unknown runner event

### fallback ladder for unfamiliar runner output

- First preference: parse the event fully into the right canonical action family with structured fields.
- Second preference: if the exact event shape is unfamiliar, still classify it into a broader action family and retain useful fields such as tool name, command, path, URL, query, exit code, or short text.
- Last preference: emit an explicit unknown or partially parsed event with a diagnostic summary and keep the raw payload available through raw or JSON inspection.
- Blank output or misleading labels are always considered incorrect behavior.

### layer 3: unified invocation event model

- Merge runner-derived events with Agency-authored events.
- Agency events remain first-class and include:
  - prompt seed
  - follow-up prompts
  - checkpoint lifecycle
  - land or discard lifecycle
  - other invocation control-plane events
- Human-facing surfaces should not treat runner events as the only source of invocation truth.

### layer 4: shared projections

- Derive shared projections from the unified invocation event stream.
- Minimum shared projections:
  - turns
  - latest activity summary
  - checkpoint summary
  - turn selector mapping
- These projections are the contract for human-facing surfaces.

### layer 5: surface renderers

- Human-readable history, restart picker, checkpoint views, list rows, watch panels, show, review, and diff-navigation views should consume shared projections.
- Raw or machine-readable modes remain explicit escape hatches.
- Default rendering should be concise and human-readable, not a dump of raw payloads.

## Current Code Hotspots

The following areas were identified during discovery as the current implementation hotspots for S8 work:

- store paths for invocation vs sandbox ownership
- daemon lifecycle cleanup that removes sandbox-owned artifacts
- timeline assembly and pagination
- runner stream adapters for Claude, Codex, and Cursor
- transcript rendering and raw transcript rendering
- restart history picker turn grouping
- turn-aware diff selector resolution
- invocation list and watch summary rendering
- checkpoint listing
- invocation show and review summaries
- raw log read surfaces

More concretely, the main files involved today are:

- `internal/store/store.go`
- `internal/daemon/landing/service.go`
- `internal/daemon/read_timeline.go`
- `internal/daemon/stream/parser.go`
- `internal/daemon/stream/claude.go`
- `internal/daemon/stream/codex.go`
- `internal/daemon/stream/cursor.go`
- `internal/render/transcript.go`
- `internal/tui/historypicker/turn.go`
- `internal/tui/historypicker/model.go`
- `internal/daemon/read_diff_turn.go`
- `internal/commands/agent.go`
- `internal/commands/checkpoint.go`
- `internal/commands/watch.go`
- `internal/commands/watch_tui.go`
- `internal/watch/model.go`
- `internal/daemon/read_checks.go`

## Rendering Principles

- Default human-readable views should summarize large tool payloads.
- Full tool payloads, file-edit diffs, or raw runner output should be behind raw, json, or explicit expansion behavior.
- Unknown content should render with explicit diagnostics rather than blank lines or misleading labels.
- Summaries should be driven by action family rather than runner-specific tool names.
- Examples:
  - file read: path plus short range or size summary
  - search: query plus hit count or target scope
  - file change: changed paths plus concise counts
  - command execution: command plus exit code plus short output snippet
  - web action: URL or host plus concise result summary
- `agent logs` remains the lowest-level inspection path and should not be treated as the design baseline for other human-facing surfaces.

## Manual Runner Evidence Collected

Discovery included official-doc review plus local runner smokes observed on 2026-03-11 and a real direct-runner fixture capture on 2026-03-12.

### official docs consulted

- Anthropic Claude Code SDK docs
- OpenAI Codex CLI docs
- Cursor CLI output-format docs

These were used to validate current output-family expectations before comparing them to the repo adapters.

### local Claude observation

- A local Claude stream-json smoke confirmed the general event family, but full tool-using validation was blocked by local auth state.
- Conclusion: Claude adapter looked broadly closer to current behavior than Codex and Cursor, but full fixture coverage for authenticated tool use is still required.

### local Codex observation

- A short successful run produced current item families including `agent_message`, `command_execution`, and `file_change`.
- The observed output confirms that current repo assumptions about message and file-edit shapes are stale.

### local Cursor observation

- A short successful run produced current system, user, tool-call, assistant, and result events.
- The observed echoed prompt and nested tool-call structure confirm that the current parser and renderer path is stale.

### direct fixture capture on 2026-03-12

- A full direct-runner fixture batch for `D05` and `D06` was captured on 2026-03-12 and imported into `internal/daemon/stream/testdata/s8_20260312`.
- Capture details and findings are recorded in `docs/v2.1/s8/s8_fixture_capture_20260312.md`.
- The imported corpus confirms:
  - Claude requires `--verbose` with `-p --output-format stream-json`
  - current Codex emits `agent_message.text`, `command_execution`, and `file_change`
  - current Cursor echoes the initial prompt as a top-level `user` event and nests tool results under `result.success` or `result.failure`
  - all three runners include enough raw detail that human-readable defaults must summarize rather than dump payloads

## Validation Strategy

### fixture corpus

- Capture real raw outputs for current Claude, Codex, and Cursor runs.
- The currently imported direct corpus covers `D05` and `D06` only; it is useful but not yet full matrix coverage.
- Include long tool-using sessions with:
  - assistant-only text
  - prompt echo or follow-up prompts
  - shell or command tools
  - file reads
  - file edits
  - search or grep style tools
  - optional runner-native web tools where supported
  - success and failure cases
  - long payloads and large edits
- Preserve these fixtures as replay inputs for converter regression tests.

### docs plus reality

- Do not rely on docs alone.
- Validate against real locally installed runner behavior because the runner CLIs can drift ahead of repo assumptions.

### explicit unknown-data behavior

- Tests should verify that unknown or partially parsed output remains visible via raw capture and surfaces typed parse or truncation diagnostics.
- The system should never silently degrade to an empty or misleading user-facing row.

### cross-surface consistency

- Tests should verify that one invocation yields the same turn boundaries, checkpoint associations, latest activity, and selector semantics across:
  - history
  - restart history
  - checkpoint views
  - turn-aware diff
  - list or watch summaries
  - show and review

## Open Execution Notes

- A full authenticated Claude fixture run may require the user to run a capture prompt matrix locally if local automation cannot access Claude tool-using mode.
- The slice should assume that future runner versions will change shape again; replayable raw capture and fixture refresh are therefore design requirements, not nice-to-haves.
- The UI or TUI overhaul should not begin until raw capture durability and shared projections are stable. Otherwise the project will produce a better-looking surface over inconsistent truth.
