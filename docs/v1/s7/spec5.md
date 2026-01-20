# PR Spec: Verbose, Structured Error Output (v1)

## Goal

Surface existing error context to users in a consistent, human-friendly way by improving error rendering, without changing error creation sites or public error codes.

This PR fixes the core UX bug: **Agency captures rich error details but discards them at print time**.

## Non-Goals

* No `--quiet` mode
* No JSON error output
* No refactor of all error callsites (except targeted `command` population—see below)
* No change to error codes, exit statuses, or command semantics
* No new persistence formats

## Scope

This PR modifies **error formatting and printing only**, plus minimal callsite cleanup where ad-hoc stderr printing bypasses the error system.

### In scope

* Add `--verbose` global flag
* Implement tiered error output (default vs verbose)
* Print selected fields from `AgencyError.Details`
* Print verify failure output inline (tail)
* Centralize hint printing inside `errors.PrintWithOptions`
* **Remove all external `printHint(` calls** (grep must return zero outside `internal/errors`)
* Remove ad-hoc stderr printing that duplicates error output
* **Targeted fix**: ensure `command` field is populated for push/merge git/gh failures
* Add formatter-focused tests

### Out of scope

* Changing how errors are constructed (beyond targeted `command` fix)
* Replacing `Details map[string]string` with a typed struct
* Adding redaction heuristics beyond basic truncation
* Golden/snapshot tests

---

## User-Visible Behavior

### Default error output (no flags)

**Invariant format:**

```
error_code: E_...
<one-line message>

<context (selected fields)>
<hint (if present)>
```

Example (verify failure):

```
error_code: E_SCRIPT_FAILED
verify failed (exit 1)

script: scripts/agency_verify.sh
exit_code: 1
duration: 12.3s
log: /path/to/verify.log
record: /path/to/verify_record.json

output (last 20 lines):
  npm ERR! Test failed
  npm ERR! code ELIFECYCLE
  ...

hint: fix the failing tests and run: agency verify my-feature
```

### `--verbose`

Adds:

* More context keys (whitelisted)
* Longer output tails (up to caps)
* Extra details keys (sorted) under `extra:`

Never prints entire logs or unbounded stderr.

---

## Output Contract (Hard Requirements)

### Ordering (strict)

1. `error_code: E_...`
2. Message line
3. Blank line
4. Context block (`key: value`, stable order)
5. Optional `output (tail N):` blocks
6. `hint:` line (if present)
7. `try:` lines (if present)

### Context key whitelist (default mode, in order)

```
op
run_id
repo
worktree
script
command
branch
parent
pr
exit_code
duration
log
record
```

* Keys absent → skipped
* Unknown keys → not printed (unless `--verbose`)

### Truncation

| Mode    | Max lines | Max chars |
| ------- | --------- | --------- |
| default | 20        | 8 KB      |
| verbose | 100       | 64 KB     |

* Always append `… (truncated)` when truncation occurs
* Paths are never truncated

### Value Sanitization Rules

**Context block values (`key: value`)**:
* Must be single-line
* Newlines in value → replaced with literal `\n`
* Individual values truncated to 256 chars (append `…`)
* Trailing whitespace trimmed
* `\r\n` normalized to `\n` before replacement

**Output blocks (tails)**:
* Multi-line content allowed, indented with 2 spaces
* Per-line max: 512 chars (truncate + `…`)
* Trailing whitespace trimmed per line
* `\r\n` normalized to `\n`

**`extra:` section (verbose only)**:
* Keys sorted alphabetically
* Values single-line, truncated to 128 chars
* Never print `stderr` under `extra:` if already printed as output tail
* Skip keys already printed in context block

---

## Special Case: Verify Failures

On `E_SCRIPT_FAILED` **from verify**:

Default mode **must** include:

* script path
* exit code
* duration (if known)
* tail of `verify.log` (preferred) or stderr fallback
* log + record paths
* hint (if present)

This is the only error type that prints output tails by default.

---

## Flag Semantics

* `--verbose` is a **global flag** (root command)
* Applies uniformly to all subcommands
* Parsed once, passed via context or options struct

No `--quiet`.

### Implementation Note: Global Flag Parsing

Agency uses stdlib `flag`. To support global flags before subcommand dispatch:

```go
// In cli/dispatch.go or similar
globalFlags := flag.NewFlagSet("agency", flag.ContinueOnError)
verbose := globalFlags.Bool("verbose", false, "show detailed error context")
globalFlags.Parse(os.Args[1:])

// Remaining args after global flags
subArgs := globalFlags.Args()
// subArgs[0] is subcommand name, subArgs[1:] passed to subcommand FlagSet
```

This ensures `--verbose` works regardless of position before the subcommand name.

