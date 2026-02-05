# [p1][core][bug] Run pipeline uses current branch as a proxy for parent in some flows

labels: `p1`, `type:bug`, `area:core`

## summary
Run pipeline uses current branch as a proxy for parent in some flows

## context
- section: Code Smells / Bugs
- source: docs/issues.md
- details:
  - `runservice.checkRepoContextOnly` falls back to current branch when parent is deferred. That’s a semantic mismatch and can mask invalid parent config.

## acceptance criteria
- [ ] define minimal fix + tests

