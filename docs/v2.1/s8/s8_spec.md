# Slice S8: Invocation History and Runner Log Convergence - Spec

## Goal

Make invocation history, checkpoints, logs, and live activity for Claude, Codex, and Cursor durable, replayable, and consistent across all human-facing surfaces.

## Acceptance Criteria

### invocation-owned raw capture survives lifecycle cleanup
- **given**: a Claude, Codex, or Cursor invocation finishes and is later landed or discarded
- **when**: a user reads its raw logs, normalized history, restart context, or checkpoint context
- **then**: the invocation still has its original raw stdout and stderr, and history rendering no longer depends on a surviving sandbox directory

### preserved raw output remains replayable across runner version changes
- **given**: historical raw output from a supported runner and a newer converter implementation
- **when**: Agency replays that raw capture
- **then**: canonical normalized events can be regenerated without losing the original raw bytes, and unrecognized output is surfaced explicitly rather than silently dropped

### supported runners converge into one canonical invocation event model
- **given**: current Claude, Codex, and Cursor outputs that include assistant messages, echoed prompts or follow-ups, tool activity, tool results, file edits, errors, usage, and checkpoint-relevant mutations
- **when**: Agency ingests or replays those runs
- **then**: the resulting invocation event stream preserves the same semantic categories across the three supported runners, remains mergeable with Agency control-plane events, and does not require downstream surfaces to branch on runner-specific tool names

### default human history is concise while full fidelity remains available
- **given**: an invocation with verbose tool output or large file edits
- **when**: a user views history or other human-facing activity surfaces in their default mode
- **then**: the default view shows concise summaries instead of dumping full raw tool payloads, and a raw or debug path still exposes the complete underlying output

### all invocation surfaces share one turn and activity truth
- **given**: a single invocation with prompts, follow-ups, assistant responses, tool use, checkpoints, and later review or watch activity
- **when**: a user inspects history, restart-from-history, checkpoint history, turn-aware diff, invocation list or watch, show, or review surfaces
- **then**: those surfaces agree on latest activity, turn boundaries, checkpoint associations, and turn selectors

### unknown or partially parsed data never renders as misleading blanks
- **given**: runner output that is new, malformed, oversized, or only partially understood by a converter
- **when**: a user inspects the invocation through human or machine-readable read surfaces
- **then**: Agency preserves the raw data, exposes parse or truncation diagnostics, avoids mislabeling unknown content as a tool result or an empty message, and preserves echoed user prompts as prompts rather than tool results

## Key Decisions

**Raw capture is the source of truth**: supported runner stdout and stderr are preserved under invocation ownership as immutable replay inputs. Normalized events and human-readable views are derived artifacts, not the authority path.

**Runner conversion is replayable and runner-specific**: Claude, Codex, and Cursor each keep their own converter, but they all emit the same canonical event categories so downstream history and activity surfaces do not need runner-specific branching.

**Normalization is by action family, not by tool-name whitelist**: converters should map runner-specific tool shapes into stable action families such as message, tool start or completion, command execution, file read, file change, search, web action, final, error, and unknown event. The system should not depend on a fixed list of exact tool names to remain usable.

**Agency control-plane events remain first-class**: prompt seed, follow-up prompts, checkpoint lifecycle, landing, discard, and related Agency-authored events are not modeled as runner output. The canonical invocation view is formed by merging runner-derived events with Agency control-plane events.

**Human surfaces consume projections, not flat raw timelines**: turns, latest activity summaries, checkpoint summaries, and turn selectors are shared derived views used across history, restart, watch, review, show, and diff flows.

**Default human rendering is summarized and raw access is explicit**: human-readable surfaces optimize for comprehension, while raw or machine-readable modes remain available for full-fidelity inspection and debugging.

**Unknown output must degrade safely, not disappear**: if a converter does not fully understand a new runner event shape, Agency should still preserve the raw event, emit a typed unknown or partially parsed event, and render a useful diagnostic summary instead of an empty row or misleading label.

**Scope is intentionally limited to Claude, Codex, and Cursor**: this slice hardens the supported runner set that already drives the key history and checkpoint experience. Additional runner families are deferred until this model is stable.

## Out of Scope

- New runner support beyond Claude, Codex, and Cursor
- GUI or web dashboard surfaces
- Replacing raw log access with normalized-only views
- Aesthetic-only TUI work before data and projection convergence
- Checkpoint engine redesign beyond what is required for durable history, replay, and shared projection correctness
