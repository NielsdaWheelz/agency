# [p2][core][tech-debt] fs.WriteJSONAtomic uses raw os.* and bypasses fs.FS

labels: `p2`, `type:tech-debt`, `area:core`

## summary
fs.WriteJSONAtomic uses raw os.* and bypasses fs.FS

## context
- section: Audit: Core/Shared
- source: docs/issues.md
- details:
  - inconsistent abstraction and impossible to stub in tests that use fake FS.

## acceptance criteria
- [ ] define minimal fix + tests

