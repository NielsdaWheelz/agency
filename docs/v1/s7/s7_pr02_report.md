# PR 7.2 Report: Enhanced ls/show output

## Summary

This PR implements the enhanced `agency ls` and `agency show` output for the runner status contract (Slice 7). The changes make runner-reported status visible in the command line interface, providing users with actionable information about what their runners are doing.

### Key Changes

1. **Updated `agency ls` output format**:
   - Replaced RUNNER and CREATED columns with SUMMARY column
   - Summary shows runner-reported summary (truncated to 40 chars)
   - For stalled runs, summary shows "(no activity for Xm)"
   - Shows "-" when no runner_status.json or invalid

2. **Updated `agency show` output format**:
   - Added new `runner_status:` section in human output
   - Shows status, updated time, summary, questions, blockers, how_to_test, and risks
   - Only appears when `.agency/state/runner_status.json` exists and is valid

3. **JSON output updates**:
   - Added `summary` and `stalled_duration` fields to ls JSON output
   - Added `runner_status` object to show JSON derived section

## Problems Encountered

1. **Column width changes**: Removing RUNNER and CREATED columns in favor of SUMMARY required updates to the column width calculation and formatting functions.

2. **Test failures**: Existing tests referenced the old struct fields (Runner, CreatedAt) that were removed. Had to update tests to use the new Summary field.

3. **Time formatting**: Needed two different time formatters - one for stall duration (compact: "45m", "2h") and one for relative time in show output (verbose: "5m ago", "2h ago").

## Solutions Implemented

1. **Render layer separation**: Kept the formatting logic clean by:
   - `formatSummary()` handles display logic for summary including stall duration
   - `formatStalledDuration()` for compact duration formatting
   - `formatRelativeTimeForShow()` for verbose time-ago formatting

2. **Graceful degradation**: When runner_status.json is missing or invalid:
   - Status derivation falls back to tmux-based detection (active/idle)
   - Summary shows "-" in ls output
   - runner_status section is omitted from show output

3. **Stall detection integration**: Used the existing watchdog package to check for stalled runs, then format the duration appropriately for display.

## Decisions Made

1. **Column removal**: Removed RUNNER and CREATED columns from human output to make room for SUMMARY. The runner is still available in JSON output and show output. Created time is available via show command.

2. **Summary truncation**: Chose 40 characters as the max summary length to balance readability with terminal width constraints.

3. **Stall message format**: Used "(no activity for Xm)" format to clearly indicate this is a stall condition, not runner-reported.

4. **runner_status section placement**: Added at the end of show output after a blank line, making it easy to find without cluttering core metadata.

## Deviations from Spec

None. The implementation follows the s7_spec.md and s7_prs.md specifications exactly.

## How to Run

### Building

```bash
go build ./cmd/agency
```

### Testing

```bash
go test ./internal/commands/... ./internal/render/... ./internal/status/... ./internal/runnerstatus/... ./internal/watchdog/...
```

### Using the enhanced ls command

```bash
# List runs with summary column
agency ls

# Example output:
# RUN_ID              NAME            STATUS            SUMMARY                    PR
# 20260119-a3f2       auth-fix        needs input       Which auth library?        #123
# 20260118-c5d2       bug-fix         stalled           (no activity for 45m)      -
```

### Using the enhanced show command

```bash
# Show run details with runner_status section
agency show <run_id>

# Example output (partial):
# ...
# status: needs input
#
# runner_status:
#   status: needs_input
#   updated: 5m ago
#   summary: Implementing OAuth but need clarification
#   questions:
#     - Which OAuth provider should I use?
```

### JSON output

```bash
# ls JSON now includes summary
agency ls --json | jq '.data[].summary'

# show JSON now includes runner_status in derived section
agency show <run_id> --json | jq '.data.derived.runner_status'
```

## Branch Name and Commit Message

**Branch:** `pr7/enhanced-ls-show-output`

**Commit message:**

```
feat(s7): implement enhanced ls/show output with runner status

PR 7.2: Add runner-reported summary to agency ls and detailed
runner_status section to agency show output.

Changes:
- Replace RUNNER/CREATED columns with SUMMARY in ls output
- Add runner_status section to show human output displaying
  status, updated time, summary, questions, blockers, how_to_test, risks
- Add summary and stalled_duration fields to ls JSON output
- Add runner_status object to show JSON derived section
- Load runner_status.json and compute stall detection in ls/show
- Update tests for new output format

The ls command now shows runner-reported summaries:
  RUN_ID              NAME            STATUS            SUMMARY                    PR
  20260119-a3f2       auth-fix        needs input       Which auth library?        #123
  20260118-c5d2       bug-fix         stalled           (no activity for 45m)      -

For stalled runs, summary shows "(no activity for Xm)" to indicate
stall detection rather than runner-reported status.

The show command now includes a runner_status section when the
.agency/state/runner_status.json file exists and is valid:
  runner_status:
    status: needs_input
    updated: 5m ago
    summary: Implementing OAuth but need clarification
    questions:
      - Which OAuth provider should I use?

This completes the observability layer for the runner status contract,
making runner-reported state visible to users in the CLI.

Slice: 7 (Runner Status Contract & Watchdog)
Part: PR 7.2 (Enhanced ls/show output)
```

## Files Changed

- `internal/render/json.go` - Added Summary, StalledDuration fields to RunSummary; added RunnerStatusJSON type
- `internal/render/ls.go` - Updated columns (removed RUNNER/CREATED, added SUMMARY); added formatSummary, truncateSummary
- `internal/render/show.go` - Added RunnerStatusDisplay type; updated WriteShowHuman to include runner_status section
- `internal/commands/ls.go` - Load runner_status.json; compute stall; pass to Derive(); added formatStalledDuration
- `internal/commands/show.go` - Load runner_status.json; build RunnerStatusDisplay; pass to output functions
- `internal/commands/ls_test.go` - Updated tests for new struct fields
- `README.md` - Updated ls/show output documentation
