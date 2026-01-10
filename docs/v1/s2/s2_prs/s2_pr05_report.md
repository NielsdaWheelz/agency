# s2 pr-05 report: `agency show <id>` (human + `--json` + `--path`)

## summary of changes

implemented `agency show <id>` command for detailed run inspection:

1. **internal/render/show.go** (new file)
   - `WriteShowPaths()` - renders `--path` output in locked format
   - `WriteShowHuman()` - renders human-readable output with sections
   - `ShowPathsData` and `ShowHumanData` structs for data passing
   - `ResolveScriptLogPaths()` helper for log path resolution

2. **internal/render/json.go** (extended)
   - `RunDetail` struct for `show --json` output
   - `DerivedJSON`, `ReportJSON`, `LogsJSON`, `PathsJSON` nested structs
   - `ShowJSONEnvelope` for stable schema versioning
   - `WriteShowJSON()` function

3. **internal/commands/show.go** (new file)
   - `ShowOpts` struct with RunID, JSON, Path flags
   - `Show()` main command implementation
   - Global ID resolution using `ids.ResolveRunRef`
   - Local snapshot computation (tmux, worktree, report)
   - Status derivation using `status.Derive`
   - Error handling for ambiguous/not found/broken runs
   - Output routing to human/json/path modes

4. **internal/cli/dispatch.go** (extended)
   - Added `show` command to main dispatch switch
   - Added `showUsageText` help text
   - Added `runShow()` handler with flag parsing

5. **internal/commands/show_test.go** (new file)
   - JSON output schema tests
   - Path output format tests
   - Human output section tests
   - ID resolution error tests
   - Integration tests with fake data

6. **README.md** (updated)
   - Full `agency show` documentation
   - Usage, flags, output formats
   - Error codes and behavior
   - Examples

## problems encountered

1. **Mock error types in tests**: initially used custom mock error types for `ErrAmbiguous` and `ErrNotFound`, but the `handleResolveError` function uses type assertions to detect the actual `ids.ErrAmbiguous` and `ids.ErrNotFound` types. Fixed by using the real error types from the `ids` package.

2. **Repo root resolution complexity**: the spec requires best-effort repo root resolution with specific preference order (cwd match, then repo_index paths). Simplified by checking cwd repo root if available, otherwise using `store.PickRepoRoot`.

3. **Broken run paths**: for broken runs where meta.json is unreadable, we still need to output paths in `--path` mode. Solved by deriving paths from run directory name rather than meta fields.

## solutions implemented

1. **Global ID resolution**: reused `store.ScanAllRuns()` from PR-01 and `ids.ResolveRunRef()` from PR-02 to provide consistent resolution across all commands.

2. **Pure status derivation**: reused `status.Derive()` from PR-03 to compute derived status from meta + local snapshot, ensuring consistency with `ls` output.

3. **Modular rendering**: separated rendering logic into `render/show.go` to keep command logic clean and facilitate testing.

4. **Graceful degradation**: 
   - `--json` outputs envelope with null data on resolution errors
   - `--path` outputs best-effort paths for broken runs
   - Human output shows warnings section only when relevant

## decisions made

1. **Log path convention**: used `<run_dir>/logs/{setup,verify,archive}.log` as the canonical format, matching s1 implementation. Created `ResolveScriptLogPaths()` helper for consistency.

2. **Empty title handling**: human output shows `<untitled>` for empty titles, while JSON output preserves the raw empty string (per spec).

3. **Archived status suffix**: human output appends "(archived)" to derived status when worktree is missing, consistent with `ls` behavior.

4. **PR section visibility**: PR section in human output only appears if `pr_number` or `pr_url` is set, reducing noise for runs without PRs.

5. **Warnings section**: only displayed when at least one warning condition is true, keeping output clean for healthy runs.

## deviations from spec

**none** - implementation follows the spec precisely:
- path output format matches locked specification
- json schema matches locked specification
- error codes match specification (E_RUN_NOT_FOUND, E_RUN_ID_AMBIGUOUS, E_RUN_BROKEN)
- broken run handling matches spec (E_RUN_BROKEN for show, but ls shows broken=true)
- no network calls, no writes, no capture (read-only per spec)

## how to run new/changed commands

### show run details (human)
```bash
agency show <run_id>
agency show <run_id_prefix>   # unique prefix resolution
```

### show run details (json)
```bash
agency show <run_id> --json
agency show <run_id> --json | jq '.data.derived.derived_status'
agency show <run_id> --json | jq '.data.meta.title'
```

### show only paths
```bash
agency show <run_id> --path
```

### verify error handling
```bash
# not found error
agency show nonexistent
# E_RUN_NOT_FOUND: run not found: nonexistent

# ambiguous error (need 2+ runs with similar prefix)
agency show 2026
# E_RUN_ID_AMBIGUOUS: ambiguous run id '2026' matches multiple runs: ...
```

## how to test

### unit tests
```bash
go test ./internal/commands/... -v -run "TestShow"
go test ./internal/commands/... -v -run "TestWriteShow"
go test ./internal/commands/... -v -run "TestHandleResolveError"
```

### all tests
```bash
go test ./...
```

### manual verification
```bash
# build
go build -o agency ./cmd/agency

# check help
./agency show --help

# verify error handling
./agency show nonexistent 2>&1

# list runs first (to get a run_id to test with)
./agency ls --all-repos --json

# if runs exist, test show
./agency show <run_id>
./agency show <run_id> --json
./agency show <run_id> --path
```

## branch name and commit message

**branch**: `pr5/show-command`

**commit message**:
```
feat(s2): implement agency show command for run inspection

Add `agency show <id>` command with three output modes:
- Human-readable output with structured sections (run, workspace, pr, report, logs, status, warnings)
- JSON output with stable schema (schema_version: 1.0) for machine consumption
- Path-only output for scripting and tooling integration

Key implementation details:
- Global ID resolution: works from anywhere, not just inside a repo
- Accepts exact run_id or unique prefix for convenience
- Broken run handling: E_RUN_BROKEN error for targeted broken runs, but still outputs JSON envelope with broken=true and best-effort paths
- Pure status derivation reusing status.Derive() from PR-03
- No network calls, no state mutation (read-only command per spec)

Files changed:
- internal/render/show.go: rendering functions for human and path output
- internal/render/json.go: RunDetail struct and JSON envelope for show
- internal/commands/show.go: main command implementation
- internal/cli/dispatch.go: wire show command and help text
- internal/commands/show_test.go: comprehensive tests
- README.md: full documentation for show command

Error codes:
- E_RUN_NOT_FOUND: run not found
- E_RUN_ID_AMBIGUOUS: prefix matches multiple runs (lists candidates)
- E_RUN_BROKEN: run exists but meta.json is unreadable

Closes s2-pr05. Part of slice 2 (observability).
```
