# [p1][policy][policy] beta decisions (policy)

labels: `p1`, `type:policy`, `area:policy`

## summary
beta decisions (policy)

## context
- section: merged
- source: docs/issues.md (merged)
- merged items:
  - break backward compatibility for correctness
  - events are required where they are part of the contract
  - env merging must be deterministic
  - no migrations
- details:
  - enforce strict schema versions, reject unknown fields, and allow data migrations (not silent fallbacks).
  -
  - event appends must be atomic, locked, and fail hard in those flows.
  -
  - standardize on one merge function (override wins, no duplicate keys).
  -
  - acceptable path is to delete data dir and restart. document this clearly and enforce with strict version checks.
  -

## acceptance criteria
- [ ] define minimal fix + tests

