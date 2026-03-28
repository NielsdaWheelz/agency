# Slice S8: Exhaustive Manual Agent / Worktree Validation Matrix

This is the manual test package for the current branch binary, not the older installed system binary.

It is deliberately broader than the earlier S8 runner closure pass. The goal here is to exercise:

- every `agent` command family
- every `worktree` command family
- `checkpoint`
- `watch`
- the current Claude / Codex / Cursor runner paths
- durability, replay, restart, follow-up, diff, review, checkpoint, and watch consistency

This matrix is intended for an interactive terminal session with a human in the loop.

## Prompt Files

- main seed:
  - `docs/v2.1/s8/manual_prompts/seed_exhaustive.txt`
- follow-up 1, search + append:
  - `docs/v2.1/s8/manual_prompts/followup_exhaustive.txt`
- follow-up 2, multi-file write + verification:
  - `docs/v2.1/s8/manual_prompts/followup_multifile_verify.txt`
- follow-up 3, deterministic failure:
  - `docs/v2.1/s8/manual_prompts/followup_failure.txt`
- follow-up 4, optional native web action:
  - `docs/v2.1/s8/manual_prompts/followup_web_optional.txt`
- isolated failure-path run:
  - `docs/v2.1/s8/manual_prompts/failure_exhaustive.txt`
- headed runner paste-in prompt:
  - `docs/v2.1/s8/manual_prompts/headed_smoke.txt`

## Scope Notes

- Use the local branch binary `./agency`.
- `worktree pr sync` and `worktree merge` are remote-mutating operations. Run them only on a disposable test worktree and only if this repo has a GitHub origin and `gh` auth configured.
- `agent open` launches the configured editor for real. Use it only when you are comfortable opening the sandbox in your editor.
- `worktree open` can be safely smoke-tested with `--editor /usr/bin/true`.

## Build And Variables

Build the current branch binary first:

```bash
GOCACHE=/tmp/agency-gocache go build -o ./agency ./cmd/agency
```

Set shared variables:

```bash
export AGENCY_BIN=./agency
export TEST_TAG="s8-manual-$(date +%Y%m%d-%H%M%S)"

export WT_CODEX="${TEST_TAG}-codex"
export WT_CURSOR="${TEST_TAG}-cursor"
export WT_CLAUDE="${TEST_TAG}-claude"
export WT_HEADED="${TEST_TAG}-headed"
export WT_RM="${TEST_TAG}-rm"
export WT_REMOTE="${TEST_TAG}-remote"
```

## Phase 0: Quick CLI Contract Probes

These should fail fast with helpful usage errors or guidance:

```bash
$AGENCY_BIN agent ls --watch --json
$AGENCY_BIN worktree ls --watch --json
$AGENCY_BIN watch --interval 100ms
$AGENCY_BIN agent start --worktree "$WT_CODEX" --headless --runner codex
$AGENCY_BIN agent start --worktree "$WT_CODEX" --headless --runner codex --prompt "hi" --prompt-file docs/v2.1/s8/manual_prompts/seed_short.txt
$AGENCY_BIN agent start --worktree "$WT_CURSOR" --headless --runner cursor --effort medium --prompt "hi"
```

Expected:

- `agent ls --watch` and `worktree ls --watch` should point you to `agency watch`
- `watch --interval 100ms` should reject the interval
- headless start without prompt should fail
- `--prompt` plus `--prompt-file` should fail
- Cursor with `--effort` should fail validation before launch

## Phase 1: Worktree Command Matrix

Create worktrees:

```bash
$AGENCY_BIN worktree create --name "$WT_CODEX"
$AGENCY_BIN worktree create --name "$WT_CURSOR"
$AGENCY_BIN worktree create --name "$WT_CLAUDE"
$AGENCY_BIN worktree create --name "$WT_HEADED"
$AGENCY_BIN worktree create --name "$WT_RM" --open --editor /usr/bin/true
$AGENCY_BIN worktree create --name "$WT_REMOTE" --parent "$(git branch --show-current)"
```

List and inspect:

```bash
$AGENCY_BIN worktree ls
$AGENCY_BIN worktree ls --json
$AGENCY_BIN worktree ls --all
$AGENCY_BIN worktree ls --all-repos

$AGENCY_BIN worktree show "$WT_CODEX"
$AGENCY_BIN worktree show "$WT_CODEX" --json
$AGENCY_BIN worktree path "$WT_CODEX"
$AGENCY_BIN worktree open "$WT_CODEX" --editor /usr/bin/true
```

