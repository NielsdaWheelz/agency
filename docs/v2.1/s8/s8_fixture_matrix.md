# Slice S8: Runner Fixture Capture Matrix

This document defines the fixture-capture protocol for Slice S8 PR-01 and PR-02. It is intentionally operational rather than architectural. Its purpose is to make future converter and UI work depend on preserved evidence instead of memory.

Initial captured direct corpus: `docs/v2.1/s8/s8_fixture_capture_20260312.md`

## Purpose

The fixture corpus must answer two different questions:

1. What did the runner actually emit on stdout and stderr?
2. What did Agency preserve, normalize, merge, and render from that run?

Because of that, S8 uses two capture modes:

- direct runner capture: isolates raw runner output shape
- Agency-managed capture: validates end-to-end invocation storage, checkpoints, follow-up prompts, restart, and lifecycle retention

Both are required.

## Supported Runner IDs

- Agency canonical runner IDs:
  - `claude-code`
  - `codex`
  - `cursor`
- Compatibility aliases still exist, but fixtures should record canonical IDs in metadata.
- For direct runner capture, the local executables are expected to be:
  - `claude`
  - `codex`
  - `agent` for Cursor

## Capture Principles

- Use a dedicated synthetic fixture workspace with no secrets.
- Preserve raw stdout and raw stderr exactly as emitted.
- Record enough metadata to replay or interpret the run later.
- Do not overwrite earlier captures when runner output changes. Add a new fixture revision instead.
- If a runner fails because of auth or environment, preserve that raw output too; it is still evidence.
- If a runner chooses a different but valid tool sequence than expected, keep the capture. The goal is output-family coverage, not forcing one exact reasoning path.

## Required Metadata Per Fixture

Each fixture capture should preserve at least:

- fixture id
- capture date
- runner canonical id
- direct executable used
- runner version output
- invocation mode:
  - direct
  - agency-managed
- command line used
- workspace identifier or fixture repo commit
- prompt text
- whether the run required auth and whether auth was available
- whether the run completed, partially completed, or failed
- notes about anything unusual

## Suggested Artifact Set

For each captured fixture, preserve:

- `prompt.txt`
- `stdout.raw`
- `stderr.raw`
- `meta.json`
- if Agency-managed:
  - invocation id
  - preserved invocation raw stdout path
  - preserved invocation stderr path
  - normalized stream output
  - invocation events
  - checkpoint listing output
  - selected history output or JSON

## Fixture Workspace Requirements

Use a deterministic synthetic workspace with files that make read, search, edit, command, failure, and long-output cases easy to trigger.

Minimum workspace contents:

- `README.md`
- `docs/notes.md`
- `src/math.js`
- `src/app.js`
- `data/large.txt`
- `scripts/fail.sh`
- `tests/check.sh`
- `tmp/`

Recommended content:

- `src/math.js` exports a simple `add` function
- `docs/notes.md` contains a few existing lines that can be appended to
- `data/large.txt` contains at least 300 numbered lines
- `scripts/fail.sh` exits non-zero in a deterministic way
- `tests/check.sh` is a lightweight validation script that can pass or fail deterministically

The workspace should be committed before each capture run so file changes and checkpoint behavior are easy to reason about.

## Direct Runner Command Skeletons

These are starting points, not a rigid contract. The exact non-interactive flags may vary by local auth and sandbox setup.

### Claude direct

Use `claude -p` with `--output-format stream-json`. Choose a non-interactive permission mode that allows the requested tools in the dedicated fixture workspace.

### Codex direct

Use `codex exec --json --sandbox workspace-write --skip-git-repo-check`.

### Cursor direct

Use `agent -p --output-format stream-json --trust`.
If the environment requires non-interactive approval bypass in the dedicated fixture workspace, use `--force` or its equivalent deliberately and record that in metadata.

## Agency-Managed Command Skeleton

Use headless invocations so Agency captures the same surfaces S8 is fixing.

Example shape:

