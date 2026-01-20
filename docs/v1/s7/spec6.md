# PR Spec: Improve Merge Conflict UX (Guidance + Resolve Helper)

## PR ID
pr-merge-conflict-ux

## Goal (strict)
When `agency merge` fails due to merge conflicts, the user must be given a clear, actionable path back to a mergeable state, without any automatic conflict resolution or history rewriting.

## Non-Goals (explicit)
- No automatic conflict resolution
- No automatic rebase or merge
- No AI-assisted resolution
- No repo-level policy config changes
- No schema version bumps
- No interactive TUI
- No changes to `gh pr merge` strategy defaults (remains `--squash`)

---

## Summary of Changes
This PR introduces:
1. **Enhanced merge conflict error output** (action card)
2. **New helper command:** `agency resolve <id>` (guidance-only)
3. **Explicit support for rebased branches via** `agency push --force-with-lease`
4. **Clear, consistent guidance that uses `agency open`, not `agency path`**

All behavior is additive and constitution-safe.

---

## User-Facing Behavior

### 1) `agency merge <id>` — conflict case

When GitHub reports `mergeable == CONFLICTING`:

- Command exits with `E_PR_NOT_MERGEABLE`
- Output includes:
  - PR URL (plain text, no markdown)
  - Base branch
  - Run branch
  - Worktree path
  - A **single recommended resolution strategy** (default: rebase)
  - Exact next steps, using the **same ref the user invoked**

**Example output (to stderr):**
```
error_code: E_PR_NOT_MERGEABLE
PR #93 has conflicts with main and cannot be merged.

pr: https://github.com/owner/repo/pull/93
base: main
branch: agency/feature-x-a3f2
worktree: /path/to/worktree

next:

1. agency open feature-x
2. git fetch origin
3. git rebase origin/main
4. resolve conflicts, then:
   git add -A && git rebase --continue
5. agency push feature-x --force-with-lease
6. agency merge feature-x

alt: cd "/path/to/worktree"
```

**Output conventions:**
- All output to stderr (after `error_code` line)
- Plain `key: value` format, no color, no markdown
- `alt:` line provides fallback if `agency open` fails

---

### Lock Release Invariant

The repo lock ordering must be:

1. Acquire lock
2. Read meta, query `gh pr view` for mergeability, update events/meta if any
3. **Release lock**
4. Print guidance

**Required invariant:** Lock is held only for the minimal critical section. Guidance printing must not depend on any mutations after lock release. No git mutations occur during error handling.

---

### 2) `agency resolve <id>` (new command)

**Purpose:** Provide the same guidance as above, on demand.

**Behavior:**
- Resolves `<id>` as usual (name, run_id, or prefix)
- If worktree **present**: prints the action card to **stdout**, exits `0`
- If worktree **missing**: prints partial guidance (PR URL, branch, base) to stderr, exits non-zero with `E_WORKTREE_MISSING`
- Opens nothing automatically
- Makes **no git changes**
- Does not require repo lock (read-only)

**Example (worktree present):**
```
agency resolve feature-x
```

Outputs to stdout:
```
pr: https://github.com/owner/repo/pull/93
base: main
branch: agency/feature-x-a3f2
worktree: /path/to/worktree

next:

1. agency open feature-x
2. git fetch origin
3. git rebase origin/main
4. resolve conflicts, then:
   git add -A && git rebase --continue
5. agency push feature-x --force-with-lease
6. agency merge feature-x

alt: cd "/path/to/worktree"
```

**Example (worktree missing):**
```
agency resolve feature-x
```

Outputs to stderr:
```
error_code: E_WORKTREE_MISSING
worktree archived or missing

pr: https://github.com/owner/repo/pull/93
base: main
branch: agency/feature-x-a3f2

hint: worktree no longer exists; resolve conflicts via GitHub web UI or restore locally
```

---

### 3) `agency push --force-with-lease`

**Why:** Required to support the default rebase resolution strategy safely.

**Behavior:**
- New flag: `--force-with-lease`
- When provided, uses:
```
git push --force-with-lease -u origin <branch>
```
- Never used implicitly
- Never inferred automatically

**Failure UX:**

If push fails due to non-fast-forward and flag is not provided, detect via stderr pattern matching:

**Detection rule:** If git push stderr contains any of:
- `non-fast-forward`
- `fetch first`
- `[rejected]` combined with `non-fast-forward`

Then print the hint. Otherwise print generic push failure.

**Example (non-fast-forward detected):**
```
error_code: E_GIT_PUSH_FAILED
push rejected (non-fast-forward)

hint: branch was rebased or amended; retry with:
  agency push <id> --force-with-lease
```

**Example (other push failure):**
```
error_code: E_GIT_PUSH_FAILED
git push failed

stderr: <truncated git stderr>
```

---

## Resolution Strategy Rules (v1)

