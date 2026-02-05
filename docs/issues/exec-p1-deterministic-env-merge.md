# [p1][exec][tech-debt] deterministic env merge (single function, sorted keys)

labels: `p1`, `type:tech-debt`, `area:exec`

## summary
deterministic env merge (single function, sorted keys)

## context
- section: merged
- source: docs/issues.md (merged)
- merged items:
  - env merging must be deterministic
  - exec.Run env override behavior is ambiguous
  - env merging is inconsistent and nondeterministic
  - env merging rules are inconsistent across runners
  - env merge is non-deterministic
  - env merging is inconsistent
  - verify env is non-deterministic and sets repo_root to worktree
  - runner env merge is nondeterministic
- details:
  - standardize on one merge function (override wins, no duplicate keys).
  -
  - It appends `Env` entries without removing existing keys. Duplicate keys can yield platform-dependent results. Normalize env before exec.
  -
  - different code paths append env keys without de-duping. define one merge function and use it everywhere.
  -
  - `exec.Run` appends env keys (ambiguous precedence) while runservice overrides; standardize on deterministic override.
  -
  - it appends `AGENCY_*` to `os.Environ` without de-duping; duplicate keys can win unpredictably.
  -
  - verify runner assumes caller merged env; other flows build env ad-hoc.
  -
  - `buildVerifyEnv` appends to `os.Environ` and uses worktree as repo_root; standardize + inject actual repo root.
  -
  - headless spawn appends `req.Env` to `os.Environ` with duplicates and no ordering guarantees.
  -

## acceptance criteria
- [ ] define minimal fix + tests

