# Agency v1: Dirty Worktree Gating (Spec 2)

This spec introduces strict dirty-worktree gating for `agency push`, `agency merge`, and `agency clean`.
Default behavior is a hard block with an explicit override flag. This is a safety-first change that
prevents pushing/merging/cleaning when the worktree contains uncommitted changes.

---

## 1) Goals

- Prevent unsafe operations when the run worktree is dirty.
- Require an explicit override (`--allow-dirty`) to proceed.
- Surface the dirty context (`git status --porcelain --untracked-files=all` output) so the user can decide quickly.
- Preserve existing typed confirmations for merge/clean.
- Emit a structured event when the dirty override is used.

---

## 2) Non-Goals

- Changing dirty detection semantics beyond `git status --porcelain --untracked-files=all`.
- Bypassing `.gitignore` (ignored files remain ignored).
- Changing semantics of existing `--force` flags.
- Adding interactive prompts for dirty status (no typed token gate for dirtiness).
- Auto-stashing or auto-committing.

---

## 3) Definitions

**dirty worktree**: `git status --porcelain --untracked-files=all` output is non-empty for the run
worktree path. This includes untracked files and respects `.gitignore` (ignored files are excluded).

**dirty status output**: the literal stdout of `git status --porcelain --untracked-files=all` (trimmed only for trailing newline),
printed as context lines and stored in events when `--allow-dirty` is used.

---

## 4) CLI Surface

### New flag (all three commands)

```
--allow-dirty   allow operation even if run worktree has uncommitted changes
```

Applies to:
- `agency push <id> [--allow-dirty] [--force]`
- `agency merge <id> [--allow-dirty] [--force] [--squash|--merge|--rebase]`
- `agency clean <id> [--allow-dirty]`

`--force` semantics remain unchanged:
- `push`: bypass report gate only (missing/empty report)
- `merge`: bypass verify-failed prompt only

---

## 5) Error Code

Add a new public error code:

- `E_DIRTY_WORKTREE` — run worktree has uncommitted changes

Error message (exact):

```
E_DIRTY_WORKTREE: worktree has uncommitted changes; use --allow-dirty to proceed
```

---

## 6) Output Contract

### When dirty and no `--allow-dirty`

Command must:
1. Print error line to stderr (exact format above). This must be the first stderr output for the command.
2. Print dirty context to stderr (see below).
3. Exit non-zero with `E_DIRTY_WORKTREE`.

### When dirty and `--allow-dirty` is set

Command must:
1. Print warning line to stderr:

```
warning: worktree has uncommitted changes; proceeding due to --allow-dirty
```

2. Print dirty context to stderr (see below).
3. Continue with normal flow.

### Dirty context format

```
dirty_status:
<line 1 from git status --porcelain --untracked-files=all>
<line 2 from git status --porcelain --untracked-files=all>
...
```

Notes:
- Print the full porcelain output (no truncation).
- If the command is blocking, print the context after the error line.
- If the command is proceeding (allow-dirty), print after the warning line.
- Do not add extra prefixes to the porcelain lines.

---

## 7) Behavior by Command

### 7.1) `agency push`

**Gate placement**: after repo lock + worktree existence checks, before gh auth, fetch, or any network
effects, before report gating, and before any stderr output.

**Algorithm (inserted step)**:
1. Check worktree cleanliness using `git status --porcelain --untracked-files=all` in the worktree.
2. If dirty and `--allow-dirty` is NOT set:
   - append `push_failed` event with `error_code=E_DIRTY_WORKTREE` and `step=dirty_check`.
   - return `E_DIRTY_WORKTREE` and print dirty context.
3. If dirty and `--allow-dirty` is set:
   - append `dirty_allowed` event (see Events section).
   - emit warning and dirty context.
   - continue.

**Other gates unchanged**:
- report gating still uses `--force` (unchanged).
- ahead==0 still returns `E_EMPTY_DIFF` (unchanged).
- git push does not use `--force` or `--force-with-lease` (unchanged).

### 7.2) `agency merge`

**Gate placement**: after repo lock + worktree existence checks, before any gh checks, before any
user prompts (verify-failed prompt and merge confirmation), and before any stderr output.

