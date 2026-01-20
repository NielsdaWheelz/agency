# PR Spec: Global Run Resolution + Explicit Repo Targeting

## Goal

Decouple run-targeted commands from CWD-based repo discovery so that existing runs can be operated on from anywhere, while preserving predictable, repo-scoped name resolution.

This PR must not change core run lifecycle semantics, persistence formats, or introduce remote execution.

---

## Scope

### In Scope

1. **Global run resolution**
   - Run-targeted commands (`attach`, `resume`, `stop`, `kill`, `clean`, `verify`) must work from any directory.
   - Full `run_id` and unique `run_id` prefixes resolve globally.
   - Name-based resolution prefers current repo when CWD is inside a repo.

2. **Explicit repo targeting**
   - Add `--repo <path>` flag (path only) to:
     - repo-required commands: `init`, `run`, `doctor`
     - run-targeted commands (for name disambiguation)
   - `--repo` overrides CWD-based repo discovery.
   - `--repo` must accept any path inside a repo and normalize to repo root.

3. **Deterministic resolution order**
   - Implement a single shared resolution algorithm (see below).
   - Archived runs are excluded from **name** matching globally and per-repo.
   - Archived runs remain resolvable by exact `run_id` or prefix.

4. **Robust repo relocation handling**
   - If a resolved run’s last-known repo path is missing:
     - Attempt recovery via `repo_index.paths` (most recent first).
     - If none exist, fail with actionable error and hint.

5. **Help text + errors**
   - Update command help to reflect “works from anywhere” semantics.
   - Improve ambiguity errors with disambiguation hints.

---

## Out of Scope

- Remote hosts, SSH, MCP, APIs
- `--repo-id` or non-path repo identifiers
- Changes to run_id generation format
- Interactive TUI
- New commands
- Persistence schema changes beyond additive fields (if any)

---

## Resolution Semantics (Authoritative)

### Run-Targeted Commands

Given `<ref>` and optional `--repo`:

1. **Exact run_id match**
   - If `<ref>` matches full run_id regex, resolve globally and return.
   - When resolving by exact run_id or prefix, `--repo` is ignored and must not cause errors or filtering.
   - `--repo` only scopes name resolution.

2. **Unique run_id prefix**
   - If `<ref>` matches run_id prefix regex and is unique globally, resolve and return.

3. **Explicit repo scope**
   - If `--repo <path>` provided:
     - Normalize to repo root.
     - Resolve `<ref>` as name within that repo (active runs only).
     - If not found, error.

4. **CWD repo scope**
   - If CWD is inside a repo:
     - Resolve `<ref>` as name within that repo (active runs only).
     - If found, return.

5. **Global name resolution**
   - Resolve `<ref>` as name across all repos (active runs only).
   - If unique, return.
   - If ambiguous, error with disambiguation list.

### Archived Runs

- Never matched by **name**.
- May be resolved by exact run_id or unique prefix.
- Subsequent command behavior may error (e.g. attach to archived run).

---

## Regex and Prefix Rules

- **Exact run_id**: `^\d{8}-[a-f0-9]{4}$` (13 chars total: 8 digits + hyphen + 4 hex)
- **Prefix match**: `^\d{8}-[a-f0-9]{1,4}$` — must include the full timestamp and hyphen, plus at least 1 hex char
  - Minimum valid prefix: `20250109-a` (10 chars)
  - This avoids date-only collisions (e.g. `202501` would match all runs on that day)
- Non-matching refs are always treated as names.

---

## Repo Resolution Rules

- `--repo <path>`:
  - Must exist.
  - Must be inside a git repo.
  - Normalize using `git rev-parse --show-toplevel -C <path>`.
  - On failure: `E_INVALID_REPO_PATH`.

- Missing repo root for resolved run:
  1. Try `repo_root_last_seen`.
  2. Try other paths in `repo_index.paths` (most recent first).
  3. For each candidate path: verify it still matches the `repo_key` (origin URL or path hash), not just that it exists.
  4. If none valid:
     - Error `E_REPO_NOT_FOUND`
     - Hint: run `agency doctor --repo <newpath>` to relink.

---

## Commands Affected

### Behavior Change
- `attach`
- `resume`
- `stop`
- `kill`
- `clean`
- `verify` — operates on an existing run; does not require CWD repo context

### Flag Addition
- `--repo <path>`: `init`, `run`, `doctor`, `ls`, and all run-targeted commands

### Flag Addition Only (no behavior change)
- `ls` — add `--repo <path>` to scope listing to a specific repo (alternative to CWD scoping)

No behavior changes for:
- `show`, `open`, `path`, `push`, `merge` (already global-capable)

---

## Error Handling

### New / Clarified Errors

- `E_INVALID_REPO_PATH`
  - `--repo` path does not exist or is not inside a git repo

- `E_RUN_REF_AMBIGUOUS`
  - Name matches multiple active runs
  - Include disambiguation list:
    - repo_key
    - run_id
    - name
    - created_at

- `E_REPO_NOT_FOUND`
  - Run resolved but no valid repo path exists
  - Include known paths and relink instructions

### Ambiguity Errors

- `E_RUN_ID_AMBIGUOUS`: run_id prefix matches multiple runs
- `E_RUN_REF_AMBIGUOUS`: name matches multiple active runs

These are distinct error codes with different resolution hints.

---

## Internal Design Constraints

- Introduce **one** shared resolver:
  - `ResolveRepoContext(...)`
  - `ResolveRunContext(...)`
- No command-specific resolution logic.
- No CWD reads outside resolver.
- No persistence schema breaking changes.
- Global resolution may scan all known runs; performance optimizations are deferred.

---

## Tests (Required)

### Integration Tests

1. Run-targeted command from outside any repo:
   - `agency attach <run_id>` works.

2. Name resolution inside repo:
   - Two repos with same run name.
   - From repo A: `agency attach <name>` resolves repo A run.

3. Name resolution outside repo:
   - Same setup.
   - From outside: `agency attach <name>` errors with ambiguity.

4. `--repo` disambiguation:
   - `agency attach <name> --repo /path/to/repoB` resolves correctly.

5. Archived exclusion:
   - Archived run name never matches.
   - Archived run_id still resolves.

6. Missing repo path recovery:
   - Delete repo root.
   - Resolver finds alternate path from `repo_index`.
   - If none: `E_REPO_NOT_FOUND` with hint.

7. Invalid `--repo`:
   - Non-existent path → `E_INVALID_REPO_PATH`.

---

## Non-Goals / Guardrails

- Do not change run_id format.
- Do not introduce remote identifiers.
- Do not add caching layers.
- Do not change locking semantics.
- Do not modify how tmux sessions are managed.

---

## Acceptance Criteria

- All listed commands work from any directory.
- Name resolution is predictable and documented.
- Ambiguity errors are actionable.
- No regressions in repo-scoped workflows.
- All tests above pass.

---

## Notes

This PR intentionally treats run IDs as the primary universal handle.
Repo context is a scoping convenience, not a requirement.

Future remote execution must reuse these semantics unchanged.
