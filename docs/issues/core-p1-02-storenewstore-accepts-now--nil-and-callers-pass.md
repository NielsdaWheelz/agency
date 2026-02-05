# [p1][core][bug] Store.NewStore accepts Now == nil, and callers pass nil

labels: `p1`, `type:bug`, `area:core`

## summary
Store.NewStore accepts Now == nil, and callers pass nil

## context
- section: Code Smells / Bugs
- source: docs/issues.md
- details:
  - If any code path calls `s.Now()`, it will panic. Provide a default (`time.Now`) in `NewStore` or enforce non-nil.

## acceptance criteria
- [ ] define minimal fix + tests

