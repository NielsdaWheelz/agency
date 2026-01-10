# agency slice 01 — pr-05 report: worktree creation + workspace scaffolding + ignore warning

## summary of changes

implemented the `CreateWorktree` step for slice 01:

1. **new package `internal/worktree`**: provides git worktree creation and workspace scaffolding
   - `Create()`: creates worktree + branch via `git worktree add -b <branch> <path> <parent>`
   - `WorktreePath()`: computes worktree path under `${AGENCY_DATA_DIR}/repos/<repo_id>/worktrees/<run_id>/`
   - `ReportTemplate()`: returns the `.agency/report.md` template with title
   - `ScaffoldWorkspaceOnly()`: creates `.agency/`, `.agency/out/`, `.agency/tmp/` directories and `report.md`

2. **new package `internal/runservice`**: concrete implementation of `pipeline.RunService`
   - wires together all step implementations (repo gates, config loading, worktree creation)
   - implements `CheckRepoSafe`, `LoadAgencyConfig`, `CreateWorktree`
   - stubs `WriteMeta`, `RunSetup`, `StartTmux` with `E_NOT_IMPLEMENTED` (deferred to PR-06/07/08)

3. **integration tests**: comprehensive test coverage for worktree creation
   - success path with title
   - empty title defaults to `untitled-<shortid>`
   - collision returns `E_WORKTREE_CREATE_FAILED` with git command and stderr in details
   - missing parent branch returns `E_WORKTREE_CREATE_FAILED`
   - report.md not overwritten if already exists
   - `.agency/` ignore warning emitted when not gitignored
   - no warning when `.agency/` is properly ignored

4. **documentation updates**:
   - README.md progress section updated
   - project structure section includes new packages

## problems encountered

1. **pipeline step ordering vs parent branch validation**: the pipeline runs `CheckRepoSafe` before `LoadAgencyConfig`, but parent branch may come from config defaults if not specified via `--parent` flag. the existing `CheckRepoSafe` API requires a parent branch to validate.

2. **symlink resolution on macos**: temp directories on macos use `/var` which is symlinked to `/private/var`, causing path comparison failures in tests when comparing repo roots.

## solutions implemented

1. **deferred parent branch validation**: when `--parent` is not provided on CLI:
   - `CheckRepoSafe` uses current branch for initial validation (empty repo, dirty, etc.)
   - `LoadAgencyConfig` validates the resolved parent branch (from config defaults) exists after config is loaded
   - this preserves the pipeline step order while ensuring parent branch validation happens before worktree creation

2. **symlink-aware path comparison**: in tests, use `filepath.EvalSymlinks()` to resolve the repo root before comparison, ensuring `/var/...` and `/private/var/...` paths match correctly.

3. **collision handling**: all git worktree add failures (branch exists, path exists, already checked out) return `E_WORKTREE_CREATE_FAILED` with:
   - exact git command string
   - stderr/stdout (truncated to 32KB if needed)
   - exit code

4. **best-effort ignore check**: uses `git check-ignore -q .agency/` with exit code handling:
   - 0 = ignored, no warning
   - 1 = not ignored, emit `W_AGENCY_NOT_IGNORED` warning
   - 128 = error/unknown, no warning (treat unknown as non-fatal)

## decisions made

1. **report template format**: uses a standardized format with sections: summary of changes, problems encountered, solutions implemented, decisions made, deviations from spec, how to test.

2. **title defaulting**: when title is empty, defaults to `untitled-<shortid>` where `<shortid>` is the 4-char random suffix from run_id. this happens in the worktree layer before branch name computation so it propagates to both branch name and report template.

3. **runservice architecture**: created a new `internal/runservice` package rather than putting the concrete implementation in the pipeline package. this keeps `pipeline.go` focused on orchestration and step ordering, while the service package handles the actual implementation details.

4. **warning type conversion**: added `Warning` struct to worktree package, then convert to `pipeline.Warning` in the service layer. this keeps packages loosely coupled and avoids import cycles.

