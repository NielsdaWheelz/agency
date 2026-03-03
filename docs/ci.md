# ci

this document defines the ci contract. it must match `.github/workflows/ci.yml`.

## required checks

- `go test ./...`
- `go test -tags=e2e ./internal/commands -run TestS5E2EAgentPRSyncMergeFailureMatrix -count=1`
- `go test -tags=e2e ./internal/commands -run TestGHE2EAgentPRSyncMerge -count=1` (only when `AGENCY_GH_TOKEN` is configured)

## environment

- runner: ubuntu-latest
- go version: from `go.mod`

## rules

1. ci must be green before merge.
2. new test suites must be added to ci in the same pr.
3. flaky tests are treated as failures; fix or remove.

## stubs

- linting and static analysis pipeline
- artifact retention and coverage reporting
