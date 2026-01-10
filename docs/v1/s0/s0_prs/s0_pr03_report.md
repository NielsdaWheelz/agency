# agency slice 00 / pr-03 report: persistence schemas + repo store

## summary of changes

implemented the persistence layer for agency's global state:

1. **new error code**: added `E_STORE_CORRUPT` to `internal/errors/errors.go` for handling malformed JSON or unreadable schema in store files

2. **new package `internal/store/`** with three files:
   - `store.go`: `Store` struct with injectable dependencies (FS, DataDir, clock), path helper methods
   - `repo_index.go`: `RepoIndex` type and operations for `repo_index.json`
   - `repo_record.go`: `RepoRecord` type and operations for `repo.json`

3. **comprehensive tests** in `store_test.go`:
   - path construction tests
   - roundtrip tests for both index and records
   - upsert behavior tests (deduplication, path ordering, timestamp preservation)
   - corrupt JSON handling tests
   - directory creation tests
   - JSON format verification

4. **documentation updates**:
   - README.md status updated to reflect PR-03 complete
   - project structure updated to include `store/` package

## problems encountered

1. **import naming collision**: the standard library has `io/fs` and our package is `internal/fs`. solved by aliasing the stdlib import where needed, but our package is named consistently with the established pattern.

2. **timestamp format decision**: RFC3339 allows variable precision. chose fixed format `2006-01-02T15:04:05Z` for consistent output across all timestamps.

3. **path deduplication order**: spec said "most-recent-first (optional but recommended)". implemented strict most-recent-first ordering with existing paths moved to front when re-seen.

## solutions implemented

1. **injectable clock**: `Store.Now` is a `func() time.Time` parameter enabling deterministic tests without time mocking libraries

2. **atomic writes**: all JSON files use `WriteFileAtomic` (temp file + rename) per PR-00's established pattern

3. **graceful missing file handling**:
   - `LoadRepoIndex` returns empty index for missing file
   - `LoadRepoRecord` returns `(zero, false, nil)` for missing file
   - both return `E_STORE_CORRUPT` for invalid JSON or schema issues

4. **directory auto-creation**: `SaveRepoIndex` and `SaveRepoRecord` create parent directories as needed via `MkdirAll`

## decisions made

1. **single schema version constant**: `SchemaVersion = "1.0"` shared between repo_index.json and repo.json rather than separate constants

2. **strict schema validation**: reject files with missing or unknown schema_version (future-proofing)

3. **JSON indentation**: 2-space indentation with trailing newline for human readability

4. **path normalization**: `filepath.Clean` applied to paths in `UpsertRepoIndexEntry` to prevent duplicate entries from path variations like `/path/to/../to/repo`

5. **capabilities as nested struct**: made `Capabilities` a named type for cleaner API in `BuildRepoRecordInput`

## deviations from prompt/spec

1. **no `BuildRepoRecord` function**: spec suggested a `BuildRepoRecord(input)` method separate from `UpsertRepoRecord`. combined into `UpsertRepoRecord(existing *RepoRecord, input)` which handles both create and update cases more cleanly.

2. **error wrapping**: spec mentioned `E_STORE_CORRUPT` with "wrapped parse error". implemented this via `errors.Wrap()` which preserves the underlying error while adding context.

3. **unsupported schema_version handling**: spec only mentioned missing schema_version. added check for unexpected schema_version values to reject files from future versions.

## how to run

### build

```bash
go build -o agency ./cmd/agency
```

### run tests

```bash
# all tests
go test ./...

# store package only with verbose output
go test ./internal/store/... -v
```

### verify compilation

```bash
go run ./cmd/agency --help
```

## how to check new functionality

the store package is internal-only for this PR (no CLI wiring yet). functionality is verified via tests:

```bash
# run store tests
go test ./internal/store/... -v

# expected output: 19 passing tests covering:
# - path construction (3 tests)
# - repo_index operations (7 tests)
# - repo_record operations (6 tests)
# - directory creation (2 tests)
# - JSON formatting (1 test)
```

## branch name and commit message

**branch**: `pr03/persistence-schemas-repo-store`

**commit message**:

```
feat(store): add persistence schemas for repo_index.json and repo.json

implement the on-disk storage layer for agency's global state per
slice 00 / pr-03 specification.

adds internal/store package with:
- Store struct: injectable FS, DataDir, clock dependencies
- RepoIndex: schema for ${AGENCY_DATA_DIR}/repo_index.json
  - maps repo_key -> {repo_id, paths[], last_seen_at}
  - LoadRepoIndex: returns empty index for missing file
  - UpsertRepoIndexEntry: deduplicates paths, most-recent-first
  - SaveRepoIndex: atomic write with indentation
- RepoRecord: schema for ${AGENCY_DATA_DIR}/repos/<repo_id>/repo.json
  - stores repo identity, origin info, capabilities, timestamps
  - LoadRepoRecord: returns (zero, false, nil) for missing
  - UpsertRepoRecord: preserves created_at on updates
  - SaveRepoRecord: creates repo directory if needed

adds E_STORE_CORRUPT error code for malformed JSON or invalid
schema_version in store files.

comprehensive test coverage:
- roundtrip tests for both schemas
- path deduplication and ordering
- timestamp preservation on updates
- corrupt JSON detection
- directory auto-creation
- JSON formatting verification

no CLI wiring in this PR; persistence is exercised by tests only.
agency doctor integration deferred to pr-05.

refs: docs/v1/s0/s0_prs/s0_pr03.md
```
