# [manual][s8] Remaining Gaps After 2026-03-28 Exhaustive Validation

labels: `manual-validation`, `area:s8`, `type:bug`, `type:ux`, `type:docs`

## Goal

Turn the March 28-29, 2026 exhaustive manual pass into actionable follow-up work.
This file is not the full test log. It captures the confirmed gaps that remained
after the pass, plus the documentation and fixture mismatches that materially
interfered with the pass.

## Context

- Branch under test: `agency/refactor-output-1d7a`
- Binary under test: repo-local `./agency`
- Major manual flows covered:
  - headless seed runs on Codex, Cursor, and Claude
  - queued follow-up 1 / 2 / 3 on fresh Codex, Cursor, and Claude invocations
  - turn-aware `agent diff --turn`
  - `agent restart --history`
  - Codex `agent land --apply`
  - Cursor and Claude `agent discard`
  - headed `enter` / `attach` / `shell` / `stop` / `kill`
  - remote preflight, `worktree update`, negative `pr sync`, negative `merge`
- Core path that did work:
  - all three headless runners completed the seed run
  - all three headless runners also completed queued follow-up 1 / 2 / 3
  - `history`, `checkpoint ls`, `review`, and `agent diff --turn` worked well
    enough to inspect the richer runs
  - retention survived Codex land and Cursor / Claude discard
  - headed `enter`, `attach`, `shell`, and `kill` resolved the correct
    sandbox/session

## Confirmed Issues

### 1. Codex `finished_at` is mutated by `agent land --apply`

**Observed on:** `20260328191202-74f9`

**Repro**

1. Start and finish a headless Codex invocation.
2. Record `finished_at` and `last_output_at` from `agency agent show <id> --json`.
3. Run `agency agent land <id> --apply --json`.
4. Re-read `agency agent show <id> --json`.

**Observed**

- `finished_at` changed from the runner completion time to the land time.
- `last_output_at` stayed at the original runner completion time.

**Expected**

- Runner timestamps should be immutable after the runner exits.
- Landing should update landing-specific fields only.

**Acceptance**

- [ ] `finished_at` remains the runner completion timestamp before and after
      `land` / `discard`
- [ ] landing updates only landing-specific metadata such as
      `landing_status`, `display_status`, and related sort/attention state

### 2. Codex file-change activity still degrades to `unknown runner event`

**Observed on:** `20260328191202-74f9`, `20260329051418-5f29`, `20260329052316-ac3e`

**Repro**

1. Start a headless Codex invocation that creates or updates files.
2. Inspect `agency agent history <id>` while the run is active.

**Observed**

- History shows `unknown runner event: item.started (unsupported item type)`
  around file creation/update activity.
- The event still carries checkpoint context, so it is not harmless noise: it
  becomes visible in `history` and may briefly affect `review`.

**Expected**

- Standard Codex file creation/update activity should normalize cleanly into a
  file-change/tool-activity row, or be suppressed from the default human view.

**Acceptance**

- [ ] Codex file-change activity no longer renders as
      `unknown runner event` in `history`, `history --last`, `show`, or `review`
- [ ] checkpoint association still works after the normalization change

### 3. Cursor todo-bookkeeping events leak into default human surfaces

**Observed on:** `20260328194136-6953`, `20260329053034-b9a5`

**Likely code surfaces**

- `internal/daemon/stream/cursor.go`
- `internal/daemon/read_turn_projection.go`
- `internal/tui/historypicker/turn.go`
- `internal/commands/agent.go`

**Repro**

1. Start a headless Cursor invocation with `seed_exhaustive.txt`.
2. While it is still running, queue follow-up 1 / 2 / 3.
3. Inspect:
   - `agency agent history <id>`
   - `agency agent history --last <id>`
   - `agency agent review <id>`
   - `agency agent restart <id> --history`

**Observed**

- Cursor emits `updateTodosToolCall` rows.
- Those rows become `unknown runner event: tool_call (unrecognized tool structure)`.
- Trailing todo-bookkeeping can become the "latest" turn for `history --last`,
  `review`, and `restart --history`.
- Follow-up context can appear twice: once as
  `inv_event:*:agency.followup_prompt`, then again later as runner-visible
  `[follow-up]` turns.

**Expected**

- Todo bookkeeping should either be normalized into a non-user-facing internal
  category or suppressed from default human surfaces.
- The latest meaningful turn should prefer the real assistant/follow-up close-out,
  not trailing bookkeeping.

