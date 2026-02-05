# l3 pr spec (pr-a): daemon repo registry + cwd-less operation

## 0. objective

make v2 usable from **any cwd**, including outside git repos, with:
- daemon-side **repo registry** (`repo_id ↔ repo_roots`) and access checks
- cli **repo commands** (`repo add|ls|show`) and consistent `--repo` plumbing
- gold-standard errors + hints for missing/ambiguous repo context
- worktree/invocation **names scoped per repo** (no cross-repo collisions)

explicitly **not** in scope:
- sse/events, tui, `--watch`, log-follow changes
- remote network listeners (still unix socket)
- multi-user auth
- changes to core v2 invariants (sandboxes isolated, integration marker guards, daemon sole writer)

## 1. current baseline (assumed true)

- daemon is sole writer for v2 objects.
- read endpoints accept optional `repo_id`; if omitted, daemon scans all repos (`getRepoIDsForQuery`).
- cli currently derives `repo_id` locally by requiring "in repo" (GetRepoRoot), then calls daemon read endpoints with that repo_id.
- repo_index.json exists with:
  - key: `repo_key`
  - entry: `{repo_id, paths[], last_seen_at}`
- repo.json exists and is written in worktree create flows.

### 1.1 baseline bug: cli derives repo_id locally

the current pattern (cli derives repo_id via GetRepoRoot + origin → DeriveRepoIdentity) has problems:
- **fails outside repo** by definition
- **wrong in "same origin cloned twice" case** if you rely on path-based repo_key fallback
- **duplicates identity logic** between cli and daemon

**gold standard**: daemon is the identity authority. cli should not derive repo_id except as a convenience when inside a repo (best-effort, overridable).

### 1.2 repo identity stability (explicit scope)

repo_id unifies clones **only for github origins** (repo_key = `github:owner/repo`). for non-github origins we hash the path, meaning separate clones won't unify.

origin normalization: `GetOriginInfo` must normalize `git@github.com:owner/repo.git` and `https://github.com/owner/repo` to the same canonical `owner/repo`. this includes github enterprise hosts (per binding rule: no github.com-only assumptions). if normalization is not already stable across ssh+https, this PR must fix it or the "same origin cloned twice" test will fail.

this is acceptable for "ssh into laptop and use local cli there" but not "enterprise-grade identity". if you're not changing it now, accept:
- "repo identity unifies clones only for github-compatible origins (github.com + enterprise); otherwise per-path."

## 2. target UX

### 2.1 from anywhere
- `agency repo ls` works from anywhere.
- `agency worktree ls --all-repos` works from anywhere.
- `agency agent ls --all-repos` works from anywhere.
- if user runs a *repo-scoped* command (name-based refs) outside a repo **without** `--repo`, they get a hard error with a helpful hint.

### 2.2 repo scoping
- worktree names are unique **within a repo**, not globally.
- invocation names are unique **within a repo** among active invocations, not globally. "active" = status ∈ {starting, running}. once an invocation finishes (any terminal status), its name is immediately reusable even if pending land/discard.
- daemon resolution for name-based refs must support:
  - `--repo <repo_id>`: single-repo resolution
  - `--all-repos` (or omission): global resolution may be ambiguous → return E_AMBIGUOUS with candidates.

## 3. decisions (locked)

### 3.1 repo_id ↔ repo_root cardinality
gold standard for developer machines: **many roots per repo_id**.
- rationale: same origin cloned twice; path changes; dev copies; monorepo mirrors.
- we will store **all seen canonical roots**; we will prefer one deterministically (see §4.4).
- daemon must reject repo_roots it can’t access.

### 3.2 outside-repo behavior
- cli must not require being inside a git repo for read-only operations.
- mutations should accept `repo_id` always, and optionally accept `repo_root` as a fast-path.
- if cli is inside repo, it can pass `repo_root`; daemon resolves to repo_id (and registers).
- if cli is outside, it passes `repo_id`.
- this is the "operate anywhere" property.

### 3.3 daemon is identity authority

