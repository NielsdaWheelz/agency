# [p3][daemon][tech-debt] checkpoint engine bypasses fs.FS

labels: `p3`, `type:tech-debt`, `area:daemon`

## summary
checkpoint engine bypasses fs.FS

## context
- section: Audit: Daemon
- source: docs/issues.md
- details:
  - `daemon/checkpoint/engine.go` uses `os.CreateTemp`, `os.ReadFile`, `os.WriteFile` directly.

## acceptance criteria
- [ ] define minimal fix + tests

