# Test Writing Guide

You are writing tests for a Go CLI/daemon project. Follow these rules exactly.

## Philosophy

This project follows the Testing Trophy model: integration tests are the backbone. Prefer real implementations over mocks. Test behavior, not implementation.

**Layer distribution:**

- **Unit (~20-30%):** Pure logic with meaningful branching — state machines, parsers, error classification, algorithmic code. If a function just delegates to dependencies, skip the unit test; integration tests will cover it.
- **Integration (~60-70%):** The workhorse. Real HTTP servers, real temp git repos, real filesystem. Mock only what is genuinely impractical (external SaaS APIs, non-deterministic behavior, truly slow resources).
- **E2E (~5-10%):** Smoke tests for critical user paths. Build the real binary, invoke it, verify output and side effects.

**The cardinal rule on mocking:** Use real implementations. Use real temp git repos, real `httptest.Server` instances, real filesystem via `t.TempDir()`. Only introduce an interface+fake when the real thing is impossible. When you must fake something, hand-write it — never use mock generation frameworks.

## Deciding What to Test

### Test this

- Every public function with non-trivial logic.
- Every error code/type the system defines. If `ELandConflict` exists, a test must trigger it.
- Error paths with equal rigor to happy paths. Every `if err != nil` branch that wraps, emits, or cleans up deserves a test case.
- Edge cases where logic branches: zero values, empty slices, nil inputs, boundary conditions.
- The contract your code provides: given this input/state, expect this output/side effect.

### Do NOT test this

