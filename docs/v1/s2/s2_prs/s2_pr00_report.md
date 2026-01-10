# s2 pr-00 report: repo lock helper

## summary of changes

added a repo-level locking primitive to serialize mutating commands per repository:

- created `internal/lock/repo_lock.go` with:
  - `LockInfo` struct — stores pid, timestamp, and command in lock file (json format)
  - `ErrLocked` error type — returned when lock is held by another process
  - `RepoLock` struct — configurable lock manager with injectable clock and pid checker
  - `NewRepoLock(dataDir)` — constructor with v1 defaults (2h stale threshold)
  - `Lock(repoID, cmd)` — acquires lock, returns unlock function, or `*ErrLocked`

- created `internal/lock/repo_lock_test.go` with comprehensive tests:
  - lock file creation and json content verification
  - contention detection (ErrLocked on active lock)
  - stale lock detection by dead pid
  - stale lock detection by age
  - unreadable lock file handling with mtime fallback
  - unlock idempotency
  - parent directory creation
  - concurrency sanity test
  - different repos are independent

- updated `README.md`:
  - added slice 2 progress section
  - added lock package to project structure
  - added slice 2 spec/prs to documentation links

## problems encountered

1. **pid alive detection on unix**: needed a portable way to check if a pid is alive. solved by using `os.FindProcess` + `Signal(0)` pattern, which returns ESRCH if process doesn't exist or EPERM if we lack permission (but process exists).

2. **unreadable lock file handling**: spec required conservative behavior when lock file exists but can't be parsed as json. solved by falling back to file mtime for staleness calculation — if mtime is recent, treat as locked; if mtime is old, treat as stale.

3. **atomic lock acquisition**: needed to prevent race between checking lock existence and creating it. solved by using `os.O_CREATE|os.O_EXCL` flags which atomically fail if file exists.

## solutions implemented

- **stale detection**: two-pronged approach — check if pid is dead OR if lock age exceeds threshold (default 2h). either condition triggers stale lock removal.

- **retry loop**: bounded to 3 attempts when removing stale locks, prevents infinite loops if another process keeps recreating the lock.

- **parent directory creation**: `Lock()` creates `${AGENCY_DATA_DIR}/repos/<repo_id>/` if it doesn't exist, so callers don't need to pre-create directories.

- **testability**: all time and pid operations are injectable via struct fields, enabling deterministic tests without sleeps or process spawning.

## decisions made

1. **non-blocking lock**: `Lock()` returns immediately with `ErrLocked` rather than blocking. the spec explicitly states "waiting/retry policy belongs to command layer". this keeps the lock helper simple and gives callers full control.

2. **json lock file format**: chose json over alternatives (pid file, flock) because it stores debugging info (pid, timestamp, command) that helps diagnose stuck locks.

3. **0600 permissions**: lock files are private to the user since they contain process info and are only relevant to the current user's agency sessions.

4. **bounded retries**: limited stale lock removal to 3 attempts. unbounded retries could hang if another process is actively contending. 3 is enough to handle normal race conditions.

5. **conservative unreadable handling**: if lock file exists but is garbage, we check mtime. recent garbage = locked (don't steal), old garbage = stale (steal). this prevents data loss from corrupted but active locks.

## deviations from spec

none. implementation follows s2_pr00.md spec exactly:
- lock path: `${AGENCY_DATA_DIR}/repos/<repo_id>/.lock`
- api matches spec signature
- stale detection uses both pid and age checks
- mtime fallback for unreadable locks
- all required tests implemented

## how to run new commands

this pr adds no new cli commands. it's internal infrastructure for future prs.

## how to test

```bash
# run lock package tests
go test ./internal/lock/... -v

# run all tests (verify no regressions)
go test ./...
```

expected output: all tests pass.

## branch name and commit message

**branch**: `pr/s2-pr00-repo-lock`

**commit message**:
```
feat(lock): add repo-level locking for mutating commands (s2-pr00)

Implement repo-level lock primitive to serialize concurrent mutating
operations on the same repository. This is infrastructure for slice 2
observability features that need to safely coordinate writes.

Key implementation details:
- Lock path: ${AGENCY_DATA_DIR}/repos/<repo_id>/.lock
- Lock file format: JSON with pid, timestamp, and command name
- Stale detection: pid dead OR age > 2h (configurable)
- Atomic acquisition via O_CREATE|O_EXCL
- Bounded retry (3x) when removing stale locks
- Mtime fallback for unreadable/corrupt lock files
- Injectable time/pid functions for deterministic testing

API:
- NewRepoLock(dataDir) - constructor with v1 defaults
- Lock(repoID, cmd) - acquire lock, returns unlock func or *ErrLocked
- ErrLocked - typed error with holder info when available

This PR intentionally adds no command wiring - that comes in subsequent
s2 PRs that implement ls/show/capture functionality.

Follows spec: docs/v1/s2/s2_prs/s2_pr00.md

Tests:
- Lock file creation and content verification
- Contention detection (ErrLocked)
- Stale lock by dead pid
- Stale lock by age threshold
- Unreadable lock mtime fallback (conservative)
- Unlock idempotency
- Parent directory creation
- Concurrency sanity (no blocking)
- Different repos are independent
```
