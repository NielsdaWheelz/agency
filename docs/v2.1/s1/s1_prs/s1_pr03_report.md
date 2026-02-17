# PR-03 Implementation Report: Gate + Slice Readiness Evaluation

## 1. Summary of changes

Added deterministic aggregate evaluation for Gate A, Gate B, and Slice S1 readiness:

- **`internal/errors/errors.go`**: Added `E_GATE_SET_DRIFT` and `E_GATE_BLOCKED` error constants.
- **`internal/s1gates/types.go`**: Added `GateStatus`, `GatesEvaluateRequest`, `GatesEvaluateResult` types, `CanonicalIssueMapPath` constant, and `GateStatusReady`/`GateStatusBlocked` constants.
- **`internal/s1gates/issue_map_parser.go`**: Deterministic parser for `docs/v2.1/issue-map.md` that extracts issue paths from numbered backtick-wrapped list items under H2 sections, with fenced-content skipping and occurrence counting.
- **`internal/s1gates/evaluate_gates.go`**: `EvaluateGates()` — request validation, gate source parsing (PR-01), issue-map parsing, per-item evaluation (PR-01), drift detection, and aggregate status computation with deterministic error precedence.
- **`internal/s1gates/require_ready.go`**: `RequireSliceReady()` — strict enforcement wrapper that returns `E_GATE_BLOCKED` with deterministic detail keys when slice is not ready.
- **`internal/s1gates/testdata/repo_gates_eval/*`**: 5 fixture repos (valid_blocked, valid_all_closed, drift_missing, drift_duplicate, item_malformed) covering all static test variants.

Test files:
- **`internal/errors/errors_test.go`**: `TestS1PR03ErrorCodesExist`, `TestS1PR03ErrorFormat`
- **`internal/s1gates/issue_map_parser_test.go`**: `TestParseIssueMap_DeterministicIssueCounts`, `TestParseIssueMap_MalformedContentReturnsEGateSetInvalid`, `TestParseIssueMap_IgnoresFencedContent`
- **`internal/s1gates/evaluate_gates_test.go`**: 10 tests covering aggregate status, closed counting, canonical ordering, reopen re-block, drift (missing/duplicate/first-canonical), item artifact failure (mapping + details), invalid request, error precedence
- **`internal/s1gates/require_ready_test.go`**: 3 tests covering blocked return, ready passthrough, deterministic detail keys

## 2. Problems encountered

1. **gofmt alignment**: Map literal with mixed-length keys in `evaluate_gates.go` needed `gofmt -w` to fix tab alignment. Caught by `make check` fmt-check step.
2. **No others**: The PR-01/PR-02 foundation was clean and well-structured; no bugs or API mismatches encountered.

## 3. Solutions implemented

1. **gofmt**: Ran `gofmt -w` on the file. The underlying issue was cosmetic padding in a map literal.
2. **Fixture strategy**: Used static testdata repos for the 5 core variants per spec, and `t.TempDir()` with helper functions for mutation tests (reopen, multi-drift, error precedence) that require writable repos or complex setup.

## 4. Decisions made (and why)

| Decision | Rationale |
|---|---|
| `IssueMapResult` type defined in `issue_map_parser.go`, not `types.go` | It's an internal parsing artifact, not part of the PR-03 public evaluation contract. Spec deliverables for `types.go` don't list it. |
| `GateStatusReady`/`GateStatusBlocked` constants | Avoids magic strings in status computation; makes `computeGateStatus` and tests readable. |
| Reused `fenceRe`, `anyH2Re`, `numberedItemRe` from `source_parser.go` | Same package, same parsing grammar — no duplication. |
| `BlockingItems` initialized to `[]string{}` not `nil` | JSON serialization produces `[]` not `null`, matching the L2 API contract. |
| `copyFixtureRepo` helper for reopen test | `TransitionGateItem` mutates files in-place; static testdata can't be mutated in parallel tests. |
| Error precedence is enforced by sequential control flow | Matches spec: gate source → issue-map → item artifact → drift → success. No accumulation needed. |
| `item_error_message` detail uses `err.Error()` (full `CODE: message` format) | Preserves complete diagnostics for automation consumers. |

## 5. Deviations from L4/L3/L2 with justification

None. All deliverables, types, error codes, error detail keys, and test names match the spec exactly.

## 6. Commands to run new/changed behavior

```bash
# Run all PR-03 acceptance tests (15 tests)
go test -v -count=1 ./internal/s1gates/ -run 'TestEvaluateGates|TestRequireSliceReady|TestParseIssueMap'

# Run PR-03 error code tests (2 tests)
go test -v -count=1 ./internal/errors/ -run TestS1PR03

# Run full s1gates + errors package suites
go test -count=1 ./internal/errors/ ./internal/s1gates/

# Run with race detector
go test -race -count=1 ./internal/errors/ ./internal/s1gates/
```

