# [p2][render][tech-debt] show --json leaks internal storage schema

labels: `p2`, `type:tech-debt`, `area:render`

## summary
show --json leaks internal storage schema

## context
- section: Audit: Render
- source: docs/issues.md
- details:
  - `render.RunDetail.Meta` is `*store.RunMeta`; any internal schema change becomes a public API change. define a stable DTO and map fields explicitly.

## acceptance criteria
- [ ] define minimal fix + tests

