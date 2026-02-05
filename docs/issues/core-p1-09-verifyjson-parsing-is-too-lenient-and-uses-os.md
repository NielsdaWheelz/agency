# [p1][core][tech-debt] verify.json parsing is too lenient and uses os.*

labels: `p1`, `type:tech-debt`, `area:core`

## summary
verify.json parsing is too lenient and uses os.*

## context
- section: Audit: Core/Shared
- source: docs/issues.md
- details:
  - `verify.ReadVerifyJSON` only checks non-empty schema_version and bypasses `fs.FS`. should validate exact version or explicitly allow ranges.

## acceptance criteria
- [ ] define minimal fix + tests

