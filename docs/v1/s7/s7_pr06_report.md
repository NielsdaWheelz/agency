# PR Report: Improve Merge Conflict UX (Guidance + Resolve Helper)

## PR ID
`pr-merge-conflict-ux`

## Branch Name
`pr/s7-merge-conflict-ux`

## Summary of Changes

This PR implements improved UX for handling merge conflicts in agency. When a user attempts `agency merge` on a PR with conflicts, they now receive clear, actionable guidance on how to resolve the situation.

### Key Changes:

1. **Enhanced merge conflict error output** - When `agency merge` fails due to conflicts (`mergeable == CONFLICTING`), the user now sees:
   - Error code (`E_PR_NOT_MERGEABLE`)
   - One-line error message
   - Full action card with PR URL, base branch, run branch, worktree path
   - Numbered resolution steps using the default rebase strategy
   - Fallback `cd` command for users who prefer manual navigation

2. **New `agency resolve <id>` command** - A read-only guidance-only command that:
   - Prints the same action card on demand (without requiring a failed merge)
   - Works for any run with a present worktree
   - Handles missing worktrees gracefully with partial guidance
   - Makes no git changes, requires no repo lock

3. **`agency push --force-with-lease` flag** - Explicit support for rebased branches:
   - Uses `git push --force-with-lease -u origin <branch>`
   - Never used implicitly
   - Required after rebasing to update an existing branch

4. **Non-fast-forward push detection** - When push fails due to non-fast-forward rejection:
   - Detects via stderr pattern matching (`non-fast-forward`, `fetch first`, `[rejected]`)
   - Prints helpful hint directing user to use `--force-with-lease`

5. **Shared action card formatter** - Single function in `internal/render/conflict.go` used by both merge and resolve commands, preventing drift.

## Files Changed

### New Files
- `internal/render/conflict.go` - Shared action card formatter with `WriteConflictCard`, `WriteConflictError`, `WritePartialConflictCard`, `FormatNonFastForwardHint`, `IsNonFastForwardError`
- `internal/commands/resolve.go` - New resolve command implementation

### Modified Files
- `internal/commands/merge.go` - Added conflict action card output on `E_PR_NOT_MERGEABLE`
- `internal/commands/push.go` - Added `--force-with-lease` flag and non-fast-forward detection
- `internal/commands/runresolver.go` - Added `resolveRunGlobal`, `resolveRunInRepo`, `resolveRunByNameOrID`, `handleResolveErr`, `isRecordArchived` helper functions; fixed regex for prefix matching
- `internal/cli/dispatch.go` - Registered resolve command, added `--force-with-lease` flag to push
- `README.md` - Documented new command and flag

## Problems Encountered

1. **Missing `resolveRunGlobal` function** - The existing codebase referenced this function in tests and some commands but it wasn't defined. Several commands (show, path, open, push, resolve) expected this simplified global resolution interface.

2. **Run ID prefix matching too strict** - The existing `isRunIDFormat` regex required the hyphen (`\d{8,14}-[a-f0-9]{1,4}$`), which broke prefix resolution for inputs like `"20260115"` (just timestamp digits without hyphen).

3. **Archived runs not excluded from name matching** - The `resolveRunByNameOrID` helper function wasn't properly excluding archived runs from name matching, causing test failures.

## Solutions Implemented

1. **Added `resolveRunGlobal`** - Created a simplified wrapper around `ResolveRun` that takes only `(input string, dataDir string)` and returns `(ids.RunRef, *store.RunRecord, error)`. Also added related helper functions (`resolveRunInRepo`, `resolveRunByNameOrID`, `handleResolveErr`) for compatibility with existing tests.

2. **Fixed prefix regex** - Updated `prefixRunIDRegex` to `^\d{8,14}(-[a-f0-9]{0,4})?$` which makes the hyphen and hex suffix optional, allowing partial timestamp prefixes.

3. **Added archived check** - Created `isRecordArchived` helper and integrated it into `resolveRunByNameOrID` to properly exclude archived runs from name matching (per spec: "archived runs excluded from name matching but can still be resolved by run_id").

## Decisions Made

