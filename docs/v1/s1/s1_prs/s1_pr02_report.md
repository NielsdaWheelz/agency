# agency slice 01 — pr-02 report: repo detection + safety gates + repo.json update

## summary of changes

implemented the "safe to start" checks used by `agency run` and persist/update per-repo state (`repo.json`) without mutating git state.

### new files
- `internal/repo/gates.go` — new package with `CheckRepoSafe` API
- `internal/repo/gates_test.go` — comprehensive integration tests using real git operations

### modified files
- `internal/git/repo.go` — added gate functions: `HasCommits`, `IsClean`, `BranchExists`, `GetOriginURL`
- `internal/git/repo_test.go` — added unit tests for new git functions
- `README.md` — updated progress tracker and project structure

## problems encountered

### 1. macOS symlink resolution in tests
**problem:** on macOS, git resolves symlinks in paths (e.g., `/var` → `/private/var`), causing path comparison failures in integration tests.

**solution:** used `filepath.EvalSymlinks()` to resolve the expected path before comparison, ensuring consistent behavior across platforms.

### 2. determining the right abstraction level
**problem:** the spec required reusing S0 code for repo_id computation without reimplementation. needed to decide where the boundary should be between the new `repo` package and existing packages.

**solution:** the `repo` package calls into existing packages (`git`, `identity`, `store`) rather than duplicating logic. it serves as a composition layer that orchestrates the safety checks.

## solutions implemented

### CheckRepoSafe API
implemented as specified in the PR spec:

```go
func CheckRepoSafe(ctx context.Context, cr exec.CommandRunner, fsys fs.FS, cwd string, opts CheckRepoSafeOpts) (*RepoContext, error)

type CheckRepoSafeOpts struct {
    ParentBranch string
}

type RepoContext struct {
    RepoRoot   string
    RepoID     string
    RepoKey    string
    OriginURL  string
    DataDir    string
}
```

### deterministic ordering
implemented the exact ordering specified:
1. resolve repo root
2. compute repo_id (via S0 identity package)
3. read origin URL (best-effort)
4. write/update repo.json (last_seen_at, origin_url)
5. run gates (empty repo, dirty, parent branch)

### git gate functions
added to `internal/git/repo.go`:
- `HasCommits(ctx, cr, repoRoot)` — checks if HEAD exists
- `IsClean(ctx, cr, repoRoot)` — checks if working tree is clean
- `BranchExists(ctx, cr, repoRoot, branch)` — checks if local branch exists
- `GetOriginURL(ctx, cr, repoRoot)` — gets origin URL using `git remote get-url origin`

### error codes
implemented all required error codes with correct semantics:
- `E_NO_REPO` — cannot resolve repo root from cwd
- `E_EMPTY_REPO` — git rev-parse --verify HEAD fails
- `E_PARENT_DIRTY` — git status --porcelain non-empty
- `E_PARENT_BRANCH_NOT_FOUND` — git show-ref --verify refs/heads/<parent> fails

## decisions made

### 1. repo.json update strategy
**decision:** preserve existing `gh_authed` capability from repo.json when updating, since this function doesn't check gh auth status.

**rationale:** the spec says to update `last_seen_at` and `origin_url` but doesn't require re-checking gh auth. preserving existing values avoids incorrectly overwriting valid state.

### 2. additional fields in RepoContext
**decision:** added `RepoKey` and `DataDir` to `RepoContext` beyond what the minimal spec required.

**rationale:** these values are computed/resolved during `CheckRepoSafe` and will be needed by downstream callers (e.g., `agency run` needs `DataDir` to create run directories). returning them avoids redundant computation.

### 3. test strategy
**decision:** used real git operations in integration tests rather than mocking everything.

**rationale:** the spec explicitly required integration tests with temp directories. real git ensures we're testing actual behavior, not just mock expectations. unit tests with stubs were added separately for the git functions.

## deviations from spec

### none significant
the implementation closely follows the PR-02 spec. minor additions:

1. **RepoContext has extra fields:** `RepoKey` and `DataDir` were added to the return type for downstream convenience. these are computed anyway and will be needed by later PRs.

2. **preserved gh_authed on update:** when updating an existing repo.json, we preserve the `gh_authed` capability rather than setting it to false. this prevents incorrect state when CheckRepoSafe is called without running doctor.

## how to run tests

### run all tests
```bash
go test ./...
```

### run only the new tests
```bash
# unit tests for git functions
go test ./internal/git/... -v

# integration tests for CheckRepoSafe
go test ./internal/repo/... -v
```

### run specific test
```bash
go test ./internal/repo/... -v -run TestCheckRepoSafe_CleanRepoSuccess
```

## how to use new functionality

the `CheckRepoSafe` function is internal API for use by `agency run` (PR-09). it is not exposed via CLI in this PR.

### example usage (from internal code)
```go
import (
    "github.com/NielsdaWheelz/agency/internal/repo"
    "github.com/NielsdaWheelz/agency/internal/exec"
    "github.com/NielsdaWheelz/agency/internal/fs"
)

func example() {
    ctx := context.Background()
    cr := exec.NewRealRunner()
    fsys := fs.NewRealFS()
    cwd, _ := os.Getwd()

    result, err := repo.CheckRepoSafe(ctx, cr, fsys, cwd, repo.CheckRepoSafeOpts{
        ParentBranch: "main",
    })
    if err != nil {
        // handle E_NO_REPO, E_EMPTY_REPO, E_PARENT_DIRTY, E_PARENT_BRANCH_NOT_FOUND
        return
    }

    // result.RepoRoot — absolute path to repo
    // result.RepoID — computed repo_id for storage paths
    // result.OriginURL — origin URL (or empty)
    // result.DataDir — resolved AGENCY_DATA_DIR
}
```

---

## branch name
```
pr02/s1-repo-detection-gates
```

## commit message
```
feat(s1-pr02): add repo detection + safety gates + repo.json update

implement CheckRepoSafe API for agency run pre-flight checks:

- add internal/repo package with CheckRepoSafe function that:
  - resolves repo root from cwd via git rev-parse --show-toplevel
  - computes repo_id using existing S0 identity rules
  - reads origin URL via git remote get-url origin (best-effort)
  - creates/updates repo.json with last_seen_at and origin_url
  - runs safety gates in deterministic order:
    1. empty repo check (git rev-parse --verify HEAD)
    2. dirty working tree check (git status --porcelain)
    3. local parent branch check (git show-ref --verify refs/heads/<branch>)

- add git helper functions to internal/git/repo.go:
  - HasCommits: check if repo has at least one commit
  - IsClean: check if working tree is clean
  - BranchExists: check if local branch exists
  - GetOriginURL: get origin remote URL

- error codes returned:
  - E_NO_REPO: not inside a git repository
  - E_EMPTY_REPO: repo has no commits (fresh git init)
  - E_PARENT_DIRTY: working tree has uncommitted changes
  - E_PARENT_BRANCH_NOT_FOUND: local parent branch does not exist

- comprehensive test coverage:
  - unit tests for git functions with stubbed CommandRunner
  - integration tests using real git operations in temp directories
  - tests for: clean repo, dirty repo, empty repo, missing branch,
    origin URL persistence, no origin, not inside repo, repo.json updates

this PR implements slice 1 PR-02 per docs/v1/s1/s1_prs/s1_pr02.md
```
