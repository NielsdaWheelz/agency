codex resume 019cdba0-b5ef-7331-9d1c-a2aadb5e9e57

cd "/Users/nnandal/Library/Application Support/agency/repos/769749d77af0806f/worktrees/20260311064003-1d7a"

# anomalies
Known Gaps

- codex:
    - finished_at changed to the land time after land --apply
    - transient file_change emitted an unknown runner event
    - we did not run the full live follow-up chain
- cursor:
    - updateTodosToolCall is parsed as unknown
    - history --last, review, and restart --history anchor on the wrong latest
      turn
    - after checkpoint apply, plain agent diff said (no changes) when checkpoint 1
      clearly existed
    - turn/checkpoint association drifted after restore/discard
    - we did not run the full live follow-up chain
- claude-code:
    - retained logs and logs --kind stream still read like huge blobs, not good
      human surfaces
    - we did not run the full live follow-up chain
    - we did not complete the headless post-finish agent stop lifecycle check
- Headed flow:
    - enter, attach, shell, and kill resolved the right sandbox/session
    - failures: headed smoke prompt did not produce the expected file, agent stop
      left the invocation running / needs attention, and restart --history has a
      real viewport bug on long lists

## 1
 - the Claude create output said Created integration worktree 's8-manual-2ude'
 - but the branch and path were correct for s8-manual-20260328-013029-claude
That looks like a display/rendering bug, not a creation failure. We’ll verify it in ls and show.

The remove guard did not behave as expected. An untracked file was not enough to block worktree rm. That means one of two things:
 - the implementation only treats tracked modifications as dirty
 - or worktree rm is incorrectly ignoring untracked dirt despite the help text
 - tracked-file dirt is correctly blocked
 - untracked-only dirt did not block removal
 - worktree rm blocks tracked-file dirt
 - worktree rm did not block untracked-only dirt, despite the wording “uncommitted changes” in help

## 2
no codex checkpoints:
 - $AGENCY_BIN checkpoint ls --invocation "$CODEX_INV"
 - $AGENCY_BIN checkpoint ls --invocation "$CODEX_INV" --json
 - checkpoint ls found nothing
 - plain agent diff showed no changes, even though the assistant claimed it created files
 - review stayed coherent, and turn-aware diff --turn failed cleanly with E_CHECKPOINT_NOT_FOUND, which is the right failure mode given the missing checkpoints.

## 3
  - `agency ls` summary is too long. probably don't need summary in there at all -- just messes up formatting, too much content

## 4
  - when i `agent discard`, is that a full cleanup? completely everything deleted completely?

## 5
  - `worktree rm` on a non-existent worktree hangs

## 6
two suspicious things for Codex:
  - display_status: ready for review while status: running
  - history shows unknown runner event: item.started (unsupported item type)

## 7
 - One thing to note for later: history --json --last includes sandbox-absolute markdown links in the assistant text. That may be fine, but we’ll keep an eye on whether those links stay valid after discard.

## 8
 - agent diff --turn-range "prompt_seed..$LATEST_TURN_CODEX" failing with E_CHECKPOINT_NOT_FOUND is acceptable for this run. prompt_seed has no checkpoint mapping, so this is a valid typed failure, not a broken diff surface.

## 9
 One real issue remains from Codex:
 - finished_at changed to the land time (19:38:04Z) instead of staying at runner completion time (last_output_at: 19:13:47Z). That looks like a bug. Keep that noted.

## 10
 - cursor working says BLOCKED -- should it?

## 11
 - a real Cursor bug.
 - the final/latest turn is an unknown runner event from updateTodosToolCall
 - review therefore degraded to a useless summary instead of the actual assistant close-out
 - checkpoints are fine, but latest-turn selection is wrong

## 12
 - cursor `agent restart "$INV_CURSOR" --history` can't cycle through, can't read tools

