# [p0][daemon][tech-debt] unsafe deletes in landing

labels: `p0`, `type:tech-debt`, `area:daemon`

## summary
unsafe deletes in landing

## context
- section: Audit: Daemon
- source: docs/issues.md
- details:
  - `daemon/landing/service.go` calls `os.RemoveAll(sandboxDir)` without subpath checks; use `fs.SafeRemoveAll` or explicit containment.

## acceptance criteria
- [ ] define minimal fix + tests

