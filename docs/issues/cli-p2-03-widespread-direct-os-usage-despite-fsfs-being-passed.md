# [p2][cli][tech-debt] widespread direct os.* usage despite fs.FS being passed in

labels: `p2`, `type:tech-debt`, `area:cli`

## summary
widespread direct os.* usage despite fs.FS being passed in

## context
- section: Audit: Commands
- source: docs/issues.md
- details:
  - Example: `open.go`, `path.go`, `clean.go`, `merge.go` use `os.Stat`/`os.WriteFile` even when `fsys` is available.

## acceptance criteria
- [ ] define minimal fix + tests