cli must not compute repo_id; it may compute repo_root and call daemon register. the canonical flow:
- **if inside repo**: cli sends `repo_root` to daemon; daemon returns `repo_id` (via `/repos/register` or a new resolve endpoint).
- **if outside repo**: cli uses `--repo <repo_id>` or `--all-repos`.

this makes cwd-less operation actually work and avoids mismatched identity logic.

### 3.4 preferred_root persistence

add `PreferredRoot` to repo.json (schema bump of repo.json only, not repo_index). update it on successful register and on any operation that uses a root. this is deterministic and stable.

do **not** enhance repo_index schema to store per-path last_seen_at (bigger migration, not worth it).

also: repo_index `Paths` must be canonical toplevel paths only. enforce this on insert.

## 4. daemon changes

### 4.1 new endpoints

#### POST /repos/register
**upsert + identity resolution endpoint.** this is the canonical way for cli to tell daemon about a repo root and get `repo_id` back.

request:
```json
{
  "repo_root": "/abs/path/to/repo"
}
```

response (ok):
```json
{
  "ok": true,
  "api_version": 1,
  "build_version": "...",
  "git_sha": "...",
  "request_id": "...",
  "data": {
    "repo_id": "16hex...",
    "repo_key": "github:owner/name | path:<sha256(absroot)>",
    "paths": ["/canon/root1", "/canon/root2"],
    "preferred_root": "/canon/root1",
    "preferred_root_accessible": true,
    "last_seen_at": "RFC3339"
  }
}
```

errors:
- `E_REPO_ROOT_INACCESSIBLE`: cannot stat / permission denied / path missing
- `E_NOT_A_REPO`: git rev-parse --show-toplevel fails
- `E_DAEMON_FAILED`: unexpected

notes:
- daemon must canonicalize root: `Abs → EvalSymlinks → GetRepoRoot (daemon-side) → EvalSymlinks again on toplevel`
- **invariant**: all stored roots are git toplevel (canonical), not arbitrary subdirs. if user passes a subdir, daemon normalizes to toplevel via `git rev-parse --show-toplevel` and returns the canonical root in the response. never reject a valid subdir — normalize silently.
- daemon must compute identity using existing logic: `origin info (never errors) + identity.DeriveRepoIdentity`
- must call both: `ensureRepoRegistered` (repo_index.json) and `ensureRepoRecord` (repo.json)
- must set `PreferredRoot` in repo.json to the registered root

#### GET /repos

list registered repos (derived from repo_index + repo.json where available).

query params: none for now

response:
```json
{
  "ok": true,
  "data": {
    "repos": [
      {
        "repo_id": "...",
        "repo_key": "...",
        "paths": ["..."],
        "preferred_root": "...",
        "preferred_root_accessible": true,
        "origin": {"present": true, "url": "...", "host": "..."},
        "last_seen_at": "RFC3339",
        "updated_at": "RFC3339"
      }
    ]
  }
}
```

#### GET /repos/{repo_id}

returns same DTO for a single repo.

lookup is O(n) over registered repos (scan repo_index values to find matching repo_id). acceptable because daemon's repo registry is small (developer machine). no secondary index needed.

errors:
- `E_REPO_NOT_FOUND`

### 4.2 repo.json schema update

we already have:
- repo_index.json: `repo_key → {repo_id, paths[], last_seen_at}`
- repo.json: richer record keyed by repo_id under `${DATA_DIR}/repos/<repo_id>/repo.json`

**add to repo.json**:
```go
type RepoRecord struct {
    // existing fields...
    PreferredRoot string `json:"preferred_root"` // NEW: canonical root for operations
}
```

**schema version**: bump repo.json schema only. repo_index unchanged.

**update rules for PreferredRoot**:
- set on `/repos/register` to the registered root (after canonicalization)
- update on successful mutation operations that require a root (worktree create, agent start, etc.) — only when the chosen root is accessible and successfully used
- do **not** update on read-only list endpoints (prevents thrashing from multiple shells)
- when reading: validate PreferredRoot is accessible; if not, fall back (see §4.4)

