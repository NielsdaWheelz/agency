# [p1][core][tech-debt] strict schema_version validation across persisted files

labels: `p1`, `type:tech-debt`, `area:core`

## summary
strict schema_version validation across persisted files

## context
- section: merged
- source: docs/issues.md (merged)
- merged items:
  - schema version enforcement is inconsistent and lenient
  - run meta schema version is never validated on read
  - integration worktree and invocation meta schema versions are not validated
  - scans don’t enforce schema_version
  - schema_version is never validated on load
  - schema_version is not validated
  - recovery scan ignores schema_version
  - verify.json is unbounded and lenient
- details:
  - multiple readers accept any schema_version; with beta + correctness, reject unknown versions and require explicit migrations.
  -
  - `store.ReadMeta` accepts any schema_version; incompatible data can silently pass.
  -
  - `ReadIntegrationWorktreeMeta` and `ReadInvocationMeta` accept any schema_version.
  -
  - `store.ScanInvocationsForRepo` only checks non-empty schema_version; should validate exact supported version.
  -
  - `runnerstatus.Load` ignores `SchemaVersion`, so incompatible files can slip through silently.
  -
  - `loadCheckpoints`/`LoadCheckpointsFile` accept any schema_version.
  -
  - `runRecoveryScan` unmarshals repo_index without validating schema_version; should reject or force reset.
  -
  - `ReadVerifyJSON` uses `os.ReadFile` with no size cap and accepts any schema_version.
  -

## acceptance criteria
- [ ] define minimal fix + tests

