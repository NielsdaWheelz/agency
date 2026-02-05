# [p3][cli][tech-debt] dead/no-op code in runresolver.ResolveRepoContext

labels: `p3`, `type:tech-debt`, `area:cli`

## summary
dead/no-op code in runresolver.ResolveRepoContext

## context
- section: Audit: Commands
- source: docs/issues.md
- details:
  - it calls `paths.ResolveDirs` and ignores the result. delete it or use it.

## acceptance criteria
- [ ] define minimal fix + tests