## 7. Commands used to verify correctness

```bash
# All targeted PR-03 tests pass
go test -v -count=1 ./internal/s1gates/ -run 'TestEvaluateGates|TestRequireSliceReady|TestParseIssueMap'
# Result: 15/15 PASS

go test -v -count=1 ./internal/errors/ -run TestS1PR03
# Result: 2/2 PASS (TestS1PR03ErrorCodesExist, TestS1PR03ErrorFormat)

# Full make check (fmt-check + golangci-lint + go test ./... + build)
make check
# Result: 0 lint issues, all tests pass, build succeeds

# Race detector on touched packages
go test -race -count=1 ./internal/errors/ ./internal/s1gates/
# Result: PASS

# go vet on touched packages
go vet ./internal/errors/ ./internal/s1gates/
# Result: clean
```

## 8. Traceability table

| L3 Acceptance Item | Files | Tests | Status |
|---|---|---|---|
| Aggregate evaluation returns per-gate status, closed/total counts, blocking item references, and slice readiness. | `types.go`, `evaluate_gates.go`, `errors.go` | `TestEvaluateGates_ReturnsAggregateStatusAndSliceReady`, `TestEvaluateGates_AllClosedSetsReadyAndNoBlockers`, `TestEvaluateGates_ClosedCountingRequiresClosedStateAndNoBlockingCode`, `TestEvaluateGates_BlockingItemsPreserveCanonicalOrder` | PASS |
| Gate readiness transitions reflect reopen behavior immediately (ready to blocked). | `evaluate_gates.go`, `require_ready.go` | `TestEvaluateGates_ReopenedIssueReblocksGate`, `TestRequireSliceReady_ReturnsEGateBlockedWhenSliceNotReady`, `TestRequireSliceReady_ReturnsResultWhenSliceReady` | PASS |
| Drift and blocked-completion conditions are surfaced with the correct aggregate error semantics for evaluation flows. | `issue_map_parser.go`, `evaluate_gates.go`, `errors.go` | `TestEvaluateGates_DriftMissingGateIssueReturnsEGateSetDrift`, `TestEvaluateGates_DriftDuplicateGateIssueReturnsEGateSetDrift`, `TestEvaluateGates_DriftDetailsUseFirstCanonicalMismatch`, `TestEvaluateGates_ItemArtifactFailureMapsToEGateSetInvalid`, `TestEvaluateGates_ItemArtifactFailureDetailsIncludeItemCause`, `TestEvaluateGates_InvalidRequestReturnsEGateSetInvalid`, `TestEvaluateGates_ErrorPrecedenceIsDeterministic`, `TestRequireSliceReady_BlockerDetailsAreDeterministic`, `TestParseIssueMap_DeterministicIssueCounts`, `TestParseIssueMap_MalformedContentReturnsEGateSetInvalid`, `TestS1PR03ErrorCodesExist`, `TestS1PR03ErrorFormat` | PASS |

## 9. Commit message

```
feat(s1gates): add gate + slice readiness evaluation (PR-03)

Implement deterministic aggregate evaluation for Gate A, Gate B, and
Slice S1 readiness with explicit blocker reporting and strict drift/
blocked semantics per s1_pr03.md spec.

New deliverables:
- internal/errors/errors.go: E_GATE_SET_DRIFT, E_GATE_BLOCKED constants
- internal/s1gates/types.go: GateStatus, GatesEvaluateRequest,
  GatesEvaluateResult, CanonicalIssueMapPath, gate status constants
- internal/s1gates/issue_map_parser.go: deterministic issue-map parser
  with occurrence counting and fenced-content skipping
- internal/s1gates/evaluate_gates.go: EvaluateGates with request
  validation, PR-01 gate source/item evaluation, drift detection
  (missing/duplicate), and deterministic error precedence
- internal/s1gates/require_ready.go: RequireSliceReady enforcement
  wrapper returning E_GATE_BLOCKED with deterministic detail keys

Aggregate evaluation:
- Counts items as closed only when state=closed AND no blocking_code
- Preserves canonical gate-source order in blocking_items
- Computes slice_ready as gate_a.ready AND gate_b.ready
- Maps per-item parse/load failures to E_GATE_SET_INVALID with
  issue_path, item_error_code, item_error_message details
- Detects drift when any Gate A/B issue is missing from or duplicated
  in issue-map.md, reporting first canonical mismatch

Error precedence: gate source invalid > issue-map invalid > item
artifact invalid > drift > success result.

Tests: 17 new tests across 4 files covering all acceptance scenarios
from the traceability matrix. 5 static fixture repos in testdata/
repo_gates_eval/ plus t.TempDir() helpers for mutation/edge cases.

Verified: make check (fmt, lint, test, build), race detector, go vet.
```