**Acceptance**

- [ ] `updateTodosToolCall` no longer appears as `unknown runner event` in
      `history`, `review`, or `restart --history`
- [ ] `history --last`, `review`, and `restart --history` converge on the same
      latest assistant/follow-up turn
- [ ] follow-up prompt context appears exactly once in the default human surfaces,
      or any duplication is deliberate and explicitly labeled

### 4. Cursor restore/diff projection is unstable after checkpoint apply/discard

**Observed on:** `20260328194136-6953`

**Repro**

1. Run a Cursor seed invocation that creates checkpoint 1 for `notes.md` and
   checkpoint 2 for `search.txt`.
2. Run `agency checkpoint apply --invocation <id> 1`.
3. Run `agency agent diff <id>`.
4. Discard the invocation.
5. Re-read `show`, `history`, `review`, and `checkpoint ls`.

**Observed**

- After `checkpoint apply 1`, plain `agent diff` reported `(no changes)` even
  though checkpoint 1 clearly represented a visible file change.
- After restore/discard, the displayed latest-turn/checkpoint association drifted:
  a turn that had previously been tied to checkpoint 2 later surfaced with
  checkpoint 1.

**Expected**

- `checkpoint apply` should rewind the sandbox to the selected checkpoint state.
- Plain `agent diff` should reflect the restored sandbox contents.
- Turn/checkpoint projection should remain stable before and after discard.

**Acceptance**

- [ ] after `checkpoint apply <cp>`, plain `agent diff` reflects the restored
      sandbox state instead of `(no changes)` when files are present
- [ ] the same turn id keeps the same checkpoint association before and after
      discard/replay
- [ ] restored/latest checkpoint metadata stays consistent across `show`,
      `history`, `review`, `checkpoint ls`, and `diff`

### 5. `agent restart --history` has no viewport and becomes unreadable on long histories

**Observed on:** richer Cursor and Claude follow-up runs

**Likely code surface**

- `internal/tui/historypicker/model.go`

**Repro**

1. Run a richer multi-turn invocation with seed + queued follow-ups.
2. Run `agency agent restart <id> --history` in a normal terminal.
3. Navigate with `j` / `k`, `g`, `G`, or arrow keys.

**Observed**

- The picker renders past the bottom of the terminal.
- The selected row can move off-screen.
- The user can no longer tell what is selected without cycling back to a
  visible region.

**Expected**

- The picker should use the terminal height to clip/scroll content.
- The selected row should stay visible.

**Acceptance**

- [ ] long histories scroll inside the picker instead of rendering past the
      bottom of the screen
- [ ] the selected row remains visible while navigating
- [ ] the header/help/footer remain visible on long histories

### 6. Headed lifecycle is inconsistent after detach and stop

**Observed on:** `20260329043655-244d` and `20260329045111-09b1`

**Repro**

1. Start a detached headed invocation.
2. `agency agent enter <id>`, paste `docs/v2.1/s8/manual_prompts/headed_smoke.txt`,
   submit it, then detach.
3. Re-enter or shell into the sandbox and check for
   `tmp/s8-manual/headed-smoke.txt`.
4. Run `agency agent stop <id> --json`.
5. Re-read `agency agent show <id> --json`.

**Observed**

- The headed smoke prompt did not materialize the expected file during the pass.
- `agent stop` returned success, but `show --json` still reported
  `status: running` with `display_status: needs attention`.

**Expected**

- Detach/re-enter should not lose prompt execution continuity.
- A successful `agent stop` should move the invocation to a terminal state.

**Acceptance**

- [ ] the headed smoke prompt reproducibly creates
      `tmp/s8-manual/headed-smoke.txt` after submit + detach + re-entry
- [ ] `agent stop` transitions a headed invocation out of `running`
- [ ] headed `show` no longer reports `running` after a successful stop

### 7. `agent shell` reports child exit as `E_INTERNAL`

**Observed on:** headed smoke shell probe

**Likely code surface**

- `internal/commands/agent.go`

**Repro**

1. Run `agency agent shell <id>`.
2. Execute a normal failing command inside the shell, for example
   `ls tmp/s8-manual` when the path does not exist.
3. Exit the shell.

**Observed**

- `agent shell` returns `error_code: E_INTERNAL`
  with `shell exited with code 1`.

**Expected**

- A child command exiting non-zero is a normal shell outcome, not an internal error.

**Acceptance**

- [ ] non-zero child exits from `agent shell` surface as a typed shell/process
      failure rather than `E_INTERNAL`