**Algorithm (inserted step)**:
1. Check worktree cleanliness using `git status --porcelain --untracked-files=all` in the worktree.
2. If dirty and `--allow-dirty` is NOT set:
   - append `merge_failed` event with `error_code=E_DIRTY_WORKTREE` and `step=dirty_check`.
   - return `E_DIRTY_WORKTREE` and print dirty context.
3. If dirty and `--allow-dirty` is set:
   - append `dirty_allowed` event.
   - emit warning and dirty context.
   - continue with existing flow (mergeability, verify, confirmations).

**Confirmations remain**:
- typed `merge` confirmation still required.
- verify-failed prompt still controlled by `--force` (unchanged).

### 7.3) `agency clean`

**Gate placement**: after repo lock + worktree existence checks, before any stderr output (including
the lock message), and before the typed `clean` confirmation.

**Algorithm (inserted step)**:
1. Check worktree cleanliness using `git status --porcelain --untracked-files=all` in the worktree.
2. If dirty and `--allow-dirty` is NOT set:
   - append `clean_failed` event with `error_code=E_DIRTY_WORKTREE` and `step=dirty_check`.
   - return `E_DIRTY_WORKTREE` and print dirty context.
3. If dirty and `--allow-dirty` is set:
   - append `dirty_allowed` event.
   - emit warning and dirty context.
   - continue to typed confirmation.

**Confirmations remain**:
- typed `clean` confirmation still required.

---

## 8) Events

### New event

`dirty_allowed`

**When**: emitted when `--allow-dirty` is used and the worktree is dirty.

**Schema**:

```
{
  "schema_version": "1.0",
  "timestamp": "<RFC3339>",
  "repo_id": "<repo_id>",
  "run_id": "<run_id>",
  "event": "dirty_allowed",
  "data": {
    "cmd": "push" | "merge" | "clean",
    "status": "<raw git status --porcelain --untracked-files=all output>"
  }
}
```

### Failure events

On dirty block:
- `push`: append `push_failed` with `error_code=E_DIRTY_WORKTREE`, `step=dirty_check`.
- `merge`: append `merge_failed` with `error_code=E_DIRTY_WORKTREE`, `step=dirty_check`.
- `clean`: append `clean_failed` with `error_code=E_DIRTY_WORKTREE`, `step=dirty_check`.

**Note**: `clean_failed` is a new event introduced by this spec for preflight failures.

---

## 9) Testing Requirements

Add or update tests to cover:
- Dirty worktree blocks `push` without `--allow-dirty` (error code + message, dirty context output).
- Dirty worktree allows `push` with `--allow-dirty` (warning + dirty context + `dirty_allowed` event).
- Same as above for `merge` and `clean`.
- Ensure `--force` semantics remain unchanged:
  - `push`: still only bypasses report gate.
  - `merge`: still only bypasses verify-fail prompt.

Remove/update tests that expect the old dirty warning-only behavior for `push`.

---

## 10) Documentation Updates

Update docs to reflect:
- New flag `--allow-dirty` for `push`, `merge`, `clean`.
- New error code `E_DIRTY_WORKTREE` and its exact message.
- Dirty worktree is now a hard block by default; warning-only behavior is removed.

Targets:
- `docs/v1/constitution.md`
- `docs/v1/s3/s3_spec.md`
- `docs/v1/s3/s3_prs/s3_pr4.md`
- `docs/v1/s6/s6_spec.md`
- CLI usage text in `internal/cli/dispatch.go`

---

## 11) Compatibility Notes

This change is intentionally safety-breaking:
- Workflows that relied on warning-only behavior will now fail unless `--allow-dirty` is used.
- `--force` is not repurposed; its existing meanings remain unchanged.
- `clean_failed` is a new event (schema expansion) emitted on preflight failure.

---

## 12) Implementation Notes

- Use `git status --porcelain --untracked-files=all` to detect dirty state deterministically.
- Respect `.gitignore` (ignored files remain ignored).
- Ensure dirty check runs with non-interactive env (same as other git commands).
- Context printing should not mutate files or spawn network calls.
