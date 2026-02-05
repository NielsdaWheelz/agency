# [p2][core][tech-debt] schema version constants are scattered

labels: `p2`, `type:tech-debt`, `area:core`

## summary
schema version constants are scattered

## context
- section: Audit: Core/Shared
- source: docs/issues.md
- details:
  - `"1.0"` appears in many packages with no central definition. this will drift on the next schema bump.

## acceptance criteria
- [ ] define minimal fix + tests

