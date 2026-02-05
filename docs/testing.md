# testing

this document defines the testing policy.

## goals

- prove contract compliance (schemas, events, error codes)
- prevent regressions in critical user flows
- keep tests deterministic and fast

## test tiers

- unit: pure logic, parsers, validators
- integration: real fs + temp git repos + store + daemon handlers
- e2e: gh-backed flows (opt-in)

## binding requirements

- every new error code is tested.
- every contract change has tests that reject old or unknown schema versions.
- every flow that writes events must test success and event-write failure.

## rules

1. no network access in unit or integration tests.
2. use `t.TempDir()` for isolation.
3. no `time.Sleep` for synchronization; inject clocks or use bounded waits.
4. prefer real fs and temp git repos over mocks.
5. use `httptest` for daemon api testing.

## e2e policy

- gated by `AGENCY_GH_E2E=1` and `GH_TOKEN`.
- run only targeted e2e tests in ci.

## references

- `.claude/prompts/test-writing.md`

## daemon read API tests

the read API test suite (`internal/daemon/read_handlers_test.go` and `status_derive_test.go`) covers:
- status derivation: precedence rules (13 rows), attention flags (10 rows), DTO conversions
- read handlers: list/get for worktrees, invocations, checkpoints, logs, diff
- filter helpers: state, mode, worktree ref matching
- pagination: cursor-based for all 3 list types, exclusive cursor boundaries
- diff integration: real git repo with commits, structured diff verification
- parameter parsing: defaults and overrides for all endpoint params
- routing: method not allowed, unknown sub-actions

test pattern: `httptest.NewRequest` + `httptest.NewRecorder` directly on unexported handlers (package `daemon`). status derivation tests use external package `daemon_test`.

## stubs

- coverage thresholds and reporting
