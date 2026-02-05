# [p2][invocation][tech-debt] HasSandboxMarker bypasses fs.FS

labels: `p2`, `type:tech-debt`, `area:invocation`

## summary
HasSandboxMarker bypasses fs.FS

## context
- section: Audit: Invocation
- source: docs/issues.md
- details:
  - it uses `os.Stat` directly, preventing stubs and in-memory fs tests.

## acceptance criteria
- [ ] define minimal fix + tests