```bash
agency agent start \
  --worktree <fixture-worktree> \
  --headless \
  --runner <claude-code|codex|cursor> \
  --prompt-file <prompt.txt>
```

For follow-up and restart flows, also capture:

```bash
agency agent chat <invocation_ref> --prompt-file <followup.txt>
agency agent history <invocation_ref>
agency agent history --json <invocation_ref>
agency checkpoint ls --invocation <invocation_ref>
agency agent diff <invocation_ref> --turn <turn_id>
agency agent restart <invocation_ref> --history
agency agent logs <invocation_ref>
agency agent logs <invocation_ref> --kind stderr
```

## Fixture Matrix Summary

| ID | Mode | Goal | Minimum observed families |
|---|---|---|---|
| D01 | direct | assistant-only text | assistant message, final or result |
| D02 | direct | read and search without edits | read or search tools, assistant summary |
| D03 | direct | command success plus long tool output | command tool, long tool result, assistant summary |
| D04 | direct | single-file edit | file edit activity, assistant summary |
| D05 | direct | multi-file edit plus verification command | file edits, command tool, assistant summary |
| D06 | direct | tool failure or command failure | failing tool result, error or assistant failure summary |
| A01 | agency | initial headless run with edits and checkpoints | prompt seed, runner events, checkpoint events |
| A02 | agency | follow-up prompt continuation | follow-up prompt, later runner events, checkpoint continuity |
| A03 | agency | turn selector and restart history | shared turns, checkpoint mapping, restart selection path |
| A04 | agency | lifecycle retention after land or discard | durable raw logs and history after cleanup |

## Detailed Scenarios

### D01: assistant-only text

- Objective: capture the simplest supported runner output with no tool usage.
- Mode: direct runner capture for all three runners.
- Prompt:

```text
Do not use any tools. Reply with exactly two short sentences saying this is a fixture-capture run.
```

- Expected observations:
  - assistant message content
  - result or final event
  - no tool calls

### D02: read and search without edits

- Objective: capture read/search activity and assistant summarization without mutation.
- Mode: direct runner capture for all three runners.
- Prompt:

```text
Read README.md, docs/notes.md, and src/math.js. Tell me which file defines add() and quote the first line of docs/notes.md. Do not edit any files.
```

- Expected observations:
  - file-read and search style tool calls
  - no file-edit events
  - assistant summary based on read content

### D03: command success plus long tool output

- Objective: capture a successful command tool call with non-trivial output volume.
- Mode: direct runner capture for all three runners.
- Prompt:

```text
Run a shell command that prints the first 40 lines of data/large.txt. Then tell me the last printed line. Do not edit any files.
```

- Expected observations:
  - command tool invocation
  - long tool result payload
  - assistant message derived from tool output

### D04: single-file edit

- Objective: capture the simplest file mutation path.
- Mode: direct runner capture for all three runners.
- Prompt:

```text
Append the line `fixture-note: direct-edit` to docs/notes.md and then report completion.
```

- Expected observations:
  - file-edit tool or file-change event
  - assistant completion message
  - no unrelated command noise unless the runner chooses to verify the edit

### D05: multi-file edit plus verification command

- Objective: capture richer edit activity plus command verification in one run.
- Mode: direct runner capture for all three runners.
- Prompt:

```text
Update src/math.js so it also exports subtract alongside add. Update README.md to mention subtract. Then run tests/check.sh and summarize the result.
```

- Expected observations:
  - multiple file edits or one multi-edit event
  - command execution for verification
  - assistant summary that mentions both edits and command outcome

### D06: tool or command failure

- Objective: capture a deterministic failure path and verify how it appears in raw output.
- Mode: direct runner capture for all three runners.
- Prompt:

```text
Run scripts/fail.sh, do not edit any files, and briefly explain the failure.
```

- Expected observations:
  - command tool invocation
  - non-zero exit or explicit failure result
  - assistant explanation or runner error summary

