# Test Writing (Binding + Advisory)

You are writing tests for a Go CLI/daemon project. Follow binding rules first, then advisory guidance.

## Binding Rules

1. error codes
   every error code in a new or changed flow must be exercised by a test and asserted.

2. required events
   if a flow emits events, tests must cover success and event-write failure. event append failure must fail the operation.

3. schema validation
   tests must assert that unknown or empty schema_version is rejected.

4. deterministic env merge
   tests must assert override wins, no duplicates, and stable ordering.

5. safe delete
   tests must assert deletes are blocked outside allowed prefixes.

## Strategy

- prefer integration tests with real filesystem and temp git repos.
- unit tests for pure logic, parsers, and error classification.
- e2e tests only for critical user paths.

## Patterns

- use t.TempDir() for isolation.
- no network access in tests.
- do not rely on gh, tmux, or external services.
- avoid time.Sleep. inject a clock or use Eventually with tight bounds.
- use testify/require for preconditions, assert for value checks.
- use table-driven tests for 3+ similar cases.

## Required Fixtures

- real temp git repos created on the fly.
- httptest.Server for HTTP handlers.

## Contract Tests

- when you add or change an event name, update docs/contracts/events.md and add a test that checks the event shape.
- when you change a schema, add a test that rejects the old/unknown version.