Interactive shell smoke:

```bash
$AGENCY_BIN worktree shell "$WT_CODEX"
```

Inside the shell, run:

```bash
pwd
git branch --show-current
exit
```

Dirty-remove negative and force-remove positive:

```bash
export WT_RM_PATH="$($AGENCY_BIN worktree path "$WT_RM")"
mkdir -p "$WT_RM_PATH/tmp"
printf 'rm-probe\n' >> "$WT_RM_PATH/tmp/s8-rm-probe.txt"

$AGENCY_BIN worktree rm "$WT_RM" --yes
$AGENCY_BIN worktree rm "$WT_RM" --force --yes
$AGENCY_BIN worktree ls --all
```

Expected:

- first `rm` should fail because the worktree is dirty
- second `rm --force --yes` should succeed
- `worktree ls --all` should show the archived record

Optional `--repo` coverage:

```bash
$AGENCY_BIN worktree ls --json
# copy a repo id from the JSON into REPO_ID
export REPO_ID=<paste repo id>

$AGENCY_BIN worktree show "$WT_CODEX" --repo "$REPO_ID" --json
```

## Phase 2: Shared Headless Success Flow

Repeat this full flow for `codex`, `cursor`, and `claude-code`.

Common commands while an invocation is live:

```bash
$AGENCY_BIN agent show "$INV"
$AGENCY_BIN agent show "$INV" --json
$AGENCY_BIN agent ls
$AGENCY_BIN agent ls --json
$AGENCY_BIN agent ls --all
$AGENCY_BIN agent ls --worktree "$WT"
$AGENCY_BIN agent history "$INV"
$AGENCY_BIN agent history "$INV" --json
$AGENCY_BIN agent history "$INV" --json --limit 5
$AGENCY_BIN agent logs "$INV"
$AGENCY_BIN agent logs "$INV" --offset 64
$AGENCY_BIN agent logs "$INV" --kind stream
$AGENCY_BIN agent logs "$INV" --kind stderr
$AGENCY_BIN agent logs "$INV" --follow
$AGENCY_BIN watch --interval 250ms
```

`agent logs --follow` is interactive. Exit it with `Ctrl-C` after you have seen streaming output.

Inside `agency watch`, check:

- repositories / worktrees / invocations panels populate
- latest activity and review status are coherent with `agent show` / `agent review`
- `up` / `down`, `k` / `j`, `g`, `G`, `r`, and `q`
- `enter` on a headed invocation later in Phase 3
- `o` on a selected invocation sandbox
- `p` only on a disposable remote-ready worktree later in Phase 4

After completion, run:

```bash
$AGENCY_BIN agent history "$INV" --last
$AGENCY_BIN agent history "$INV" --json --last
$AGENCY_BIN agent history "$INV" --json --limit 5
$AGENCY_BIN checkpoint ls --invocation "$INV"
$AGENCY_BIN checkpoint ls --invocation "$INV" --json
$AGENCY_BIN agent review "$INV"
$AGENCY_BIN agent review "$INV" --json
```

If `jq` is available, extract the latest turn id with:

```bash
export LATEST_TURN="$($AGENCY_BIN agent review "$INV" --json | jq -r '.navigation.latest_turn_id')"
```

If `jq` is not available, copy the `latest_turn_id` manually from `agent review --json`.

Then run:

```bash
$AGENCY_BIN agent diff "$INV" --turn "$LATEST_TURN"
$AGENCY_BIN agent diff "$INV" --json --turn "$LATEST_TURN"
$AGENCY_BIN agent diff "$INV" --turn-range "prompt_seed..$LATEST_TURN"
$AGENCY_BIN agent restart "$INV" --history
```

Also run these validation probes once you have a real invocation id:

```bash
$AGENCY_BIN agent chat "$INV" --json
$AGENCY_BIN agent chat "$INV" --prompt "hi" --prompt-file docs/v2.1/s8/manual_prompts/followup_short.txt --json
$AGENCY_BIN agent restart "$INV" --json
$AGENCY_BIN agent restart "$INV" --checkpoint 0 --json
$AGENCY_BIN agent restart "$INV" --checkpoint 1 --env INVALID --json
$AGENCY_BIN agent diff "$INV" --turn "$LATEST_TURN" --turn-range "prompt_seed..$LATEST_TURN"
```

Expected:

- `history`, `checkpoint ls`, `review`, and `diff --turn` agree about the latest meaningful turn
- `logs`, `logs --kind stream`, and `logs --kind stderr` all remain readable
- validation probes return typed usage errors or JSON envelopes rather than ambiguous failures

Optional selector / pagination coverage:

```bash
# use a unique invocation-id prefix or the invocation name from --name
$AGENCY_BIN agent show "${INV%%-*}"
$AGENCY_BIN agent history "${INV%%-*}" --last

# if jq is available and next_cursor is non-empty
export PAGE_CURSOR="$($AGENCY_BIN agent history "$INV" --json --limit 5 | jq -r '.next_cursor // empty')"
[ -n "$PAGE_CURSOR" ] && $AGENCY_BIN agent history "$INV" --json --limit 5 --cursor "$PAGE_CURSOR"
```

### Codex Success Flow

Start:

```bash
$AGENCY_BIN agent start \
  --worktree "$WT_CODEX" \
  --headless \
  --runner codex \
  --name "${TEST_TAG}-codex" \
  --prompt-file docs/v2.1/s8/manual_prompts/seed_exhaustive.txt \
  --json
```

Copy the `invocation_id` into:

```bash
export CODEX_INV=<paste invocation_id>
export INV="$CODEX_INV"
export WT="$WT_CODEX"
```

While the seed command is still sleeping, run the common live-surface checks above and send follow-up 1:

```bash
$AGENCY_BIN agent chat "$CODEX_INV" --prompt-file docs/v2.1/s8/manual_prompts/followup_exhaustive.txt --json
```

After follow-up 1 completes, inspect:

```bash
$AGENCY_BIN agent history "$CODEX_INV" --last
$AGENCY_BIN checkpoint ls --invocation "$CODEX_INV"
$AGENCY_BIN agent review "$CODEX_INV"
```

Send follow-up 2:

```bash
$AGENCY_BIN agent chat "$CODEX_INV" --prompt-file docs/v2.1/s8/manual_prompts/followup_multifile_verify.txt --json
```

After follow-up 2 completes, use that turn for `agent diff --turn` and `agent restart --history`.

Send follow-up 3:

```bash
$AGENCY_BIN agent chat "$CODEX_INV" --prompt-file docs/v2.1/s8/manual_prompts/followup_failure.txt --json
```

After follow-up 3 completes, inspect:

```bash
$AGENCY_BIN agent history "$CODEX_INV" --last
$AGENCY_BIN agent logs "$CODEX_INV" --kind stream
$AGENCY_BIN agent logs "$CODEX_INV" --kind stderr
$AGENCY_BIN agent review "$CODEX_INV"
```

Optional follow-up 4:

```bash
$AGENCY_BIN agent chat "$CODEX_INV" --prompt-file docs/v2.1/s8/manual_prompts/followup_web_optional.txt --json
```

After the shared inspection flow, keep this invocation for `land`:

```bash
$AGENCY_BIN agent path "$CODEX_INV"
$AGENCY_BIN agent shell "$CODEX_INV"
```

Inside the sandbox shell, run:

```bash
pwd
ls tmp/s8-manual
exit
```

Then land and verify retained surfaces:

```bash
$AGENCY_BIN agent land "$CODEX_INV" --json
$AGENCY_BIN agent show "$CODEX_INV" --json
$AGENCY_BIN agent history "$CODEX_INV"
$AGENCY_BIN agent logs "$CODEX_INV"
$AGENCY_BIN agent logs "$CODEX_INV" --kind stream
$AGENCY_BIN checkpoint ls --invocation "$CODEX_INV"
```

### Cursor Success Flow

Start:

```bash
$AGENCY_BIN agent start \
  --worktree "$WT_CURSOR" \
  --headless \
  --runner cursor \
  --name "${TEST_TAG}-cursor" \
  --prompt-file docs/v2.1/s8/manual_prompts/seed_exhaustive.txt \
  --json
```

Copy the `invocation_id` into:

```bash
export CURSOR_INV=<paste invocation_id>
export INV="$CURSOR_INV"
export WT="$WT_CURSOR"
```

While the seed command is still sleeping, run the common live-surface checks above and send follow-up 1:

```bash
$AGENCY_BIN agent chat "$CURSOR_INV" --prompt-file docs/v2.1/s8/manual_prompts/followup_exhaustive.txt --json
```

Then run the same follow-up sequence as Codex, one turn at a time, letting each turn settle before sending the next prompt:

```bash
$AGENCY_BIN agent chat "$CURSOR_INV" --prompt-file docs/v2.1/s8/manual_prompts/followup_multifile_verify.txt --json
$AGENCY_BIN agent chat "$CURSOR_INV" --prompt-file docs/v2.1/s8/manual_prompts/followup_failure.txt --json
$AGENCY_BIN agent chat "$CURSOR_INV" --prompt-file docs/v2.1/s8/manual_prompts/followup_web_optional.txt --json
```

After the shared inspection flow, explicitly test checkpoint restore:

```bash
$AGENCY_BIN checkpoint ls --invocation "$CURSOR_INV" --json
```

Pick a checkpoint id from the JSON or human list, then run:

```bash
$AGENCY_BIN checkpoint apply --invocation "$CURSOR_INV" <checkpoint_id>
$AGENCY_BIN agent diff "$CURSOR_INV"
```

Then discard and verify retained surfaces:

```bash
$AGENCY_BIN agent discard "$CURSOR_INV" --json
$AGENCY_BIN agent show "$CURSOR_INV" --json
$AGENCY_BIN agent history "$CURSOR_INV"
$AGENCY_BIN agent logs "$CURSOR_INV"
$AGENCY_BIN agent logs "$CURSOR_INV" --kind stream
$AGENCY_BIN checkpoint ls --invocation "$CURSOR_INV"
```

### Claude Success Flow

Start:

```bash
$AGENCY_BIN agent start \
  --worktree "$WT_CLAUDE" \
  --headless \
  --runner claude-code \
  --name "${TEST_TAG}-claude" \
  --prompt-file docs/v2.1/s8/manual_prompts/seed_exhaustive.txt \
  --json
```

Copy the `invocation_id` into:

```bash
export CLAUDE_INV=<paste invocation_id>
export INV="$CLAUDE_INV"
export WT="$WT_CLAUDE"
```

While the seed command is still sleeping, run the common live-surface checks above and send follow-up 1:

```bash
$AGENCY_BIN agent chat "$CLAUDE_INV" --prompt-file docs/v2.1/s8/manual_prompts/followup_exhaustive.txt --json
```

Then run the same follow-up sequence as Codex, one turn at a time, letting each turn settle before sending the next prompt:

```bash
$AGENCY_BIN agent chat "$CLAUDE_INV" --prompt-file docs/v2.1/s8/manual_prompts/followup_multifile_verify.txt --json
$AGENCY_BIN agent chat "$CLAUDE_INV" --prompt-file docs/v2.1/s8/manual_prompts/followup_failure.txt --json
$AGENCY_BIN agent chat "$CLAUDE_INV" --prompt-file docs/v2.1/s8/manual_prompts/followup_web_optional.txt --json
```

After the shared inspection flow, explicitly test the prior lifecycle bug:

```bash
$AGENCY_BIN agent stop "$CLAUDE_INV" --json
$AGENCY_BIN agent show "$CLAUDE_INV" --json
$AGENCY_BIN agent review "$CLAUDE_INV" --json
```

The invocation should remain successful after completion. Then discard and verify retained surfaces:

```bash
$AGENCY_BIN agent discard "$CLAUDE_INV" --json
$AGENCY_BIN agent show "$CLAUDE_INV" --json
$AGENCY_BIN agent history "$CLAUDE_INV"
$AGENCY_BIN agent logs "$CLAUDE_INV"
$AGENCY_BIN agent logs "$CLAUDE_INV" --kind stream
$AGENCY_BIN checkpoint ls --invocation "$CLAUDE_INV"
```

Optional isolated failure run:

If you want a separate failure-only invocation in addition to the in-band failure follow-up above:

```bash
$AGENCY_BIN agent start \
  --worktree "$WT_CODEX" \
  --headless \
  --runner codex \
  --name "${TEST_TAG}-codex-fail" \
  --prompt-file docs/v2.1/s8/manual_prompts/failure_exhaustive.txt \
  --json
```

Inspect `show`, `history`, `logs`, `logs --kind stream`, `logs --kind stderr`, `review`, then discard it.

## Phase 3: Headed Command Smoke

Pick the headed runner you trust most for interactive auth. Default:

```bash
export HEADED_RUNNER=claude-code
```

Start a detached headed invocation:

```bash
$AGENCY_BIN agent start \
  --worktree "$WT_HEADED" \
  --runner "$HEADED_RUNNER" \
  --detached \
  --name "${TEST_TAG}-headed-stop" \
  --json
```

