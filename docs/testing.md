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

## stubs

- coverage thresholds and reporting
