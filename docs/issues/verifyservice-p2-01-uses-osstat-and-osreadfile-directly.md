# [p2][verifyservice][tech-debt] uses os.Stat and os.ReadFile directly

labels: `p2`, `type:tech-debt`, `area:verifyservice`

## summary
uses os.Stat and os.ReadFile directly

## context
- section: Audit: Verifyservice
- source: docs/issues.md
- details:
  - bypasses `fs.FS`, breaks testability, and violates the hard rule for shared filesystem access.

## acceptance criteria
- [ ] define minimal fix + tests

