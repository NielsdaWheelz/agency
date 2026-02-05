# [p1][core][tech-debt] Global process mutation (os.Chdir) in core flow

labels: `p1`, `type:tech-debt`, `area:core`

## summary
Global process mutation (os.Chdir) in core flow

## context
- section: Quality Gaps (Global)
- source: docs/issues.md
- details:
  - `internal/commands/run.go` changes process CWD to handle `--repo`. That’s a concurrency hazard and makes in-process usage unsafe. Prefer explicit working dirs throughout.

## acceptance criteria
- [ ] define minimal fix + tests

