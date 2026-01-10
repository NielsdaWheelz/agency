# s2 pr-01 report: run discovery + parsing + broken run records

## summary of changes

implemented filesystem-based run discovery and robust meta.json parsing for s2 observability commands:

- created `internal/store/scan.go` with:
  - `RepoInfo` struct — minimal repo identity for joining runs to repos (repo_key, origin_url)
  - `RunRecord` struct — discovered run with parsed metadata (RepoID, RunID, Broken, Meta, Repo, RunDir)
  - `ScanAllRuns(dataDir)` — discovers runs across all repos, returns sorted by RepoID then RunID
  - `ScanRunsForRepo(dataDir, repoID)` — discovers runs for single repo, returns sorted by RunID
  - `LoadRepoIndexForScan(dataDir)` — loads repo_index.json for display-only purposes, handles missing file and legacy formats
  - `PickRepoRoot(repoKey, cwdRepoRoot, idx)` — selects best repo root path for display (preference order: cwd > index paths)
  - internal `repoJoinCache` — caches repo.json lookups per repo_id to avoid repeated reads

- created `internal/store/scan_test.go` with comprehensive tests:
  - valid + corrupt meta handling (3 records: 2 valid, 1 corrupt)
  - mismatched meta identity (directory name is canonical, not meta content)
  - empty/missing data directory handling (graceful empty result)
  - scoped scanning (ScanRunsForRepo returns only specified repo's runs)
  - deterministic ordering (stable sort by RepoID, then RunID)
  - broken meta types (missing schema_version, missing created_at, invalid json, empty file)
  - repo.json join is best-effort (missing/corrupt doesn't affect broken status)
  - repo.json caching (same pointer returned for same repo_id)
  - LoadRepoIndexForScan: missing file, invalid json, standard format, legacy format
  - PickRepoRoot: cwd wins, most recent path, first existing path, nil cases, file vs directory

- added error codes to `internal/errors/errors.go`:
  - `E_RUN_ID_AMBIGUOUS` — id prefix matches >1 run
  - `E_RUN_BROKEN` — run exists but meta.json is unreadable/invalid

- updated `README.md`:
  - marked s2 PR-01 as complete
  - updated next task to PR-02
  - updated store package description to include run scanning

## problems encountered

1. **type naming conflict**: s2_pr01 spec defines a `Meta` struct, but `RunMeta` already exists from s1. rather than creating duplicate types that could drift, reused the existing `RunMeta` type directly in `RunRecord`. this maintains single source of truth.

2. **repo_index.json schema differences**: s2_pr01 spec referenced `seen_paths` and `most_recent_path` fields, but constitution defines `paths` and `last_seen_at`. followed constitution schema since it's the public contract; `Paths[0]` serves as "most recent path" per the merge behavior documented in constitution.

3. **broken meta detection criteria**: spec said mark broken if `SchemaVersion == "" or CreatedAt.IsZero()`. since `RunMeta.CreatedAt` is a string (not `time.Time`), checked for empty string instead of `IsZero()`.

## solutions implemented

- **canonical identity from directories**: `RunRecord.RepoID` and `RunRecord.RunID` always come from directory names, not from meta.json content. this ensures consistent identity even if meta.json has mismatched values. meta values are preserved for debugging.

- **graceful degradation**: missing directories return empty slice (not error). corrupt meta produces `RunRecord` with `Broken=true` but still populates identity from directory names. allows `ls` to always show something rather than failing entirely.

- **repo join caching**: created internal `repoJoinCache` that loads repo.json once per repo_id and caches result (including nil for missing/corrupt). prevents N reads of the same file when scanning N runs in a repo.

- **legacy format support**: `LoadRepoIndexForScan` accepts both the standard `{ "repos": {...} }` format and a legacy `{ "entries": {...} }` format, though the legacy format isn't used in practice. defensive coding for potential migrations.

- **directory existence check**: `PickRepoRoot` verifies paths exist AND are directories (not files) using `os.Stat`. prevents false positives from files with same name as expected directories.

## decisions made

1. **reuse RunMeta not create Meta**: spec suggested creating a separate `Meta` type, but `RunMeta` already satisfies all requirements and is the actual type used in production. creating a separate type would require conversion logic and risk drift.

2. **nil vs empty slice**: `ScanAllRuns` returns nil for missing repos directory (graceful), empty slice for empty repos directory (also graceful). both are valid "no runs" results; callers check `len()` either way.

3. **stable sort order**: spec required deterministic ordering. implemented as: RepoID ascending, then RunID ascending. this provides consistent output regardless of filesystem iteration order.

4. **broken status scope**: only meta.json issues mark a run as broken. repo.json issues (missing, corrupt) set `Repo=nil` but don't affect `Broken` flag. this matches spec: "do not mark run broken unless meta is broken".

5. **LoadRepoIndexForScan returns *RepoIndex**: returns pointer so nil can indicate "file not found" (distinct from "file exists but empty"). existing `Store.LoadRepoIndex` returns empty index on missing file, but for scanning we need to distinguish these cases.

## deviations from spec

1. **type naming**: used `RunMeta` instead of creating separate `Meta` type. functionally equivalent, avoids type proliferation.

2. **field names**: spec's `RepoIndexEntry` used `SeenPaths` and `MostRecentPath`; actual schema uses `Paths`. followed constitution schema since it's the authoritative public contract.

3. **PickRepoRoot signature**: spec showed `PickRepoRoot(repoKey string, cwdRepoRoot *string) *string`, but constitution's preference order requires access to the index. implemented as `PickRepoRoot(repoKey string, cwdRepoRoot *string, idx *RepoIndex) *string` with explicit index parameter.

## how to run new commands

this pr adds no new cli commands. it's internal infrastructure for future prs (ls, show).

## how to test

```bash
# run scan package tests
go test ./internal/store/... -v -run "Scan|PickRepoRoot|LoadRepoIndexForScan"

# run all tests (verify no regressions)
go test ./...
```

expected output: all tests pass (24 scan-related tests + existing tests).

## test coverage

key test scenarios covered:
- valid + corrupt meta in same scan
- RunRecord identity from directory names (meta mismatch preserved)
- empty/missing directories (graceful handling)
- scoped vs all-repos scanning
- deterministic sort order
- various broken meta types (missing fields, invalid json, empty)
- repo.json best-effort join (corrupt doesn't break run)
- repo.json caching (same pointer returned)
- PickRepoRoot preference order (cwd > index paths > nil)
- file vs directory distinction in path checks

## branch name and commit message

**branch**: `pr/s2-pr01-run-discovery`

**commit message**:
```
feat(store): add run discovery + parsing + broken run records (s2-pr01)

Implement filesystem-based run discovery for s2 observability commands.
This enables agency ls/show to find and list runs without indexes, with
robust handling of corrupt meta.json files.

Key implementation details:
- ScanAllRuns: discovers runs across all repos by scanning
  ${AGENCY_DATA_DIR}/repos/*/runs/*/meta.json
- ScanRunsForRepo: discovers runs for single repo (more efficient)
- RunRecord: contains identity from directory names (canonical),
  parsed meta (nil if broken), repo join (best-effort), run dir path
- Broken status: set only for meta.json issues (missing, corrupt,
  missing required fields). repo.json issues don't affect broken flag.
- Stable ordering: RepoID asc, then RunID asc for deterministic output
- repo.json join caching: prevents repeated reads when scanning
- LoadRepoIndexForScan: returns nil for missing file (vs empty index)
- PickRepoRoot: selects best repo path for display (cwd > index > nil)

Added error codes:
- E_RUN_ID_AMBIGUOUS: id prefix matches multiple runs
- E_RUN_BROKEN: run exists but meta.json is unreadable/invalid

API:
- ScanAllRuns(dataDir) ([]RunRecord, error)
- ScanRunsForRepo(dataDir, repoID) ([]RunRecord, error)
- LoadRepoIndexForScan(dataDir) (*RepoIndex, error)
- PickRepoRoot(repoKey, cwdRepoRoot, idx) *string

This PR intentionally adds no command wiring - that comes in subsequent
s2 PRs that implement ls/show with id resolution and status derivation.

Follows spec: docs/v1/s2/s2_prs/s2_pr01.md

Tests (24 test functions):
- Valid + corrupt meta handling
- Mismatched meta identity (dir name canonical)
- Empty/missing directory handling
- Scoped scanning (single repo)
- Deterministic ordering
- Various broken meta types
- repo.json best-effort join
- repo.json caching
- LoadRepoIndexForScan: missing/invalid/standard/legacy
- PickRepoRoot preference order
```
