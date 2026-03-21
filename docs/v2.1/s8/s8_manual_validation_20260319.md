# Slice S8: Manual Agency Validation - 2026-03-19

This note records the real manual `agency agent ...` validation pass run against the local branch binary (`./agency`) rather than the older installed system binary.

## Setup

- Repo root binary:
  - `GOCACHE=/tmp/agency-gocache go build -o ./agency ./cmd/agency`
- Integration worktree used:
  - `headless-test` (`20260305210624-9396`)
- Prompt files used for the pass:
  - `docs/v2.1/s8/manual_prompts/seed_short.txt`
  - `docs/v2.1/s8/manual_prompts/seed_live.txt`
  - `docs/v2.1/s8/manual_prompts/followup_live.txt`

## Invocations

- Codex:
  - `20260319073701-1b5e`
- Claude:
  - `20260319074013-b0d6`
- Cursor:
  - `20260319074340-1a8e`

## Surfaces Exercised

- `./agency agent start --headless --json`
- `./agency agent show --json`
- `./agency agent ls --json`
- `./agency agent history`
- `./agency agent history --last`
- `./agency agent history --json`
- `./agency agent history --json --last`
- `./agency agent logs`
- `./agency agent logs --kind stream`
- `./agency agent logs --kind stderr`
- `./agency agent logs --follow`
- `./agency checkpoint ls --invocation`
- `./agency agent review`
- `./agency agent diff --turn <latest_turn>`
- `./agency agent restart --history`
- `./agency watch`
- `./agency agent chat --prompt-file ... --json`
- `./agency agent discard --json`

## Shared Passes

- Invocation-owned raw/stream/stderr retention worked after discard for Codex, Claude, and Cursor.
- `agent history`, `history --json`, `checkpoint ls`, `show`, `review`, and `restart --history` all rendered meaningful turn/checkpoint information instead of blank rows.
- `agent chat` produced first-class `agency.followup_prompt` history entries for Claude and Cursor while the invocation was live.
- `agent ls --watch` correctly hard-failed with the expected guidance to use `agency watch`.
- `stderr` remained readable and empty for all three manual invocations.

## Shared Defects Still Seen

- `agent diff --turn <latest_turn>` printed `(no changes)` for Codex, Claude, and Cursor even when `show`, `review`, and `checkpoint ls` all reported changed paths and checkpoint diffstats for that same turn.

## Runner-Specific Findings

### Codex (`20260319073701-1b5e`)

- Human-facing surfaces were coherent before and after discard.
- Raw and stream logs dropped the first line of a simple shell command payload:
  - expected shell output: `sleep-start` + `sleep-end`
  - captured tool payload only preserved `sleep-end`

### Claude (`20260319074013-b0d6`)

- Both the seed turn and the follow-up turn emitted successful `final` events in the stream log.
- Despite that, `agent show` and `agent review` remained `running` / `working` until manually stopped.
- After `./agency agent stop`, the invocation became:
  - `status: failed`
  - `exit_reason: stopped`
  - `exit_code: 0`
- This is a lifecycle/status reduction bug, not a durability bug.

### Cursor (`20260319074340-1a8e`)

- Prompt echoing was rendered as `prompt` / `follow-up`, not as `Tool Result`.
- Follow-up continuation completed successfully in the same invocation and converged on `finished` / `ready for review`.
- Cursor therefore passed the runner-specific status/follow-up bar in this manual pass.

## Low-Severity UX Notes

- `agency watch` painted one empty frame (`repos:0`, `no invocations found`) before refreshing to the real state.

## How To Re-Run

1. Build the local branch binary from repo root:
   - `GOCACHE=/tmp/agency-gocache go build -o ./agency ./cmd/agency`
2. Use a clean integration worktree or create one from a clean parent branch.
3. Start a runner with one of the prompt files above.
4. While it is active, inspect `show`, `history`, `logs --follow`, `watch`, and `chat`.
5. After it completes, inspect `history --last`, `history --json --last`, `checkpoint ls`, `review`, `diff --turn`, and `restart --history`.
6. Discard the invocation and re-run `show`, `history`, `logs`, and `checkpoint ls` to confirm post-cleanup retention.