---

## Code Changes

### New / Modified Functions

#### `errors.Print(w io.Writer, err error)` — **UNCHANGED**

* Existing signature preserved for zero churn
* Calls `Format(err, PrintOptions{})` internally
* No behavioral change

#### `errors.PrintWithOptions(w io.Writer, err error, opts PrintOptions)` — **NEW**

* New function for verbose mode
* Calls `Format(err, opts)`
* May perform bounded I/O for verify output tails (see below)
* Writes formatted result to `w`

#### `errors.Format(err error, opts PrintOptions) string` — **NEW**

* Pure function, no I/O
* Responsible for all formatting logic
* Returns formatted string

#### `PrintOptions`

```go
type PrintOptions struct {
    Verbose    bool
    // Tailer provides output tail lines for verify failures.
    // If nil, PrintWithOptions reads verify.log directly (bounded I/O).
    Tailer     func(logPath string, maxLines int) ([]string, error)
}
```

**I/O policy**:
* `Format` is pure—never reads files, never includes output tails
* `PrintWithOptions` workflow:
  1. Calls `Format(err, opts)` → base output string
  2. If verify failure detected AND `Details["log"]` set:
     * Reads tail via `opts.Tailer` (or default file reader if nil)
     * Appends formatted tail block to output
  3. Writes complete output to `w`
* Read is bounded: max 100 lines, 64KB
* If tail read fails: skip tail silently (no error propagation, log path is already in output)

### Verify Failure Detection Rule

An error is treated as a **verify failure** when ALL of:
1. `Code == E_SCRIPT_FAILED`
2. AND one of:
   * `Details["log"]` ends with `verify.log`
   * OR `Details["script"]` ends with `agency_verify.sh`

This is deterministic and requires no constructor changes.

### Cleanup

* Remove ad-hoc stderr printing in:
  * `push.go` (git push stderr pattern)
  * any similar callsites that bypass `errors.Print`
* **Remove all `printHint(` calls outside `internal/errors`**
  * Acceptance: `grep -r 'printHint(' --include='*.go' | grep -v internal/errors` returns empty
* Move `printHint` logic into `Format`
* Delete `internal/commands/gh_hints.go` (functions moved to errors pkg)

---

## Tests

### Unit tests (required)

File: `internal/errors/format_test.go`

**Contract tests**:
* `TestPrintSignatureUnchanged` — compile-time check that `Print(io.Writer, error)` exists
* `TestPrintWithOptionsSignature` — compile-time check for new function

**Formatter tests**:
* first line always `error_code:`
* message always second line
* context keys appear in correct order
* unknown keys hidden by default
* `--verbose` reveals extras under `extra:`
* truncation occurs + labeled
* verify failure detection rule works correctly

**Value handling tests**:
* multi-line detail values don't break formatting (newlines escaped)
* missing context keys skipped cleanly (no empty lines)
* CRLF normalized
* long values truncated with `…`

**Edge cases**:
* nil Details map
* empty string values
* Details with only unknown keys

### Integration tests (minimal)

* Run a command that triggers `E_SCRIPT_FAILED`
* Assert stderr contains:
  * error_code
  * script path
  * output tail header
  * hint

No golden files; use substring/order assertions.

---

## Migration Plan

1. Add `errors.Format` and `errors.PrintWithOptions` (new functions)
2. Update `errors.Print` to call `Format` with default options (signature unchanged)
3. Add global `--verbose` flag parsing in `cli/dispatch.go`
4. **Targeted fix**: Add `command` to Details for git/gh errors in `push.go` and `merge.go`
5. Remove all `printHint(` calls outside `internal/errors`
6. Delete `internal/commands/gh_hints.go`
7. Remove ad-hoc stderr printing (e.g., `git push stderr:` pattern)
8. Adjust tests that assert exact stderr contents
9. Add contract and formatter tests

---

## Acceptance Criteria

### Functional

* Verify failures show actionable context (script, exit code, output tail) without needing to open log files
* Push and merge failures show `command` field when available (targeted fix ensures it's populated for git/gh calls)
* `--verbose` reveals deeper diagnostics under `extra:` section
* No regression in error codes or exit behavior
* No unbounded output in default mode

### Code quality

* `grep -r 'printHint(' --include='*.go' | grep -v internal/errors` returns empty
* `errors.Print(io.Writer, error)` signature unchanged (contract test passes)
* Formatting is deterministic and testable

### Output contract

* First line always `error_code: E_...`
* Context keys in specified order
* Multi-line values escaped or indented correctly
* Truncation marked with `… (truncated)`

---

## Explicit Non-Goals (Reminder)

* No new error codes
* No JSON output
* No schema changes
* No automation logic changes
* No `--quiet` mode