### A01: headless run with edits and checkpoints

- Objective: capture Agency-managed invocation storage plus checkpoint-relevant mutations.
- Mode: Agency-managed for all three runners.
- Initial prompt:

```text
Read docs/notes.md, append the line `agency-seed: one`, then update README.md with a short note that this workspace is for fixture capture. After that, summarize what changed.
```

- Expected observations:
  - prompt seed in invocation history
  - runner events for reads, edits, and assistant summary
  - checkpoint events associated with the mutation path
  - raw stdout and stderr preserved under the invocation

### A02: follow-up prompt continuation

- Objective: capture Agency follow-up prompts as first-class invocation events and ensure later runner output continues cleanly.
- Mode: Agency-managed for all three runners after A01 succeeds.
- Follow-up prompt:

```text
Read docs/notes.md again, append the line `agency-followup: two`, and summarize both lines you added in this invocation.
```

- Expected observations:
  - explicit follow-up prompt event in Agency history
  - subsequent runner events after the follow-up
  - continued checkpoint association after the second mutation
  - consistent turn rendering across history and restart surfaces

### A03: turn selector and restart history

- Objective: validate that the same invocation can drive history, checkpoints, diff turn selectors, and restart selection.
- Mode: Agency-managed for all three runners after A02 succeeds.
- Capture steps:
  - run `agency agent history <invocation_ref>`
  - run `agency agent history --json <invocation_ref>`
  - run `agency checkpoint ls --invocation <invocation_ref>`
  - identify a meaningful turn id from history
  - run `agency agent diff <invocation_ref> --turn <turn_id>`
  - run `agency agent restart <invocation_ref> --history`

- Expected observations:
  - turn ids exposed by history are meaningful to diff and restart flows
  - checkpoint associations are consistent between checkpoint listing and restart mapping
  - the selected latest meaningful turn is not just the last low-level event

### A04: lifecycle retention after land or discard

- Objective: validate S8 durable capture requirements after sandbox cleanup.
- Mode: Agency-managed for at least one supported runner, then repeated for all if behavior differs.
- Capture steps:
  - complete A01 through A03
  - land or discard the invocation
  - re-run:
    - `agency agent history <invocation_ref>`
    - `agency agent history --json <invocation_ref>`
    - `agency agent logs <invocation_ref>`
    - `agency agent logs <invocation_ref> --kind stderr`
    - `agency checkpoint ls --invocation <invocation_ref>` if applicable

- Expected observations:
  - raw stdout and stderr remain readable after cleanup
  - history still renders without depending on a live sandbox directory
  - checkpoint and turn data remain inspectable enough for post-run analysis

## Runner-Specific Notes

### Claude

- Full tool-using coverage may require local authenticated access.
- If auth is unavailable, preserve the auth-blocked raw output under a separate fixture id and mark the tool-using scenarios as pending.

### Codex

- Current output must be checked for:
  - `agent_message` text shape
  - `command_execution`
  - `file_change`
  - usage and final event shape

### Cursor

- Current output must be checked for:
  - echoed prompt as a user message
  - nested tool arguments
  - nested result payloads
  - file-edit output shape

## Acceptance for the Fixture Corpus Itself

The fixture corpus is sufficient for S8 when:

- all three supported runners have direct captures for D01 through D06
- at least one full authenticated tool-using Claude run exists, or the gap is explicitly recorded and queued for user-assisted capture
- all three supported runners have Agency-managed captures for A01 through A03
- at least one lifecycle-retention capture exists showing post-land or post-discard readability
- metadata is complete enough to replay or reinterpret the fixtures later
- the corpus contains at least one large-output case and one failure case

## If User Assistance Is Needed

Ask the user to run captures only when local auth or environment restrictions prevent a meaningful run.

When delegating to the user:

- give one exact command block per runner
- give one prompt file per scenario
- ask them to return the preserved stdout, stderr, and invocation ids
- keep the fixture workspace synthetic and disposable