- **Default resolution guidance strategy: rebase** (printed in `next:` steps)
- **Merge strategy default remains `--squash`** (for `gh pr merge`, unchanged)

These are separate concerns:
- "Resolution guidance" = how we tell users to update their branch
- "Merge strategy" = how `gh pr merge` combines commits

Alternative resolution strategies (merge target, intermediary branch) are **allowed** but:
- Not automated
- Not encoded as commands
- Not printed unless user requests help elsewhere

No configuration changes in this PR.

---

## Action Card Formatting

### Shared Helper Requirement

There **must be** a single function that renders the action card, used by both:
- `merge` conflict error path
- `resolve` command

This prevents drift between the two code paths.

**Suggested location:** `internal/ui/actioncard.go` (or `internal/render/conflict.go`)

### Action Card Inputs

The action card renderer requires:

| Input | Source | Fallback |
|-------|--------|----------|
| `ref` | The ref the user invoked (e.g., `feature-x`) | `run_id` if name unavailable |
| `pr_url` | From meta.json | Empty string (omit line) |
| `pr_number` | From meta.json | 0 (omit from message) |
| `base` | `meta.parent_branch` | Required |
| `branch` | `meta.branch` | Required |
| `worktree` | `meta.worktree_path` | Required for full card |

**Ref selection rule:** Use the **same ref the user invoked** in the printed commands. If resolution is ambiguous (e.g., user used prefix), fall back to `run_id` which is always unambiguous.

---

## Commands Added / Modified

### Added
- `agency resolve <id>`

### Modified
- `agency merge`
- `agency push`

No other commands touched.

---

## Error Codes

### New / Extended Usage
- `E_PR_NOT_MERGEABLE`
  - Now includes action card output (to stderr)
- `E_GIT_PUSH_FAILED`
  - Adds hint for `--force-with-lease` when non-fast-forward detected
- `E_WORKTREE_MISSING`
  - Used by `resolve` when worktree archived

No new error codes introduced.

---

## Implementation Constraints

- **Do not** run `git rebase` or `git merge` automatically
- **Do not** open editor automatically
- **Do not** modify worktree state in `resolve`
- **Do not** hold repo lock while printing guidance
- **Do not** add new persistence fields or files
- **Do not** change default merge strategy (`--squash`)
- **Do** use a single shared function for action card rendering
- **Do** use plain text output (no markdown, no ANSI colors)

---

## Files Expected to Change

- `internal/commands/merge.go`
- `internal/commands/push.go`
- `internal/commands/resolve.go` (new)
- `internal/ui/actioncard.go` (new, shared formatter)
- `internal/cli/dispatch.go` (register resolve command)
- CLI help text / usage docs (if centralized)

No changes to:
- persistence schemas
- agency.json schema
- config.json schema

---

## Acceptance Criteria

### Merge conflict UX
- [ ] Merge conflict prints to stderr
- [ ] Output includes `pr:` (plain URL, no markdown)
- [ ] Output includes `base:`
- [ ] Output includes `branch:`
- [ ] Output includes `worktree:`
- [ ] Output includes `next:` section with numbered steps
- [ ] Output includes `alt:` with cd fallback
- [ ] Commands in `next:` use the same ref user invoked
- [ ] Repo lock is released before guidance printing
- [ ] No git mutations during error handling

### Resolve command
- [ ] `agency resolve <id>` prints guidance to stdout
- [ ] Output matches merge's `next:` section exactly (string-comparable)
- [ ] No git commands executed
- [ ] Works for any run with present worktree
- [ ] If worktree missing: prints partial guidance to stderr, exits with `E_WORKTREE_MISSING`
- [ ] Does not require repo lock

### Push behavior
- [ ] `agency push --force-with-lease` uses `git push --force-with-lease -u origin <branch>`
- [ ] Non-fast-forward failure detected via stderr pattern matching
- [ ] Hint printed only when pattern matches (not on all push failures)
- [ ] No automatic force push ever occurs
- [ ] Help text updated with new flag

### Shared formatter
- [ ] Single function renders action card
- [ ] Used by both merge and resolve
- [ ] Accepts ref, pr_url, base, branch, worktree as inputs

---

## Out of Scope (Future Work)

- Repo-level conflict policy config
- Listing conflicting files automatically
- AI-assisted conflict resolution
- Auto-retry merge after resolution
- Interactive conflict mode
- Colored output

---

## CI / Test Notes

- Manual test matrix:
  - conflicting PR → merge → guidance shown (stderr)
  - resolve command prints same guidance (stdout)
  - rebase + push without flag → fails with hint
  - rebase + push with `--force-with-lease` → succeeds
  - resolve with archived worktree → partial guidance + E_WORKTREE_MISSING
- Unit tests:
  - action card formatting (inputs → expected string)
  - non-fast-forward stderr pattern detection
  - flag parsing for `--force-with-lease`
- Integration tests:
  - merge conflict output includes required keys
  - resolve output matches merge's next section exactly
