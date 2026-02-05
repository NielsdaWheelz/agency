# [p2][core][tech-debt] integration marker checks bypass fs.FS

labels: `p2`, `type:tech-debt`, `area:core`

## summary
integration marker checks bypass fs.FS

## context
- section: Audit: Core/Shared
- source: docs/issues.md
- details:
  - `integrationworktree.HasIntegrationMarker` uses `os.Stat` directly; should use injected FS.

## acceptance criteria
- [ ] define minimal fix + tests