- [ ] the original child exit code is preserved in a machine-readable way

### 8. Retained Claude log surfaces are still too blob-like to be useful

**Observed on:** discarded Claude runs, especially `20260328200047-d437`

**Repro**

1. Complete and discard a Claude headless invocation.
2. Re-run:
   - `agency agent logs <id>`
   - `agency agent logs <id> --kind stream`

**Observed**

- The retained logs are available, which is good.
- But they are still giant blobs that are hard to scan as human-facing surfaces.

**Expected**

- Retained replay should stay human-usable, especially for `--kind stream`.
- If raw replay is intentionally verbose, the default docs and CLI guidance
  should make it obvious which surface is the concise one for post-discard inspection.

**Acceptance**

- [ ] retained `logs --kind stream` remains line-oriented and scannable on large
      Claude runs
- [ ] docs or CLI guidance clearly direct users to the concise inspection surface
      when raw logs are intentionally verbose

### 9. `worktree rm` help text does not match untracked-dirt behavior

**Observed during:** early worktree smoke tests

**Repro**

1. Create a clean integration worktree.
2. Add an untracked file only.
3. Run `agency worktree rm <wt>`.
4. Repeat with a tracked modification.

**Observed**

- Tracked-file dirt correctly blocked removal.
- Untracked-only dirt did not block removal.
- The help text says removal fails when the worktree has "uncommitted changes",
  which reads as broader than tracked-only modifications.

**Expected**

- Help text and behavior should match.

**Acceptance**

- [ ] either `worktree rm` blocks untracked-only dirt
- [ ] or the help text is narrowed to say tracked modifications only
- [ ] tests cover both tracked-dirty and untracked-only cases

### 10. S8 docs currently describe a follow-up timing model the product does not support

**Observed while following:** `docs/v2.1/s8/s8_exhaustive_manual_test_matrix.md`

**Likely code surfaces**

- `internal/commands/agent.go`
- `internal/daemon/followup.go`
- `docs/v2.1/s8/s8_exhaustive_manual_test_matrix.md`
- related S8 PR briefs under `docs/v2.1/s8/s8_prs/`

**Repro**

1. Follow the current matrix literally:
   - let follow-up 1 complete
   - then send follow-up 2
2. If the invocation has already exited, run
   `agency agent chat <id> --prompt-file ... --json`.

**Observed**

- `agent chat` returns `E_INVOCATION_NOT_RUNNING`.
- The product only accepts follow-up prompts while the invocation is still live.
- The richer pass worked only after queuing follow-up 2 and 3 before the run exited.

**Expected**

- Docs/matrix should match product semantics.
- If the intended product behavior is "send follow-ups after completion", then
  the product needs a continuation/restart-backed path for that.

**Acceptance**

- [ ] the S8 matrix and PR briefs describe the timing requirement correctly
- [ ] or the product supports a documented post-completion continuation flow
- [ ] `agent chat` docs explicitly say follow-up prompts target a running invocation

### 11. `followup_multifile_verify.txt` is not portable on macOS

**Observed on:** Cursor and Claude follow-up 2

**Repro**

1. Run `docs/v2.1/s8/manual_prompts/followup_multifile_verify.txt` on macOS.
2. Observe the `nl -ba ...` verification step.

**Observed**

- The prompt triggered runner commentary about macOS `nl` behavior.
- The verification command is noisy enough to distract from the actual feature
  under test.

**Expected**

- Manual validation prompts should use commands that behave the same on macOS and Linux,
  or explicitly call out platform assumptions.

**Acceptance**

- [ ] replace the verification command with a portable variant
- [ ] the prompt runs deterministically on macOS and Linux without producing
      platform-specific recovery chatter

## Clarifications From This Pass

These came up during the run but are not bugs:

- `agency agent restart <id> --history` returning `E_ABORTED` is correct when
  the picker is canceled without a selection.
- `agency agent discard` is not full record deletion. Retained `show`, `history`,
  `logs`, and `checkpoint ls` after discard are expected.
- The negative remote-flow results were expected:
  - `worktree pr sync` returned `E_EMPTY_DIFF`
  - `worktree merge` returned `E_NO_PR`
  - no PR was created and nothing was merged
- On the richer follow-up runs, the latest turn and the latest checkpoint were
  sometimes different concepts. That is acceptable when the final follow-up
  changes no files, but the surfaces still need to agree on which checkpoint
  is associated with each turn.