Copy the `invocation_id` into:

```bash
export HEADED_STOP_INV=<paste invocation_id>
```

Inspect and navigate:

```bash
$AGENCY_BIN agent show "$HEADED_STOP_INV" --json
$AGENCY_BIN agent path "$HEADED_STOP_INV"
$AGENCY_BIN agent enter "$HEADED_STOP_INV"
```

Once attached, paste the contents of `docs/v2.1/s8/manual_prompts/headed_smoke.txt` into the runner. Then detach with `Ctrl+b d`.

Re-enter through the compatibility alias:

```bash
$AGENCY_BIN agent attach "$HEADED_STOP_INV"
```

Detach again, then run:

```bash
$AGENCY_BIN agent shell "$HEADED_STOP_INV"
```

Inside the shell:

```bash
pwd
ls tmp/s8-manual
exit
```

Optional real-editor smoke:

```bash
$AGENCY_BIN agent open "$HEADED_STOP_INV"
```

Graceful stop:

```bash
$AGENCY_BIN agent stop "$HEADED_STOP_INV" --json
$AGENCY_BIN agent show "$HEADED_STOP_INV" --json
```

Start a second detached headed invocation for force-kill:

```bash
$AGENCY_BIN agent start \
  --worktree "$WT_HEADED" \
  --runner "$HEADED_RUNNER" \
  --detached \
  --name "${TEST_TAG}-headed-kill" \
  --json
```

Copy the `invocation_id` into:

```bash
export HEADED_KILL_INV=<paste invocation_id>
```

Then:

```bash
$AGENCY_BIN agent kill "$HEADED_KILL_INV" --json
$AGENCY_BIN agent show "$HEADED_KILL_INV" --json
```

## Phase 4: Remote Worktree Flow

Only run this if the repo has a GitHub `origin`, `gh auth status` succeeds, and you are willing to push and possibly merge a disposable test branch.

Use the landed Codex worktree from Phase 2 if it contains the change you want to ship, or use `$WT_REMOTE` with a small manual edit.

Optional preflight:

```bash
git config --get remote.origin.url
gh auth status
```

Then:

```bash
$AGENCY_BIN worktree show "$WT_CODEX" --json
$AGENCY_BIN worktree update "$WT_CODEX" --json
$AGENCY_BIN worktree pr sync "$WT_CODEX" --json
```

Also test the watch shortcut on the selected Codex invocation or worktree:

- open `agency watch`
- move selection to the landed/disposable worktree invocation
- press `p`

If you are explicitly willing to merge the disposable branch:

```bash
$AGENCY_BIN worktree merge "$WT_CODEX" --yes --json
```

## Phase 5: Cleanup

Archive/remove clean disposable worktrees when you are done:

```bash
$AGENCY_BIN worktree rm "$WT_CODEX" --yes
$AGENCY_BIN worktree rm "$WT_CURSOR" --yes
$AGENCY_BIN worktree rm "$WT_CLAUDE" --yes
$AGENCY_BIN worktree rm "$WT_HEADED" --yes
$AGENCY_BIN worktree rm "$WT_REMOTE" --yes
```

Then inspect archived state:

```bash
$AGENCY_BIN worktree ls --all
$AGENCY_BIN agent ls --all
```

Optional explicit `--repo` checks after you know `REPO_ID`:

```bash
$AGENCY_BIN agent show "$CODEX_INV" --repo "$REPO_ID" --json
$AGENCY_BIN agent history "$CURSOR_INV" --repo "$REPO_ID" --json
$AGENCY_BIN checkpoint ls --invocation "$CLAUDE_INV" --repo "$REPO_ID" --json
```

## What To Record While We Run This

For each runner, keep:

- invocation id
- runner name
- whether the seed run completed successfully
- whether follow-up 1 / 2 / 3 / optional 4 completed successfully
- latest turn id used for `diff --turn`
- checkpoint ids visible in `checkpoint ls`
- whether `restart --history` looked correct and selected the expected checkpoint
- whether logs remained readable after land or discard
- any places where `history`, `review`, `show`, `checkpoint ls`, `diff`, or `watch` disagreed

For worktree and headed flows, keep:

- whether `open`, `shell`, `path`, `enter`, and `attach` resolved the correct path or session
- whether `rm` dirty protection and `--force` behaved correctly
- whether `pr sync`, `update`, and `merge` were available and behaved correctly in your environment