### 4.3 access checks (hard requirement)

daemon must reject any repo_root that it can't access:
- `os.Stat(repo_root)` must succeed
- `git rev-parse --show-toplevel` must succeed using the daemon's command runner with `Dir=repo_root`

for existing repos in repo_index, daemon must be resilient:
- some stored paths may become inaccessible later
- list endpoints should include them but mark `accessible=false` per path if we include that field; otherwise just keep paths and let operations fail when selected

### 4.4 deterministic preferred root selection

when multiple roots exist for the same repo_id, daemon must choose one deterministically for operations that need a path.

algorithm (deterministic, no prompts):
1. if request includes `repo_root` (mutations inside repo), prefer that canonical root if it matches an entry and is accessible.
2. else if repo.json has `PreferredRoot` and it is accessible, use it.
3. else collect candidate paths from repo_index entry `Paths` (already canonicalized when added), filter to accessible, and choose lexicographically smallest (last resort fallback).
4. if none accessible: return `E_REPO_NO_ACCESSIBLE_ROOTS` with hint.

**daemon helper** (internal):
```go
func (s *Server) resolveRepoRoot(repoID string, optionalRepoRoot string) (repoRoot string, identity RepoIdentity, err error)
```
- validates repo_id exists in repo_index or repo.json
- validates chosen root is accessible (os.Stat + git rev-parse)
- returns `{repo_id, preferred_root}` or error

**every handler that takes repo_id must use this helper.**

we also update `UpsertRepoIndexEntry` to:
- canonicalize the inserted root (Abs → EvalSymlinks → GetRepoRoot → EvalSymlinks)
- reject non-toplevel paths (must be canonical git root)
- de-dupe Paths
- update entry LastSeenAt
- keep Paths sorted for stable diffs

### 4.5 resolution changes (name scoped per repo)

daemon already resolves refs across provided repoIDs and can return E_AMBIGUOUS.

changes:
- ensure `resolveWorktreeRef` and `resolveInvocationRef` treat name uniqueness as per repo:
  - when `repoIDs` contains exactly 1 repo: name match is allowed even if same name exists in other repos
  - when `repoIDs` contains multiple repos: same name in different repos should return `E_AMBIGUOUS` with candidates containing `{repo_id, id, name}`

### 4.6 read endpoints: support --all-repos cleanly

keep existing semantics:
- `repo_id` omitted → scan all repos
- `repo_id` provided → filter

but add a better error surface for global scans:
- if resolution is ambiguous globally: return `E_AMBIGUOUS` with candidates including `repo_id` and display strings

### 4.7 mutation endpoints: accept repo_id everywhere

currently some mutating endpoints require `repo_root`, some don't. normalize:
- all mutating endpoints accept `repo_id` (query param or body field) **in addition to** existing `repo_root` behavior
- `repo_root` remains supported and is not removed — this is additive only
- if both `repo_id` and `repo_root` provided: daemon validates they agree (same repo), uses `repo_root`
- if `repo_id` provided without `repo_root`: daemon uses `resolveRepoRoot()` to find accessible root
- if `repo_root` provided without `repo_id`: daemon resolves to `repo_id` via register (existing behavior)

this enables "operate anywhere" for mutations without breaking older CLI versions.

## 5. cli changes

### 5.1 new cobra group: agency repo

commands:

**agency repo add \<path\>**
- calls `POST /repos/register` with `repo_root = path`
- prints: `repo_id`, `preferred_root`, `origin` (if present)
- exit codes: non-zero on error

**agency repo ls [--json]**
- calls `GET /repos`
- human format: table with columns:
  - repo_id (short)
  - origin (github:owner/repo or "path:")
  - preferred_root (shortened)
  - last_seen_at

**agency repo show \<repo_id\> [--json]**
- calls `GET /repos/{repo_id}`
- prints details + all paths

### 5.2 global flags for list commands

add to:
- `agency worktree ls`
- `agency agent ls`