## 13
  - Cursor is emitting updateTodosToolCall events.
  - The Cursor adapter does not understand that structure, so it falls back to
    unknown runner event: tool_call (unrecognized tool structure) in /Users/nnandal/
    Library/Application%20Support/agency/repos/769749d77af0806f/
    worktrees/20260311064003-1d7a/internal/daemon/stream/cursor.go.
  - The history grouper turns those unknowns into diagnostic turns in /Users/
    nnandal/Library/Application%20Support/agency/repos/769749d77af0806f/
    worktrees/20260311064003-1d7a/internal/tui/historypicker/turn.go.
  - The latest-activity/review path then blindly takes the last grouped turn in /
    Users/nnandal/Library/Application%20Support/agency/repos/769749d77af0806f/
    worktrees/20260311064003-1d7a/internal/daemon/read_turn_projection.go:90 and /
    Users/nnandal/Library/Application%20Support/agency/repos/769749d77af0806f/
    worktrees/20260311064003-1d7a/internal/commands/agent.go:3366.
  - That is why review, history --last, and restart --history are anchoring on the
    trailing todo-bookkeeping diagnostic instead of the real assistant close-out.

  So the bug is:

  1. Cursor parser gap for updateTodosToolCall.
  2. Bad “latest meaningful turn” heuristic.
  3. Restart/history UI is showing those diagnostic turns as first-class restart targets, which makes it look noisy and wrong.
    So for Cursor, the correct fix is:

  - parse or explicitly suppress updateTodosToolCall from default human surfaces
  - do not let trailing diagnostic/tool-housekeeping rows become the canonical
    “latest turn”
  - prefer the latest assistant/follow-up turn for review, history --last, and
    restart --history

## 14
  Two more Cursor bugs to note from this step:
  - checkpoint apply 1 followed by plain agent diff showed (no changes) even though checkpoint 1 was tmp/s8-manual/notes.md.
  - After restore, the historical latest turn drifted: stream:44 had been tied to checkpoint 2, but after apply/discard it showed checkpoint=1. That means turn/checkpoint projection is not stable.

## 15
 - logs and retained logs --kind stream are too blob-like to count as good human-facing surfaces

## 16
 - headed should skip permissions?

## 17
 - agent shell turning a child exit code into E_INTERNAL is a CLI bug/rough edge. /Users/nnandal/Library%20/Application%20Support/agency/repos/769749d77af0806f/worktrees/20260311064003-1d7a/internal/commands/agent.go:2482 currently wraps non-zero shell exit as internal.

## 18
 - agent stop returned success, so the interrupt was sent
 - agent show --json still reports status: running with display_status: needs attention. So the headed invocation did not converge cleanly after stop.

## 19
 - The daemon supports follow-up prompts on a running invocation; the problem is just that our tested invocation had already exited. I’m turning that into the next concrete step.
 - This is a real gap, and we’ve now pinned it down precisely:
- agent chat only works while the invocation is running. /Users/nnandal/Library%20/Application%20Support/agency/repos/769749d77af0806f/worktrees/20260311064003-1d7a/internal/commands/agent.go:2601 and the daemon return E_INVOCATION_NOT_RUNNING once the run has finished.
- So the current matrix wording, “after follow-up 1 completes, send follow-up 2,” does not match the actual product behavior on this build. That means Codex follow-up 1 passed, but the rest of the live follow-up chain still needs a different execution style: queue the later follow-ups while the invocation is still active.

## 20
 - latest checkpoint remained cp:3 from the multifile-write turn, while the final failure-summary turn was stream:106. That’s defensible because the failure follow-up changed no files, but it means “latest turn” and “latest checkpoint” are not the same concept.

## 21
 - What still looks wrong:
  - updateTodosToolCall is still polluting history as unknown runner event
  - follow-up context appears twice: once as inv_event:*:agency.followup_prompt, then again later as runner-visible [follow-up] turns
  - the final failure turn stream:102 is associated with checkpoint=6, which is really the prior file-change checkpoint from report.txt
  - Cursor also surfaced a platform-specific wrinkle: nl -ba behavior on macOS during the multifile verify prompt
 - So Cursor is functional end-to-end, but not cleanly normalized.

## 22
 - 