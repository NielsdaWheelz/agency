# Agency S3 PR-01 Report — Core Plumbing for Push

## Summary

This PR adds the internal foundations required to implement `agency push` in later PRs:

1. **Added 10 new slice-03 error codes** to `internal/errors/errors.go`:
   - `E_UNSUPPORTED_ORIGIN_HOST` — origin is not github.com
   - `E_NO_ORIGIN` — no origin remote configured
   - `E_PARENT_NOT_FOUND` — parent branch ref not found locally or on origin
   - `E_GIT_PUSH_FAILED` — git push non-zero exit
   - `E_GH_PR_CREATE_FAILED` — gh pr create non-zero exit
   - `E_GH_PR_EDIT_FAILED` — gh pr edit non-zero exit
   - `E_GH_PR_VIEW_FAILED` — gh pr view failed after create retries
   - `E_PR_NOT_OPEN` — PR exists but is not open (CLOSED or MERGED)
   - `E_REPORT_INVALID` — report missing/empty without --force
   - `E_EMPTY_DIFF` — no commits ahead of parent branch

2. **Extended `meta.json` schema** with two new optional fields in `internal/store/run_meta.go`:
   - `last_report_sync_at` (RFC3339 string) — timestamp of last report sync to PR body
   - `last_report_hash` (lowercase hex string) — sha256 hash of report at last sync

3. **Added comprehensive tests** for:
   - Error code existence and stability (`TestSlice3ErrorCodesExist`)
   - Error code formatting (`TestSlice3ErrorFormat`)
   - Meta field roundtrip (`TestMetaSlice3PushFields`)
   - JSON serialization of new fields (`TestMetaSlice3FieldsInJSON`)
   - Omitempty behavior for new fields

4. **Updated README.md** with:
   - Slice 3 progress tracking
   - Links to slice 3 spec and PR breakdown docs

## Problems Encountered

None significant. The existing codebase was well-structured with clear patterns for:
- Adding error codes (follow existing const block pattern)
- Extending meta.json schema (follow existing field patterns with omitempty)
- Writing table-driven tests

## Solutions Implemented

1. **Error codes**: Added all error codes as public constants in the existing error code block, grouped under a `// Slice 3 push/PR error codes` comment for clarity.

2. **Meta fields**: Added fields with `omitempty` JSON tag to maintain backward compatibility and prevent empty values from appearing in serialized JSON.

3. **Tests**: Followed existing test patterns:
   - Used compile-time verification to ensure error codes exist
   - Used table-driven tests for format verification
   - Used functional tests for meta roundtrip behavior

## Decisions Made

1. **Error code naming**: Followed existing naming conventions (`E_` prefix, uppercase snake_case).

2. **Field placement in struct**: Placed new fields after `LastVerifyAt` to group push-related timestamps together.

3. **Test coverage**: Focused on the spec requirements:
   - Error codes compile and format correctly
   - Meta fields serialize/deserialize correctly
   - Empty fields are omitted from JSON

4. **No behavioral changes**: Per the spec's guardrails, no CLI commands or git/gh behavior was implemented in this PR.

## Deviations from Spec

None. The implementation follows the spec exactly:
- All specified error codes added
- Both meta fields added with correct types and JSON tags
- Tests verify the requirements in the spec's acceptance checklist

## How to Run New/Changed Commands

No new commands were added in this PR. This PR adds internal foundations only.

### Running Tests

```bash
# Run all tests
go test ./...

# Run slice 3 specific tests
go test ./internal/errors/... -run "Slice3" -v
go test ./internal/store/... -run "Slice3" -v
```

### Verifying Error Codes

```go
// The new error codes can be used in application code:
import "github.com/NielsdaWheelz/agency/internal/errors"

err := errors.New(errors.ENoOrigin, "no origin remote configured")
err := errors.New(errors.EEmptyDiff, "no commits ahead of parent branch")
// etc.
```

### Verifying Meta Fields

```go
// The new meta fields can be set during UpdateMeta:
store.UpdateMeta(repoID, runID, func(m *store.RunMeta) {
    m.LastReportSyncAt = time.Now().UTC().Format(time.RFC3339)
    m.LastReportHash = "abc123..." // sha256 hex
})
```

## How to Check New/Changed Functionality

```bash
# Verify all tests pass
go test ./... -count=1

# Verify error codes exist (compile-time check)
go build ./...

# Check test output for new slice 3 tests
go test ./internal/errors/... -v -run "Slice3"
go test ./internal/store/... -v -run "Slice3"
```

## Branch Name and Commit Message

**Branch name:** `pr3/s3-pr01-push-plumbing`

**Commit message:**

```
feat(s3): add error codes and meta fields for push workflow

Add internal foundations for `agency push` command (slice 3 PR-01):

Error codes (public contract):
- E_UNSUPPORTED_ORIGIN_HOST: origin is not github.com
- E_NO_ORIGIN: no origin remote configured
- E_PARENT_NOT_FOUND: parent branch ref not found
- E_GIT_PUSH_FAILED: git push non-zero exit
- E_GH_PR_CREATE_FAILED: gh pr create failed
- E_GH_PR_EDIT_FAILED: gh pr edit failed
- E_GH_PR_VIEW_FAILED: gh pr view failed after retries
- E_PR_NOT_OPEN: PR exists but not open
- E_REPORT_INVALID: report missing/empty without --force
- E_EMPTY_DIFF: no commits ahead of parent

Meta schema extensions (additive):
- last_report_sync_at: RFC3339 timestamp of report sync
- last_report_hash: sha256 hex of report at sync

Tests verify:
- All error codes compile and format correctly
- Meta fields roundtrip through JSON correctly
- Empty fields are omitted from serialized JSON

No behavioral changes in this PR per spec guardrails.
```

## Files Modified

- `internal/errors/errors.go` — added 10 new error code constants
- `internal/errors/errors_test.go` — added tests for new error codes
- `internal/store/run_meta.go` — added 2 new optional fields
- `internal/store/run_meta_test.go` — added tests for new fields
- `README.md` — updated with slice 3 progress and docs links