flags:
- `--repo <repo_id>` (optional filter)
- `--all-repos` (boolean)
- mutual exclusion: if `--repo` provided, `--all-repos` is error (not silently ignored)

behavior:
- if in repo and no flags: behave as today (auto filter to that repo via `/repos/register`)
- if outside repo:
  - if `--repo` provided: use it
  - else if `--all-repos`: omit repo_id and list globally
  - else: error with hint (§5.3)

### 5.2.1 single-ref commands: no --all-repos

commands that resolve a single entity by name/prefix:
- `agent show`, `agent attach`, `agent logs`, `agent stop`, `agent kill`
- `worktree show`, `worktree open`, `worktree path`, `worktree land`, `worktree discard`

these commands **do not support `--all-repos`**. they require repo context:
- if in repo: auto-derive via `/repos/register`
- if outside repo without `--repo`: hard error with hint
- if outside repo with `--repo`: proceed with single-repo resolution

rationale: global resolution for single-ref commands is a footgun. if daemon returns `E_AMBIGUOUS` for a global scan, it confuses users. require explicit repo context instead.

### 5.3 error hints (gold standard)

**pattern**: error hints must be command-specific and actionable.
- first line: what failed (not where)
- second line: what the program expected
- hint: exact commands to fix

#### outside repo, single-ref command

example for `agent show foo` outside repo:
```
error: cannot resolve agent "foo" without a repo context
hint: run "agency repo ls" and re-run with "--repo <repo_id>"
hint: or register a repo: "agency repo add /path/to/repo"
```

#### outside repo, list command

example for `agency worktree ls` outside repo:
```
error: no repo context (not in a git repo)
hint: run "agency repo ls" then re-run with --repo <repo_id>
hint: or pass --all-repos to list across all registered repos
hint: or register a repo with "agency repo add /path/to/repo"
```

#### ambiguous name (daemon returns E_AMBIGUOUS)

print 3-5 candidates max, then `(+N more)`:
```
error: "foo" matches multiple agents across repos
candidates:
  - abc123 (repo: myrepo, worktree: feature-x)
  - def456 (repo: other, worktree: main)
  (+2 more)
hint: re-run with --repo <repo_id>
```

### 5.4 CLI auto-registration flow

when cli is inside a repo and needs repo context:
1. cli calls `POST /repos/register` with `repo_root` (derived from git rev-parse)
2. daemon returns `repo_id`
3. cli uses `repo_id` for subsequent calls in the session

this replaces the current pattern of cli deriving repo_id locally. daemon is authority.

## 6. error codes (daemon)

add/standardize:
- `E_REPO_NOT_FOUND`
- `E_REPO_NOT_A_GIT_REPO` (or reuse existing `E_NOT_IN_REPO` if that exists)
- `E_REPO_ROOT_INACCESSIBLE`
- `E_REPO_NO_ACCESSIBLE_ROOTS`
- reuse existing `E_AMBIGUOUS`, `E_NOT_FOUND` where appropriate, but ensure they carry candidates + hint.

error payload must include:
- `message`
- `hint` (string)
- `candidates` where relevant

## 7. data migrations / compatibility

- repo_index.json schema **unchanged**.
- repo.json schema **bumped** to add `PreferredRoot` field.
  - existing repo.json files without `PreferredRoot` are valid; daemon treats missing field as empty.
  - on next successful operation, `PreferredRoot` gets populated.
- `/repos/register` must be idempotent:
  - registering same root multiple times should not create duplicates; it should update `last_seen` and `PreferredRoot`.

api_version:
- no bump (APIVersion stays 1) because this is additive.

## 8. tests

### 8.1 daemon unit tests

**register repo:**
- given a temp git repo with origin present/absent
- POST /repos/register creates/updates repo_index and repo.json
- paths dedupe; last_seen updates
- `PreferredRoot` set to registered root

**reject inaccessible:**
- nonexistent path → `E_REPO_ROOT_INACCESSIBLE`
- permission denied path (best-effort on unix; if hard to simulate, skip)

**reject not-a-repo:**
- temp dir without .git → `E_REPO_NOT_A_GIT_REPO`