- Trivial getters/setters that just return a field.
- Constructor wiring (`NewService(dep1, dep2)` that just assigns fields).
- Third-party library behavior (don't test that `os.MkdirAll` creates directories).
- Exact log messages (too brittle; test the behavior that triggers logging, not the string).
- Implementation details like internal method call order.
- Code with no branching logic and no meaningful failure modes.

## Test Structure

### File organization

- One test file per source file: `service.go` -> `service_test.go`.
- Test helpers specific to one package go in `helpers_test.go` within that package.
- Test helpers shared across packages go in `internal/testutil/`.
- Do not split tests by unit/integration file names. Use build-tagged files only when required (e.g., E2E).

### Naming

Use `Test<Function>_<Scenario>` or `Test<Type>_<Method>_<Scenario>`:

```
TestLand_Success
TestLand_NothingToLand
TestLand_ConflictDuringCherryPick
TestHandleLandRequest_InvalidBody
TestHandleLandRequest_InvocationRunning
```

Scenarios describe the condition or outcome, not implementation steps. Do not prefix with `TestUnit_` or `TestIntegration_`.

### Table-driven tests

Use table-driven tests when you have 3+ cases testing the same code path with different inputs:

```go
tests := []struct {
    name     string
    input    Input
    expected Output
    wantErr  errors.Code
}{
    {"valid input", Input{...}, Output{...}, ""},
    {"missing field", Input{}, Output{}, errors.EMissing},
}
for _, tt := range tests {
    t.Run(tt.name, func(t *testing.T) {
        // ...
    })
}
```

For tests with complex setup, unique assertions, or different dependency configurations, use standalone test functions. Do not force table-driven when cases are structurally dissimilar — a table with `setupFn func()` fields has gone too far.

### Assertions

Use `testify/require` and `testify/assert`:

- `require` for preconditions that should halt the test. Always use `require.NoError(t, err)` before accessing a result.
- `assert` for checks where you want to see all failures in a single run.

```go
result, err := service.Land(ctx, opts)
require.NoError(t, err)
assert.Equal(t, "merged", result.Strategy)
assert.Equal(t, 3, result.CommitCount)
```

### Determinism and time

- Never use real `time.Sleep` in unit or integration tests. Inject a sleeper/clock or advance a fake time source.
- Avoid `time.Now()` in assertions; inject time or compare within explicit bounds.
- If randomness is required, fix the seed and assert on invariants.
- For async behavior, use `require.Eventually`/`assert.Eventually` with tight, bounded timeouts.

### Parallelism and isolation

- Default to `t.Parallel()` for tests that use isolated state (their own temp dir, their own server).
- Tests that touch environment variables, package-level globals, or shared resources must NOT call `t.Parallel()`.
- Every test gets its own `t.TempDir()`. Never share filesystem state between tests.
- Never rely on test execution order.
- If adding `t.Parallel()` breaks a test, the test has a design problem — fix the test.

### Environment, globals, and external dependencies

- Use `t.Setenv` for environment variables. Always restore package-level globals with `t.Cleanup`.
- Tests that mutate env/globals must not run in parallel.
- Tests must not require `gh`, `tmux`, or network access. Use fakes or helper scripts instead.
- Real `git` is acceptable for integration tests that create temp repos.

## Infrastructure Patterns

### Temp git repos

Create real git repos for testing. Use a helper that:

- Takes `*testing.T`
- Creates a temp dir with `t.TempDir()`
- Initializes a git repo with an initial commit
- Returns the repo path
- Cleans up automatically (temp dirs are cleaned by the testing framework)

```go
func setupTestRepo(t *testing.T) string {
    t.Helper()
    dir := t.TempDir()
    run(t, dir, "git", "init")
    run(t, dir, "git", "commit", "--allow-empty", "-m", "initial")
    return dir
}
```

Always create repo state programmatically. Do not check fixture repos into `testdata/`.

### In-process servers

For integration tests against HTTP handlers, start the server in-process:

```go
func setupTestServer(t *testing.T) (*httptest.Server, *Client) {
    t.Helper()
    handler := NewRouter(realDeps...)
    srv := httptest.NewServer(handler)
    t.Cleanup(srv.Close)
    client := NewClient(srv.URL)
    return srv, client
}
```

Never start the compiled binary for integration tests. That is E2E territory.

### Cleanup

Every test resource must be cleaned up via `t.Cleanup()` or `t.TempDir()` (which self-cleans). Never require manual teardown. Prefer `t.Cleanup` for resource lifetimes that must survive `t.FailNow()`. Using `defer` is fine inside helpers or for short-lived resources after `require.NoError`.

## E2E Tests

- Gate with `//go:build e2e` build tag.
- Keep E2E in `*_e2e_test.go` files.
- Build the real binary before running: `go build -o <tmpdir>/agency ./cmd/agency`.
- Invoke the binary as a subprocess with `exec.Command`.
- Assert on exit codes, stdout/stderr content, and filesystem side effects.
- Use `testing.Short()` — E2E tests should call `if testing.Short() { t.Skip("skipping e2e") }`.
- Run with `go test -tags=e2e ./...`. Optionally require `E2E=1` and skip otherwise.

## Error Path Testing

For every error code defined in the system, write at least one test that:

1. Sets up conditions that trigger that error.
2. Calls the function/endpoint.
3. Asserts the correct error code is returned.
4. Asserts any side effects of the error path (events emitted, cleanup performed, state unchanged).

```go
t.Run("invocation still running", func(t *testing.T) {
    // Setup: create invocation in running state
    // Act: attempt to land
    // Assert: get EInvocationStillRunning error
    ae, ok := errors.AsAgencyError(err)
    require.True(t, ok)
    assert.Equal(t, errors.EInvocationStillRunning, ae.Code)
})
```

## Golden File / Snapshot Tests

Use golden files for:

- CLI output formatting (help text, status output, structured error messages).
- Serialized API responses where hand-writing assertions is tedious.

Avoid golden files for CLI help text when it changes frequently; prefer substring/regex assertions for stable parts. Do not use golden files for output containing timestamps, random IDs, or non-deterministic ordering.

Convention:

- Golden files live in `testdata/` next to the test file.
- Update with an `-update` flag: `go test -run TestFoo -update`.

```go
var update = flag.Bool("update", false, "update golden files")

func TestOutput(t *testing.T) {
    got := runCommand(...)
    golden := filepath.Join("testdata", t.Name()+".golden")
    if *update {
        os.WriteFile(golden, got, 0o644)
    }
    expected, _ := os.ReadFile(golden)
    assert.Equal(t, string(expected), string(got))
}
```

## Discovering Existing Helpers

Before writing new test helpers, check:

1. `internal/testutil/` for shared helpers.
2. `helpers_test.go` in the current package.
3. Neighboring `_test.go` files for patterns already in use.

Reuse existing helpers when they align with this guide. If they contradict this guide, follow this guide and refactor the helper if practical.

## Pre-Submission Checklist

Before considering tests complete:

- [ ] All new/changed logic has test coverage for happy path AND error paths.
- [ ] Tests pass with `go test -race ./...`.
- [ ] Tests pass under `make verify`.
- [ ] `t.Parallel()` is used for isolated tests; no parallel tests mutate env/globals.
- [ ] No mocks exist where a real implementation is feasible.
- [ ] No tests assert on implementation details (internal call order, private state).
- [ ] Test names describe conditions/outcomes, not implementation steps.