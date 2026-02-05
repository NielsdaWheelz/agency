# [p2][runnerstatus][tech-debt] uses os.* directly

labels: `p2`, `type:tech-debt`, `area:runnerstatus`

## summary
uses os.* directly

## context
- section: Audit: Runnerstatus
- source: docs/issues.md
- details:
  - `runnerstatus.Load` bypasses `fs.FS` and cannot be stubbed.

## acceptance criteria
- [ ] define minimal fix + tests

