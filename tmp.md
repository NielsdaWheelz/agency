codex resume 019cdba0-b5ef-7331-9d1c-a2aadb5e9e57

cd "/Users/nnandal/Library/Application Support/agency/repos/769749d77af0806f/worktrees/20260311064003-1d7a"

# anomalies

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