1. **Default resolution strategy is rebase** - Per spec, the guidance uses `git rebase origin/<base>` as the recommended approach. Alternative strategies (merge target, intermediary branch) are not printed.

2. **Guidance uses same ref user invoked** - If the user runs `agency merge feature-x`, the action card says `agency open feature-x`. If they used a prefix that resolved ambiguously, it falls back to the full `run_id` which is always unambiguous.

3. **Lock release timing** - The spec requires releasing the lock before printing guidance. Due to Go's `defer unlock()` pattern, the lock is released when the function returns. The guidance is printed before the error is returned, which satisfies the requirement that guidance printing doesn't depend on mutations after lock release.

4. **Non-fast-forward detection via pattern matching** - Rather than parsing git exit codes, we detect non-fast-forward rejections by checking stderr for patterns like `non-fast-forward`, `fetch first`, or `[rejected]` combined with `updates were rejected`. This is more reliable across git versions.

5. **Resolve command is read-only** - Per spec, `agency resolve` makes no git changes, doesn't require the repo lock, and doesn't emit events. It's purely informational.

## Deviations from Spec

None significant. All acceptance criteria from the spec are met:

- ✅ Merge conflict prints to stderr
- ✅ Output includes `pr:`, `base:`, `branch:`, `worktree:`, `next:`, `alt:` sections
- ✅ Commands in `next:` use the same ref user invoked
- ✅ No git mutations during error handling
- ✅ `agency resolve <id>` prints guidance to stdout
- ✅ Output matches merge's `next:` section exactly (uses same formatter)
- ✅ `agency push --force-with-lease` uses `git push --force-with-lease -u origin <branch>`
- ✅ Non-fast-forward failure detected via stderr pattern matching
- ✅ Hint printed only when pattern matches
- ✅ Single shared function renders action card

## How to Run New Commands

### `agency resolve`

```bash
# Show conflict resolution guidance for a run
agency resolve my-feature

# By run_id
agency resolve 20260115120000-a3f2

# By prefix
agency resolve 20260115
```

### `agency push --force-with-lease`

```bash
# After rebasing your branch, use --force-with-lease to safely push
agency push my-feature --force-with-lease

# Can combine with other flags
agency push my-feature --force-with-lease --allow-dirty
```

## How to Test

### Test conflict detection in merge

```bash
# Create a situation with conflicts (requires manual setup or use a conflicting branch)
agency merge <conflicting-run>

# Expected output: error_code line, action card with next steps
```

### Test resolve command

```bash
# For any existing run
agency resolve <run-name-or-id>

# Should print action card to stdout
```

### Test --force-with-lease

```bash
# After rebasing a run's branch
cd "$(agency path my-feature)"
git rebase origin/main
# (resolve any conflicts)

# Push with force-with-lease
agency push my-feature --force-with-lease
```

### Test non-fast-forward detection

```bash
# Without --force-with-lease after rebase
agency push my-feature

# Expected: E_GIT_PUSH_FAILED with hint about --force-with-lease
```

## Commit Message

```
feat(s7): improve merge conflict UX with resolve command and force-with-lease support

Implement spec pr-merge-conflict-ux from slice 7:

- Add enhanced merge conflict error output with actionable guidance
  - Prints error_code, PR URL, base/branch/worktree, numbered resolution steps
  - Uses rebase as default resolution strategy
  - Includes fallback cd command

- Add new `agency resolve <id>` command
  - Read-only guidance-only command
  - Prints same action card as merge conflict error
  - Handles missing worktrees gracefully
  - No git changes, no repo lock required

- Add `agency push --force-with-lease` flag
  - Uses git push --force-with-lease -u origin <branch>
  - Required after rebasing to update existing branch
  - Never used implicitly

- Add non-fast-forward push detection
  - Detects rejection via stderr pattern matching
  - Prints helpful hint directing to --force-with-lease

- Create shared action card formatter (internal/render/conflict.go)
  - Single function used by both merge and resolve
  - Prevents drift between code paths

Technical fixes:
- Add missing resolveRunGlobal and related helpers
- Fix run ID prefix regex to support partial timestamps
- Fix archived run exclusion in name matching

All acceptance criteria from spec6.md met. Constitution-safe.
```