5. **runner resolution in LoadAgencyConfig**: when a non-default runner is requested (e.g., `--runner codex` when default is `claude`), the service layer handles re-resolution since `ValidateForS1` only resolves the default runner.

## deviations from spec

1. **report template sections**: the s1_pr05.md spec mentioned a simpler template (summary, plan, progress, notes), but updated to use the standardized report format requested (summary of changes, problems, solutions, decisions, deviations, how to test).

2. **no cli wiring**: per spec, CLI wiring is deferred to PR-09. the `CreateWorktree` step is only callable through the pipeline or programmatically, not via a user command yet.

## how to test

### build

```bash
go build ./...
```

### run all tests

```bash
go test ./...
```

### run worktree tests specifically

```bash
go test ./internal/worktree/... -v
```

### run runservice tests specifically

```bash
go test ./internal/runservice/... -v
```

### verify worktree creation (programmatic)

since CLI wiring is not in this PR, you can verify worktree creation via the pipeline tests or by writing a small test harness:

```go
package main

import (
    "context"
    "fmt"
    "github.com/NielsdaWheelz/agency/internal/pipeline"
    "github.com/NielsdaWheelz/agency/internal/runservice"
)

func main() {
    svc := runservice.New()
    st := &pipeline.PipelineState{
        RunID:        "20260110120000-test",
        Title:        "Test Run",
        RepoRoot:     "/path/to/repo",
        RepoID:       "abcd1234ef567890",
        DataDir:      "/path/to/data",
        ParentBranch: "main",
    }
    err := svc.CreateWorktree(context.Background(), st)
    if err != nil {
        fmt.Printf("error: %v\n", err)
        return
    }
    fmt.Printf("branch: %s\n", st.Branch)
    fmt.Printf("worktree: %s\n", st.WorktreePath)
}
```

### verify via git

after running tests, you can inspect worktree state:

```bash
# in test temp repo
git worktree list
# should show the created worktree

# check scaffolding
ls -la <worktree_path>/.agency/
# should show: out/, tmp/, report.md
```

---

## branch name and commit message

**branch name**: `pr/s1-pr05-worktree-scaffolding`

**commit message**:

```
feat(s1): implement worktree creation + workspace scaffolding (PR-05)

Add CreateWorktree step for slice 1 run pipeline:

- internal/worktree: git worktree add + .agency/ dir scaffolding
  - Create() creates worktree+branch in one command
  - Scaffolds .agency/, .agency/out/, .agency/tmp/
  - Creates .agency/report.md from template (never overwrites)
  - Best-effort .agency/ ignore check with W_AGENCY_NOT_IGNORED warning

- internal/runservice: concrete RunService implementation
  - Wires CheckRepoSafe, LoadAgencyConfig, CreateWorktree
  - Stubs WriteMeta/RunSetup/StartTmux for later PRs

- Collision handling: all git failures → E_WORKTREE_CREATE_FAILED
  with exact command string and stderr in error details

- Title defaults to "untitled-<shortid>" when empty

- Deferred parent branch validation when --parent not on CLI:
  CheckRepoSafe uses current branch, LoadAgencyConfig validates
  resolved parent from config defaults

Tests:
- Worktree creation success (with title, empty title)
- Collision detection with proper error details
- Report.md not overwritten on re-scaffold
- Ignore warning emitted when .agency/ not in .gitignore
- No warning when properly ignored

Per s1_pr05.md spec. CLI wiring deferred to PR-09.
```

---

## files changed

### new files
- `internal/worktree/worktree.go` - worktree creation + scaffolding
- `internal/worktree/worktree_test.go` - integration tests
- `internal/runservice/service.go` - concrete RunService implementation
- `internal/runservice/service_test.go` - service tests
- `docs/v1/s1/s1_prs/s1_pr05_report.md` - this report

### modified files
- `README.md` - updated progress, added new packages to structure
