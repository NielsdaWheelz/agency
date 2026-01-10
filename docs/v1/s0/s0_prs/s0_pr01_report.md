# PR-01 Report: Directory Resolution + Repo Discovery + Origin Parsing

## Summary of Changes

this PR implements the foundational "pure logic" layer for agency's repo identity system:

1. **directory resolution** (`internal/paths/xdg.go`)
   - resolves `AGENCY_DATA_DIR`, `AGENCY_CONFIG_DIR`, `AGENCY_CACHE_DIR`
   - follows constitution: env override → macOS Library paths → XDG fallback → default
   - platform-aware via `runtime.GOOS` with explicit darwin/linux test matrix

2. **repo root discovery** (`internal/git/repo.go`)
   - `GetRepoRoot()`: runs `git rev-parse --show-toplevel` via `CommandRunner`
   - normalizes paths to absolute, handles edge cases (empty, multi-line, relative)
   - returns `E_NO_REPO` on failure

3. **origin url retrieval** (`internal/git/repo.go`)
   - `GetOriginInfo()`: runs `git config --get remote.origin.url` via `CommandRunner`
   - returns `OriginInfo{Present, URL, Host}` — never errors

4. **origin host parsing** (`internal/git/repo.go`)
   - `ParseOriginHost()`: extracts hostname from scp-like or https URLs
   - explicitly rejects `ssh://` URLs per v1 spec

5. **github owner/repo parsing** (`internal/identity/origin_parse.go`)
   - `ParseGitHubOwnerRepo()`: extracts owner/repo from github.com URLs only
   - validates names against `[A-Za-z0-9_.-]+` pattern
   - preserves case as required by spec

6. **repo identity derivation** (`internal/identity/repo_id.go`)
   - `DeriveRepoIdentity()`: computes `repo_key` and `repo_id`
   - github.com → `github:owner/repo`
   - fallback → `path:<sha256(abs_repo_root)>`
   - `repo_id` = `sha256(repo_key)[:16]`

7. **error code addition** (`internal/errors/errors.go`)
   - added `E_NO_REPO` error code

## Problems Encountered

1. **cross-platform path testing**: needed `filepath.FromSlash()` in tests to handle path separators correctly across darwin/linux/windows.

2. **stubbed CommandRunner design**: the existing `CommandRunner` interface from PR-00 worked well, but the stub needed careful key construction (`name|args|dir`) to differentiate calls.

3. **origin url format diversity**: the spec is intentionally narrow (scp-like + https only), but edge cases like `ssh://` URLs needed explicit rejection.

## Solutions Implemented

1. **explicit OS parameter**: `ResolveDirsWithOS()` accepts an `isDarwin` bool for deterministic testing without relying on runtime.GOOS.

2. **table-driven tests**: comprehensive test matrices cover all resolution paths, origin formats, and edge cases per the spec.

3. **pure functions**: `DeriveRepoIdentity()` and parsing functions are pure — no I/O, no side effects — making them trivial to test.

## Decisions Made

1. **no symlink resolution**: per spec, `GetRepoRoot()` does not call `filepath.EvalSymlinks`. this keeps behavior predictable and matches git's output directly.

2. **localhost rejection**: single-component hostnames like `localhost` are rejected by `isValidHost()` since they don't contain a dot. this prevents false positives.

3. **regex for name validation**: used `regexp.MustCompile` for `[A-Za-z0-9_.-]+` pattern. compiled once at package init, no performance concern.

4. **OriginInfo embedded in RepoIdentity**: `DeriveRepoIdentity()` populates a full `OriginInfo` struct to avoid repeated parsing downstream.

## Deviations from Spec

none. this PR strictly follows:
- constitution §7 (directory resolution)
- constitution §3-4 (repo identity)
- s0_pr01.md (exact behaviors)

the implementation is intentionally narrow per spec: no support for `ssh://` URLs, no GitHub Enterprise, no monorepo overrides.

## How to Run

### build

```bash
go build -o agency ./cmd/agency
```

### run tests

```bash
go test ./...
```

### run tests with coverage

```bash
go test ./... -cover
```

### run verbose tests for new packages

```bash
go test -v ./internal/paths/...
go test -v ./internal/git/...
go test -v ./internal/identity/...
```

### verify cli still works

```bash
go run ./cmd/agency --help
go run ./cmd/agency init --help
go run ./cmd/agency doctor --help
```

note: `init` and `doctor` still return `E_NOT_IMPLEMENTED` — they will be wired up in PR-04 and PR-05.

## Checking New Functionality

the new packages are internal and have no CLI surface yet. to verify they work correctly:

```bash
# all tests should pass
go test ./internal/paths/... ./internal/git/... ./internal/identity/... -v

# check test coverage
go test ./internal/paths/... ./internal/git/... ./internal/identity/... -cover
```

expected output: all tests pass, coverage should be high (>90%) for pure logic modules.

## Branch Name

```
pr01/directory-resolution-repo-discovery-origin-parsing
```

## Commit Message

```
feat(s0/pr01): add directory resolution, repo discovery, and origin parsing

implement foundational pure-logic layer for agency's repo identity system
per slice-00 PR-01 specification.

new packages:
- internal/paths: XDG-compliant directory resolution
  - ResolveDirs() computes data/config/cache paths
  - supports AGENCY_*_DIR env overrides, macOS Library paths, XDG fallbacks
  - platform-aware with explicit darwin detection

- internal/git: repo root and origin discovery via CommandRunner
  - GetRepoRoot() runs git rev-parse --show-toplevel
  - GetOriginInfo() runs git config --get remote.origin.url
  - ParseOriginHost() extracts hostname from scp-like/https URLs
  - returns E_NO_REPO for non-git directories

- internal/identity: repo identity derivation
  - ParseGitHubOwnerRepo() extracts owner/repo from github.com URLs
  - DeriveRepoIdentity() computes repo_key and repo_id
  - github.com origins: repo_key = "github:owner/repo"
  - non-github/missing: repo_key = "path:<sha256(abs_repo_root)>"
  - repo_id = sha256(repo_key)[:16]

error codes:
- add E_NO_REPO to internal/errors

tests:
- table-driven unit tests for all new functions
- stubbed CommandRunner for git command testing
- cross-platform path handling via filepath.FromSlash

this PR contains pure logic only: no filesystem writes, no persistence,
no init/doctor command wiring. follows constitution §3-4, §7 and
s0_pr01.md specification exactly.

refs: docs/v1/s0/s0_prs/s0_pr01.md
```