**preferred root selection:**
- when multiple paths exist and one inaccessible: chooses accessible
- when none accessible: `E_REPO_NO_ACCESSIBLE_ROOTS`

### 8.2 same-origin cloned twice case (important)

test scenario:
1. create two temp dirs with same github origin URL
2. register root A → get repo_id X
3. register root B (same origin) → get repo_id X (same)
4. verify: `paths` contains both A and B
5. verify: `preferred_root` is B (most recently registered)
6. delete root B
7. call operation requiring root → daemon falls back to A
8. verify: `preferred_root` updated to A

### 8.3 path-identity repo moves case (important)

test scenario:
1. create temp git repo without origin (path-based identity)
2. register root A → get repo_id X (sha256 of path A)
3. move directory from A to B
4. register root B → get repo_id Y (sha256 of path B, **different**)
5. verify: two separate repo entries exist
6. document: "expected behavior - path-based identity does not survive moves"
7. if user tries to access old repo_id X with inaccessible root A:
   - return `E_REPO_NO_ACCESSIBLE_ROOTS` with hint to re-register

### 8.4 inaccessible preferred_root fallback (important)

test scenario:
1. register root A, then root B (same repo_id)
2. `preferred_root` is B
3. make B inaccessible (delete or chmod)
4. call operation requiring root
5. verify: daemon falls back to A
6. verify: `preferred_root` updated to A
7. verify: `preferred_root_accessible` in response is `true`

### 8.5 cli integration tests

**outside repo:**
- `agency worktree ls` → errors with hint (unless `--all-repos`)
- `agency worktree ls --all-repos` → succeeds (calls daemon with no repo_id)
- `agency agent show foo` outside repo without `--repo` → errors with hint
- `agency agent show foo --all-repos` → error: `--all-repos` not supported for single-ref commands

**ambiguity:**
- two repos registered, same invocation name in both
- `agent show <name> --repo <repo_id>` → succeeds (single repo resolution)
- verify cli never does global resolution for single-ref commands

## 9. acceptance checklist

- [ ] `agency repo add <path>` registers repo and prints repo_id
- [ ] `agency repo ls` works from anywhere
- [ ] from outside any repo:
  - [ ] `agency worktree ls --all-repos` works
  - [ ] `agency agent ls --all-repos` works
  - [ ] name-based show commands fail without `--repo` and provide correct hint
  - [ ] `--all-repos` rejected for single-ref commands
- [ ] worktree/invocation names are scoped per repo and do not collide across repos
- [ ] daemon rejects repo_root it cannot access
- [ ] daemon falls back to accessible root when `PreferredRoot` inaccessible
- [ ] cli no longer computes repo_id locally; it computes repo_root and calls `/repos/register` to get repo_id from daemon
- [ ] same-origin cloned twice: both paths stored, same repo_id
- [ ] path-identity moves: documented as creating new repo_id
- [ ] no v2 invariants weakened; no runner ever uses integration cwd

## 10. implementation notes (non-normative)

**daemon:**
- add new handler file: `internal/daemon/repo_handlers.go`
- register in server mux: `/repos` and `/repos/{repo_id}`
- add `resolveRepoRoot(repoID, optionalRepoRoot)` helper used by all handlers needing repo context
- add store helpers if needed:
  - `Store.LoadRepoIndex` already exists
  - `Store.ReadRepoRecord(repoID)` (if not present) for GET /repos
- avoid any new global index beyond repo_index.json

**cli:**
- remove local repo_id computation via `identity.DeriveRepoIdentity`
- replace with: if in repo, compute repo_root (via git rev-parse), then call `POST /repos/register` to get repo_id from daemon
- keep git rev-parse detection for "am i in a repo?" check (cli still needs this to decide whether to require --repo or auto-register)
- add `--repo` and `--all-repos` flags per §5.2
- add `ErrNoRepoContext(cmd string)` helper for consistent error messages

**repo.json schema:**
- add `PreferredRoot string json:"preferred_root"` field
- bump schema version if you have one (or add one)

