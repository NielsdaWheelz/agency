# PR-02 Report: agency.json schema + strict validation

## summary of changes

implemented loading and strict validation of `agency.json` (schema v1) with:

- **new package**: `internal/config/` with two files:
  - `agencyjson.go`: struct definitions (`AgencyConfig`, `Defaults`, `Scripts`), JSON loading with strict type checking
  - `validate.go`: semantic validation, runner resolution logic, `FirstValidationError` helper

- **new error codes** in `internal/errors/errors.go`:
  - `E_NO_AGENCY_JSON` — file not found
  - `E_INVALID_AGENCY_JSON` — malformed JSON or schema violations
  - `E_RUNNER_NOT_CONFIGURED` — runner name not in `runners` map and not `claude`/`codex`

- **test fixtures** in `internal/config/testdata/` (17 JSON files covering all validation scenarios):
  - `valid_min.json`, `valid_full.json` — valid configurations
  - `missing_*.json` — missing required fields
  - `wrong_types*.json` — type mismatches at various levels
  - `wrong_version*.json` — version as wrong type or value
  - `empty_*.json` — empty string values
  - `unknown_keys.json` — extra keys (should be ignored)
  - `runner_*.json` — runner resolution scenarios
  - `invalid_json.json` — malformed JSON syntax

- **comprehensive tests** in `internal/config/config_test.go` (21 test functions, all passing)

- **README.md** updated to reflect PR-02 completion and add `config/` to project structure

## problems encountered

1. **go's json.Unmarshal silently accepts wrong types**: the standard library will silently drop unknown fields and zero-default wrong-typed nested fields. the spec requires strict type checking (e.g., if `defaults` is a string instead of object, must fail with clear error).

2. **interface signature mismatch**: initial `stubFS` implementation for tests used generic return types (`any`, anonymous interfaces) instead of concrete types (`io.WriteCloser`, `iofs.FileInfo`), causing compile errors against the `fs.FS` interface.

3. **fractional version numbers**: JSON numbers like `1.5` unmarshal successfully into Go `int` (truncating to `1`). spec requires `version` to be exactly integer `1`, rejecting `1.5`.

4. **version as string**: JSON `"1"` (string) should be rejected but Go's unmarshal doesn't catch this directly when target is `int`.

## solutions implemented

1. **two-phase parsing**: first unmarshal into `map[string]json.RawMessage` at top level, then unmarshal each known field into its expected type. type mismatches produce clear JSON unmarshal errors which we wrap with field context.

2. **fixed interface signatures**: updated `stubFS` in tests to return exact types: `io.WriteCloser` for `CreateTemp`, `iofs.FileInfo` for `Stat`, `os.FileMode` for permission params.

3. **fractional version detection**: when unmarshal to `int` fails, try `float64` and check if it equals its integer truncation. if not, reject with "version must be an integer".

4. **string version detection**: unmarshal to `int` fails for string values, producing the "version must be an integer" error message.

## decisions made

1. **load vs validate separation**: kept `LoadAgencyConfig` (file I/O + type checking) separate from `ValidateAgencyConfig` (semantic validation). this lets callers inspect partially-valid configs if needed and matches the spec's "first error" semantics.

2. **runner resolution in validate**: runner command resolution (`ResolvedRunnerCmd`) happens during `ValidateAgencyConfig`, not during load. this keeps loading focused on JSON structure.

3. **whitespace detection**: used `unicode.IsSpace` to detect any whitespace character (space, tab, newline) in runner commands, per spec requirement that runner commands be "single executable (no args)".

4. **error code selection**: used `E_INVALID_AGENCY_JSON` for all schema/type/required-field errors. used `E_RUNNER_NOT_CONFIGURED` only when runner resolution fails (name not in `runners` map and not `claude`/`codex`).

5. **`LoadAndValidate` convenience function**: added a combined function for the common case where callers want both operations. takes `fs.FS` interface directly.

## deviations from prompt/spec

1. **no fsReadAdapter**: the spec suggested a minimal adapter approach, but I removed it in favor of requiring full `fs.FS` interface for `LoadAndValidate`. simpler and more consistent with the rest of the codebase.

2. **ValidationError struct defined but not exported**: the spec mentioned `ValidationError { Field, Msg }` but I kept it internal since `FirstValidationError` extracts the message from `AgencyError` directly.

## how to run

### build

```bash
go build -o agency ./cmd/agency
```

### run tests

```bash
# all tests
go test ./...

# config package only (verbose)
go test ./internal/config/... -v
```

### verify CLI still works

```bash
go run ./cmd/agency --help
go run ./cmd/agency init --help
go run ./cmd/agency doctor --help
```

(init and doctor still return "not yet implemented" — they'll be wired in PR-04 and PR-05)

## how to check new functionality

the config package is internal; no CLI commands use it yet. to verify:

```bash
# run the config package tests
go test ./internal/config/... -v

# expected output: 21 test functions, all PASS
```

test coverage includes:
- missing file → `E_NO_AGENCY_JSON`
- invalid JSON syntax → `E_INVALID_AGENCY_JSON`
- wrong types at every level (defaults, scripts, runners, version)
- missing/empty required fields
- unknown keys ignored (forward compatibility)
- runner resolution: claude/codex PATH fallback, custom via runners map, missing custom → error
- runner with args (whitespace) → error
- `FirstValidationError` message stability

## branch name and commit message

**branch**: `pr02/agency-json-schema-validation`

**commit message**:

```
feat(config): implement agency.json schema v1 loading and strict validation

Add internal/config package with comprehensive agency.json support:

- AgencyConfig, Defaults, Scripts structs matching constitution schema
- LoadAgencyConfig: reads JSON with strict type checking via two-phase
  parsing (raw map first, then typed fields). catches type mismatches
  that Go's json.Unmarshal would silently accept.
- ValidateAgencyConfig: semantic validation for required fields, version
  check (must be integer 1), runner resolution logic, whitespace check
  for runner commands (no args allowed).
- FirstValidationError: extracts stable error message for doctor output.
- LoadAndValidate: convenience function combining both operations.

Add error codes to internal/errors:
- E_NO_AGENCY_JSON: file not found
- E_INVALID_AGENCY_JSON: malformed JSON or schema violations  
- E_RUNNER_NOT_CONFIGURED: runner not in map and not claude/codex

Add 17 test fixtures in internal/config/testdata/ covering:
- valid minimal and full configurations
- missing required fields (defaults, parent_branch, runner, scripts)
- wrong types (defaults as string, scripts as array, version as string/float)
- empty string values, unknown keys (ignored), runner resolution scenarios

Add 21 test functions in internal/config/config_test.go with:
- stubFS implementation for isolated testing
- table-driven tests for type strictness and required fields
- stability tests for FirstValidationError message format
- integration test with real filesystem

Update README.md to reflect PR-02 completion.

Closes: slice 0 PR-02
Spec: docs/v1/s0/s0_prs/s0_pr02.md
```
