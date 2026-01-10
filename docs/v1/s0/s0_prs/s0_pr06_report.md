# PR-06 Report: Docs + Contract Sync (Slice 00 Completion)

## Summary of Changes

### Documentation Created
1. **`docs/v1/slices/slice-00_bootstrap.md`** — New comprehensive slice documentation covering:
   - Complete `agency init` and `agency doctor` command documentation
   - Output format contracts
   - Error codes specific to slice 0
   - Persistence schema documentation with examples
   - Scope boundaries (in/out of scope)
   - Demo commands and references

### Documentation Updated

2. **`docs/v1/constitution.md`** — Updated error codes section (§16.5):
   - Organized error codes into logical categories (core/CLI, repository, tool/prerequisite, script, persistence, run/workflow)
   - Added missing error codes: `E_USAGE`, `E_NOT_IMPLEMENTED`, `E_STORE_CORRUPT`
   - Added descriptions for all error codes
   - Documented error output format including optional `hint:` line
   - Added complete doctor output format with numbered key list
   - Added init output format specification

3. **`docs/v1/constitution.md`** — Updated storage section (§10):
   - Expanded `repo_index.json` schema with full field documentation
   - Added example showing both GitHub and path-based repo keys
   - Documented merge behavior for repo index updates
   - Expanded `repo.json` schema with full field documentation
   - Added two examples: GitHub repo and local-only repo
   - Documented timestamp semantics (`created_at` vs `updated_at`)

4. **`README.md`** — Updated to reflect PR-06 completion:
   - Marked slice 0 as complete
   - Added "next: slice 1" indicator
   - Added link to new slice-00_bootstrap.md documentation

## Problems Encountered

1. **Schema documentation gap**: The constitution had only brief field lists for `repo_index.json` and `repo.json`. The actual implementation in `internal/store/` had richer semantics (merge behavior, timestamp handling) that weren't documented.

2. **Error code completeness**: The constitution listed error codes but the implementation added `E_USAGE`, `E_NOT_IMPLEMENTED`, and `E_STORE_CORRUPT` which weren't in the original list.

3. **Output format ambiguity**: The doctor output format was specified but not numbered. Init output format wasn't specified at all.

## Solutions Implemented

1. **Schema documentation**: Added complete JSON schema documentation with examples for both `repo_index.json` and `repo.json`. Included merge behavior semantics and timestamp handling rules.

2. **Error code taxonomy**: Reorganized error codes into logical categories with descriptions. Added missing codes that are actually used in the implementation.

3. **Output contracts**: Numbered the doctor output keys for unambiguous ordering. Added init output format specification with all keys documented.

## Decisions Made

1. **Docs-first approach**: Per PR-06 spec, I updated documentation to match implementation rather than changing code. The implementation from PRs 00-05 is the source of truth.

2. **Error code organization**: Grouped error codes by category (core, repo, tools, scripts, persistence, run) for readability. Future slices will add to the appropriate categories.

3. **Slice docs location**: Created `docs/v1/slices/` directory as specified in PR-06 spec. This parallels the existing `docs/v1/s0/` structure but provides completed slice summaries.

4. **Schema version as string**: Documented that `schema_version` is `"1.0"` (string) not `1.0` (number), matching the implementation.

5. **No code changes**: PR-06 scope explicitly prohibits new features. All changes are documentation-only.

## Deviations from Prompt/Spec/Roadmap

1. **No deviations**: This PR follows the PR-06 spec exactly:
   - Updated constitution with error codes and schema docs
   - Created slice-00_bootstrap.md
   - No new features or code changes
   - Tests remain passing

## How to Run Commands

### Build
```bash
go build -o agency ./cmd/agency
```

### Run from source
```bash
go run ./cmd/agency --help
go run ./cmd/agency init --help
go run ./cmd/agency doctor --help
```

### Test
```bash
go test ./...
```

## How to Verify Changes

### Verify documentation consistency
```bash
# Check help text matches docs
go run ./cmd/agency --help
go run ./cmd/agency init --help
go run ./cmd/agency doctor --help
```

### Verify slice 0 demo works
```bash
cd /tmp && mkdir test-repo && cd test-repo
git init
go run /path/to/agency/cmd/agency init
go run /path/to/agency/cmd/agency doctor
# Should succeed if gh auth is configured and claude/codex on PATH
```

### Verify tests pass
```bash
go test ./...
```

## Branch Name and Commit Message

**Branch**: `pr06/slice-00-docs-cleanup`

**Commit message**:
```
docs: complete slice 00 documentation and contract sync

Close out slice 00 (bootstrap) by documenting all public contracts
and ensuring error codes align with implementation.

Changes:
- Add docs/v1/slices/slice-00_bootstrap.md with complete slice
  documentation including command specs, output formats, error codes,
  and persistence schemas
- Update constitution.md error codes (§16.5):
  - Organize codes into categories (core, repo, tools, scripts,
    persistence, run/workflow)
  - Add missing codes: E_USAGE, E_NOT_IMPLEMENTED, E_STORE_CORRUPT
  - Add descriptions for all error codes
  - Document error output format with optional hint line
  - Add numbered doctor output key list
  - Add init output format specification
- Update constitution.md storage section (§10):
  - Expand repo_index.json schema with merge behavior docs
  - Expand repo.json schema with timestamp semantics
  - Add JSON examples for GitHub and path-based repos
- Update README.md to mark slice 0 complete

No code changes. Tests pass.

Refs: slice 0 complete, ready for slice 1 (run + worktrees + tmux)
```
