# s2 pr-03 report: derived status computation (pure)

## summary of changes

implemented a pure, local-only status derivation function that converts run metadata and local filesystem/tmux state into derived status values. the implementation:

1. **created `internal/status/derive.go`**:
   - `Snapshot` struct for local inputs (tmux active, worktree present, report bytes)
   - `Derived` struct for outputs (derived status, archived, report nonempty)
   - `Derive(meta *store.RunMeta, in Snapshot) Derived` pure function
   - `ReportNonemptyThresholdBytes = 64` constant
   - exported status string constants for user-visible contract stability

2. **created `internal/status/derive_test.go`**:
   - comprehensive table-driven tests (40+ test cases)
   - covers all precedence rules from the spec
   - test helpers: `mkMeta()`, `ptrTime()`, `ptrInt()`, `ptrBool()`
   - explicit panic-safety test for nil meta
   - constant value verification tests

3. **updated `README.md`**:
   - marked PR-03 as complete in slice 2 progress
   - added `status/` package to project structure

## problems encountered

1. **meta field types**: the existing `store.RunMeta` uses value types (int, string) rather than pointers for optional fields like `PRNumber` and `LastPushAt`. this meant checking for "not set" requires checking for zero values (0 for int, "" for string) rather than nil. this is not a problem per se, just required careful handling.

2. **nil sub-structs**: `RunMeta.Flags` and `RunMeta.Archive` are pointers that can be nil. all predicate functions needed nil guards before accessing fields.

## solutions implemented

1. **nil meta handling**: the `Derive` function handles nil meta gracefully by returning "broken" status immediately, without accessing any meta fields. archived and report_nonempty are still computed from the snapshot since they're meaningful even for broken runs.

2. **helper predicates**: created small helper functions (`isMerged`, `isAbandoned`, `isSetupFailed`, etc.) that encapsulate nil-safe access patterns. this keeps the main `deriveStatus` function readable.

3. **negative bytes clamping**: spec requires clamping negative report bytes to 0. implemented at the start of `Derive` before any threshold comparisons.

## decisions made

1. **exported status constants**: defined all status strings as exported constants (`StatusBroken`, `StatusMerged`, etc.) for two reasons:
   - allows callers to compare against known values without magic strings
   - enables tests to verify the string values are stable (user-visible contract)

2. **precedence in separate function**: split status derivation into `Derive` (handles nil meta, computes all derived values) and `deriveStatus` (implements precedence rules for non-nil meta). this separation makes the code easier to test and reason about.

3. **no archived suffix**: per spec, the "(archived)" suffix is the render layer's responsibility. `Derive` only returns the `Archived` boolean; callers append the suffix when formatting output.

4. **string values match spec exactly**: used exact strings from the spec ("needs attention", "ready for review", "active (pr)", etc.) since these are part of the user-visible contract.

## deviations from prompt/spec/roadmap

**none**. the implementation follows the spec exactly:
- pure function with no filesystem, tmux, or network calls
- precedence rules implemented exactly as specified
- threshold constant defined in the status package
- all required test cases covered
- nil meta returns "broken" without panicking

## how to run new/changed commands

this PR adds no new CLI commands. it's a pure library PR.

### running tests

```bash
# run status package tests
go test ./internal/status/... -v

# run all tests
go test ./...
```

### using the status package (internal API)

```go
import (
    "github.com/NielsdaWheelz/agency/internal/status"
    "github.com/NielsdaWheelz/agency/internal/store"
)

// example: derive status from meta and local state
meta := &store.RunMeta{ /* ... */ }
snapshot := status.Snapshot{
    TmuxActive:      true,  // tmux session exists
    WorktreePresent: true,  // worktree path exists on disk
    ReportBytes:     100,   // size of .agency/report.md
}
derived := status.Derive(meta, snapshot)
// derived.DerivedStatus = "active" or "ready for review" etc.
// derived.Archived = false
// derived.ReportNonempty = true
```

## how to verify functionality

```bash
# run the comprehensive test suite
go test ./internal/status/... -v

# verify specific precedence cases
go test ./internal/status/... -v -run "TestDerive/merged_wins"
go test ./internal/status/... -v -run "TestDerive/setup_failed"
go test ./internal/status/... -v -run "TestDerive/ready_for_review"
go test ./internal/status/... -v -run "TestDerive/nil_meta"

# verify no panics
go test ./internal/status/... -v -run "TestDeriveNilMetaDoesNotPanic"

# verify constants
go test ./internal/status/... -v -run "TestReportNonemptyThresholdConstant"
go test ./internal/status/... -v -run "TestStatusStringConstants"
```

## branch name and commit message

**branch:** `pr03/s2-derived-status-computation`

**commit message:**

```
feat(status): implement pure derived status computation for s2-pr03

Add internal/status package with pure status derivation logic that converts
run metadata and local filesystem/tmux state into derived status values.

Key components:
- Snapshot struct: TmuxActive, WorktreePresent, ReportBytes inputs
- Derived struct: DerivedStatus, Archived, ReportNonempty outputs
- Derive() function implementing precedence rules from s2_spec

Status precedence (highest wins):
1. nil meta → "broken"
2. merged (archive.merged_at set) → "merged"
3. abandoned (flags.abandoned) → "abandoned"
4. setup_failed (flags.setup_failed) → "failed"
5. needs_attention (flags.needs_attention) → "needs attention"
6. ready_for_review (pr_number + last_push_at + report >= 64 bytes) → "ready for review"
7. activity fallbacks:
   - tmux active + pr → "active (pr)"
   - tmux active → "active"
   - pr → "idle (pr)"
   - else → "idle"

Design decisions:
- Pure function with no filesystem, tmux, or network calls
- Handles nil meta gracefully (returns "broken", does not panic)
- Clamps negative report bytes to 0
- Exports status string constants for contract stability
- "(archived)" suffix is render layer responsibility

Test coverage:
- 40+ table-driven test cases
- All precedence rules verified
- Edge cases: nil meta, nil sub-structs, threshold boundaries
- Explicit panic-safety test

Files:
- internal/status/derive.go (implementation)
- internal/status/derive_test.go (comprehensive tests)
- README.md (updated progress + project structure)
- docs/v1/s2/s2_prs/s2_pr03_report.md (this report)

Implements: s2-pr03 (derived status computation)
Part of: slice 2 (observability)
```
