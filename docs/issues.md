**Audit Snapshot**
- this file is an inventory snapshot, not the live backlog.
- issue stubs live in `docs/issues/` and should be moved to github issues for real tracking.
- priorities here are triage hints, not execution order.

**Backlog**
1. **low** Switch to Cobra from Go stdlib.
2. **low** Reports should be JSON, simpler, not required, and omit "how to test" or other basics.
3. **low** Cleaner flags and confirmation. Add `--yes` to skip confirmation.
4. **low** Short flags and easier commands.
5. **low** Add a flag to `run --open` (open in IDE right away).
6. **low** support other runners (Cursor, amp, droid, opencode) headed and headless.

**Incremental Runtime Integration Plan**
1. **high** Define a single source of truth for runtime dirs.
   Use a small package, e.g. `internal/runtime` or `internal/appctx`, with a struct like `RuntimeDirs{DataDir, ConfigDir, CacheDir}`. Provide `ResolveRuntimeDirs(env paths.Env, homeDir string) (RuntimeDirs, error)`. Optionally add `ResolveRuntimeDirsFromOS() (RuntimeDirs, error)`.
2. **high** Bootstrap once at the boundary.
   Resolve `RuntimeDirs` in CLI bootstrap (cobra root / command init) and pass it down via a lightweight `CommandContext`.
3. **high** Create test-first helpers that enforce the standard.
   Add `internal/testutil/runtime.go` with `NewRuntimeDirs(t)` that uses `t.TempDir()` and returns a fully-populated `RuntimeDirs`. New tests should use this helper and pass `RuntimeDirs` down instead of `DataDirOverride`.
4. **high** Add new “gold-standard” entry points without breaking existing APIs.
   Example: `ResolveRunContextWithDirs(ctx, cr, cwd, repoPath, dirs)` in `internal/commands/runresolver.go`. Keep `ResolveRunContext(...)` as a thin wrapper that calls the new function with resolved dirs from OS/env.
5. **high** Set the migration rule for code changes.
   When touching any command/service that uses `DataDirOverride`, refactor that unit to accept `RuntimeDirs` (or a lightweight `CommandContext` containing `RuntimeDirs`) and eliminate any local `os.UserHomeDir + paths.ResolveDirs` duplication. No new `DataDirOverride` fields in new opts. Leave untouched code as-is for now.
6. **medium** Add a low-friction guardrail.
   Add a short comment in the new helper or `AGENTS.md` to set expectations: “New tests should use `internal/testutil.NewRuntimeDirs` and pass dirs explicitly.” Optional: add a small `go test` check or `rg`-based CI check later to flag new direct uses of `paths.ResolveDirs` outside the runtime package.
7. **medium** Decommission overrides gradually.
   As commands are migrated, mark `DataDirOverride` as `Deprecated:` in comments. Only remove `DataDirOverride` once every caller has switched to the injected `RuntimeDirs`.

**Open Issues / Notes**
1. **high** Repo ID collision handling is overkill, but the real collision is repo moves + path-key.
   You already track multiple paths, but the fallback key uses `sha256(abs_path)`, so moving a repo generates a new `repo_key` and thus a new `repo_id`. That “loses history.” If accepted for v1, call out the limitation: “path-based repo identity is not stable across moves; moving a non-github repo will be treated as a new repo.”
2. **high** Init creates stub scripts but doctor requires scripts exist + executable.
   Init semantics say “scripts are never overwritten,” and agency.json overwriting requires `--force`. Edge case: user runs init once, gets stub verify exiting 1, then doctor fails forever until they edit it. That’s intended, but the doctor error must be unmissable: “verify script is a stub and exits 1; replace it.” Also require scripts be relative to repo root in validation: reject absolute paths and `..` path traversal in v1 to avoid path injection.
3. **medium** `repo_index.json` merge behavior: case sensitivity.
   “Paths de-duplicated case-sensitively” is wrong on macOS default FS (case-insensitive). You’ll get duplicates with different casing. V1 fix: normalize paths via `filepath.Clean` and maybe `EvalSymlinks`. If you don’t want FS calls, state “paths de-duplicated by exact string match” and accept duplicates rather than claiming principled behavior.
4. **medium** Status: the “active (report missing)” clause is unreachable/mis-ordered.
   You check “PR exists and last_push_at and report exists” => ready, else if active and PR open => “active (report missing),” but you don’t define “PR open” vs “PR exists” consistently and you don’t store PR state in meta. In v1, don’t assert open/closed without calling `gh pr view`. Options: display “pr: yes” without open/closed, or define that `ls` may call `gh pr view` (slow) and cache it. Recommendation: don’t hit network in `ls` by default; show only meta fields and add `agency ls --fresh` later. Update display logic to only use meta fields: if `pr_url` present, show “(pr)” indicator, not “open.” `E_PR_NOT_OPEN` can exist for merge-time when you query gh.
5. **medium** Push step 1 `git fetch origin` can be slow and may prompt for creds.
   Fetch can hang if the remote needs authentication. You can’t eliminate this, but add timeouts for git/gh commands (not just scripts) and document “git must be configured for non-interactive auth (ssh agent, credential helper).”
6. **high** Missing constraint to prevent accidental bad roots.
   Add invariants: refuse to run if repo root is inside `${AGENCY_DATA_DIR}` (avoid recursion weirdness) and refuse to run if worktree path already exists (should be impossible but worth asserting).
7. **low** Product direction: TUI optional vs essential.
   You say “TUI optional” but also say “essential this functions like a program, a TUI.” Pick one. V1 can be CLI + tmux. A full-screen agency TUI can come later. If you truly need it in v1, add it explicitly as a slice and keep it a thin wrapper (no new logic).
8. **low** TMUX lifecycle when runner exits.
   Should we be kicked out of tmux when the runner exits? Currently `attach` fails when the runner is dead. At minimum, error messaging should be clearer. Consider whether tmux should stay open so users can work in the terminal or re-open the runner later without a full `resume`.
9. **medium** E2E tests for PR flows.
   Should we add E2E tests for creating, pushing, and merging PRs from off the non-main branch?
10. **low** Headless mode: `--headless`.
   Example: `claude -p "Find and fix the bug in auth.py" --allowedTools "Read,Edit,Bash"` and `codex exec`. This requires larger changes: attach a text prompt (e.g. `--prompt "fix bug"`) and log all outputs. See v1.5.

**Decisions (Beta)**
1. **high** break backward compatibility for correctness.
   enforce strict schema versions, reject unknown fields, and allow data migrations (not silent fallbacks).
2. **high** events are required where they are part of the contract.
   event appends must be atomic, locked, and fail hard in those flows.
3. **high** env merging must be deterministic.
   standardize on one merge function (override wins, no duplicate keys).
4. **high** support github enterprise and ssh remotes.
   parse ssh/https/ssh://, accept non-github.com hosts, and plumb host into gh usage.
5. **high** no migrations.
   acceptable path is to delete data dir and restart. document this clearly and enforce with strict version checks.

**Quality Gaps (Global)**
1. **high** CLI ignores cancellation and timeouts at the boundary.
   Cobra commands use `context.Background()` instead of `cmd.Context()`, so Ctrl+C and deadlines don’t propagate. Fix by using `cmd.Context()` everywhere and enforcing timeouts for external commands (git/gh/tmux), not just scripts.
2. **high** Global process mutation (`os.Chdir`) in core flow.
   `internal/commands/run.go` changes process CWD to handle `--repo`. That’s a concurrency hazard and makes in-process usage unsafe. Prefer explicit working dirs throughout.
3. **medium** Config resolution scattered across leaf code.
   Multiple commands resolve `paths.ResolveDirs` internally, which undermines the “resolve once at boundary” standard. Use injected `RuntimeDirs`/`CommandContext`.
4. **medium** Large, monolithic command files.
   `merge.go`, `push.go`, `agent.go` are 1000+ LOC with mixed concerns. Break into services (domain logic) + thin CLI adapters to improve testability and evolvability.
5. **medium** Weak observability in daemon and CLI.
   Plain `fmt.Fprintf` to stderr with no structured logs, levels, or request correlation. Introduce a logger interface and emit structured events.
6. **high** Unbounded in-memory stdout/stderr capture for external commands.
   `exec.Run` / `RunScript` buffer all output in memory. Large git/gh output can blow memory. Stream to file or cap buffers.
7. **medium** fs abstraction is leaky and inconsistent.
   Many store operations call `os.*` directly, ignoring `fs.FS`. That makes stubbing incomplete and portability weaker.
8. **high** Inconsistent path canonicalization.
   Daemon start `EvalSymlinks` for data dir; other commands don’t. This can break equality checks and recursion guards.
9. **high** Lock staleness is pid-only.
   PID reuse can make stale locks look alive and block operations forever. Add start-time verification or lock TTL with explicit override.
10. **high** Missing repo-level lock in run/worktree creation (CLI path).
   `run`/`worktree` creation mutate git state and repo.json without repo locks, risking races against push/merge/clean.
11. **high** Daemon HTTP server has no timeouts or size limits.
   `http.Server{Handler: mux}` uses zero timeouts; request bodies are decoded without size caps or `DisallowUnknownFields`. This is a DoS and correctness risk.
12. **high** schema version enforcement is inconsistent and lenient.
   multiple readers accept any schema_version; with beta + correctness, reject unknown versions and require explicit migrations.
13. **high** env merging is inconsistent and nondeterministic.
   different code paths append env keys without de-duping. define one merge function and use it everywhere.

**Audit: Commands**
1. **high** `open`, `attach`, `worktree`, `run` use `os/exec` directly.
   This bypasses `exec.CommandRunner`, undermines testability, and makes behavior inconsistent across commands.
2. **medium** multiple commands ignore injected `ctx`/`stdout`/`stderr`.
   `open.go` and `path.go` accept parameters and then ignore them. That’s dishonest APIs and makes testing misleading.
3. **medium** widespread direct `os.*` usage despite `fs.FS` being passed in.
   Example: `open.go`, `path.go`, `clean.go`, `merge.go` use `os.Stat`/`os.WriteFile` even when `fsys` is available.
4. **medium** duplicate repo path validation logic.
   `run.go` validates `--repo` manually instead of reusing `normalizeRepoPath` or `ResolveRunContext`. This is drift waiting to happen.
5. **low** `resolve.go` creates a `store.NewStore` and then discards it.
   dead code; remove it or use it.
6. **medium** inconsistent time source across commands.
   `time.Now()` sprinkled everywhere for events and timestamps; no injected clock; no deterministic tests.
7. **high** event logging errors are swallowed.
   `events.AppendEvent` failures are ignored in `clean`/`resume`/`merge`/`push`. If events matter, handle errors; if not, remove the calls.
8. **low** data dir resolution duplicated inside the same flow.
   several commands resolve `RunContext` then re-resolve `dataDir` again. choose one.
9. **low** dead/no-op code in `runresolver.ResolveRepoContext`.
   it calls `paths.ResolveDirs` and ignores the result. delete it or use it.
10. **medium** direct interactive command exec ignores injected I/O.
   `open`, `attach`, and `worktree` force `os.Stdin/Stdout/Stderr` even when writers are passed. decide on a single I/O strategy and remove fake parameters.
11. **high** critical file writes ignore errors.
    e.g. `merge.go` writes the merge log with `_ = os.WriteFile(...)`. if logs are part of the contract, errors must be handled.
12. **high** unbounded prompt file reads.
   `agent` reads `--prompt-file` via `os.ReadFile` with no size limit; can blow memory.
13. **medium** `worktree` hardcodes the daemon socket path.
    it uses `dirs.DataDir + "/agencyd.sock"` instead of `store.DaemonSocketPath`, and ignores symlink normalization.
14. **high** github.com is hard-coded in multiple flows.
    `push`, `merge`, and `clean` reject non-github.com hosts; `ParseGitHubOwnerRepo` and PR URL regex also assume github.com.
15. **medium** PR fallback generation is unbounded.
    `writeFallbackPRBody` calls `git log` and `git diff --name-only` without limits, then discards most lines.
16. **high** `show --capture` emits events best-effort.
    capture is mutating and should fail hard on event write errors.
17. **high** `resume` emits events best-effort.
    these are lifecycle events; if they’re required, failing to append should fail the command.

**Audit: CLI (cobra)**
1. **high** all commands drop `cmd.Context()`.
   every cobra handler uses `context.Background()`; cancellation and deadlines never propagate.
2. **medium** command constructors do too much.
   they assemble deps and resolve cwd inside handlers; move this to a shared command context/bootstrap.

**Audit: CI / Build**
1. **high** CI only runs `go test ./...`.
   no `golangci-lint`, `go vet`, `fmt-check`, `mod-tidy-check`, or `-race`. the repo has `make check/verify` but CI ignores it. enforce these gates in CI.
2. **medium** no vulnerability scanning.
   no `govulncheck` or similar dependency security gate; add to CI if you care about supply-chain hygiene.

**Audit: Daemon**
1. **high** recursion guard is wrong.
   `isInsideAgencyManagedWorktree` ignores `worktrees` and relies on `HasPrefix`, which can misclassify. Fix path containment and include `worktrees`.
2. **high** recursion guard path check is unsafe.
   `isInsideAgencyManagedWorktree` uses `strings.HasPrefix(cleanPath, reposDir)` without a path boundary check. `/data/reposX/...` is treated as inside `/data/repos`. use `filepath.Rel` or `fs.IsSubpath`.
3. **high** worktree create path doesn’t enforce recursion guard.
   `handleWorktreeCreate` never calls `isInsideAgencyManagedWorktree`, so it can accept a repo root inside managed trees.
4. **high** request decoding is too permissive.
   no `DisallowUnknownFields` and no size limits on request bodies; easy to accept garbage silently.
5. **high** idempotency entries are in-memory only and not validated.
   duplicate responses can return stale `worktree_id`/paths after deletions or daemon restarts.
6. **high** os/exec usage violates the hard rule.
   `handlers.go` spawns runner processes with `osexec.Command` instead of `internal/exec`. unify process management.
7. **high** checkpoint engine drops cancellation context.
   `doFinalCheckpoint(context.Background())` ignores caller cancellation; use the passed ctx or a derived one.
8. **critical** unsafe deletes in landing.
   `daemon/landing/service.go` calls `os.RemoveAll(sandboxDir)` without subpath checks; use `fs.SafeRemoveAll` or explicit containment.
9. **high** landing events bypass the events subsystem.
   `landing/service.go` hand-rolls JSONL without repo/run ids, ignores errors, and skips file locking. unify on `events.AppendEvent`.
10. **medium** landing uses direct `os.*` and writes patch files with 0644.
   bypasses `fs.FS` and violates permission policy.
11. **high** checkpoint apply events are best-effort and bypass the events subsystem.
    `daemon/checkpoint/apply.go` writes JSONL directly with `os.OpenFile` and ignores errors.
12. **high** checkpoint engine events are best-effort and bypass the events subsystem.
    `daemon/checkpoint/engine.go` appends JSONL directly, ignores errors, and writes with 0644.
13. **high** stream parser drops write errors.
    `daemon/stream/parser.go` ignores failures writing to raw/stream files; if these are contractually required, errors must be surfaced.
14. **high** stream parser can allocate unbounded memory on huge lines.
    `bufio.ReadBytes('\n')` reads the full line into memory before size checks. enforce hard cap with `ReadSlice` or custom reader.
15. **high** headless runner inherits stdin.
    daemon spawns runners without `cmd.Stdin = nil` or `/dev/null`, so a headless runner can hang on stdin.
16. **medium** log files are created as 0644.
    raw/stderr/stream logs are world-readable; enforce 0600.
17. **medium** pidfile is created with 0644.
    `daemon/server.go` writes pid files world-readable; use 0600 and `fs.FS`.
18. **low** checkpoint engine bypasses `fs.FS`.
    `daemon/checkpoint/engine.go` uses `os.CreateTemp`, `os.ReadFile`, `os.WriteFile` directly.
19. **medium** no size limits for prompts/log writes.
    headless start writes prompt directly to disk with no max size; add limits to prevent huge files.
20. **high** legacy headless endpoint lacks modern validation.
    `/invocations/{id}/start_headless` doesn’t enforce prompt size or stricter request validation like control-plane.
21. **high** `client_request_id` is not validated as UUID.
    idempotency relies on it but accepts any string; validate format to avoid collisions.
22. **medium** runner env merge is nondeterministic.
    headless spawn appends `req.Env` to `os.Environ` with duplicates and no ordering guarantees.
23. **high** headless runner env lacks required defaults.
    no `CI=1`, `GIT_TERMINAL_PROMPT=0`, or `AGENCY_*` values; headless runners can block or run without context.
24. **high** recovery scan ignores schema_version.
    `runRecoveryScan` unmarshals repo_index without validating schema_version; should reject or force reset.
25. **high** pidfile read assumes trailing newline.
    `ReadPidFile` slices `data[:len-1]`; empty or newline-less files can panic or parse garbage.

**Audit: Core/Shared**
1. **high** `worktree.Create` lacks rollback.
   if scaffolding fails after `git worktree add`, the branch/worktree is left behind.
2. **medium** `fs.WriteJSONAtomic` uses raw `os.*` and bypasses `fs.FS`.
   inconsistent abstraction and impossible to stub in tests that use fake FS.
3. **high** `verify` and `archive` pipelines bypass `exec.CommandRunner`.
   they use `os/exec` directly and duplicate timeout/cleanup logic, diverging from `internal/exec` conventions.
4. **critical** event logging is not concurrency-safe.
   `events.AppendEvent` appends without file locking; concurrent commands can interleave JSONL and corrupt logs.
5. **medium** schema version constants are scattered.
   `"1.0"` appears in many packages with no central definition. this will drift on the next schema bump.
6. **high** `exec.RunScript` lies about stdin and leaks child processes.
   it claims stdin is `/dev/null` but `cmd.Stdin = nil` inherits parent stdin. also no process group kill on timeout; children can survive.
7. **critical** events are now contractually required, but code treats them as optional.
   `events.AppendEvent` is marked best-effort and callers ignore errors. make it a hard failure or move it out of the critical path intentionally.
8. **high** verify record writes are best-effort in error paths.
   `verify.writeRecordBestEffort` drops errors. if verify records are required, this must fail hard.
9. **high** verify.json parsing is too lenient and uses `os.*`.
   `verify.ReadVerifyJSON` only checks non-empty schema_version and bypasses `fs.FS`. should validate exact version or explicitly allow ranges.
10. **medium** integration marker checks bypass `fs.FS`.
    `integrationworktree.HasIntegrationMarker` uses `os.Stat` directly; should use injected FS.
11. **high** `internal/exec` API is too narrow for real processes.
    lack of a spawn/streaming interface forces `os/exec` in daemon, tmux, and commands. extend CommandRunner or add a new interface.
12. **medium** verify/archive use real filesystem directly.
   `verify/runner.go` and `archive/pipeline.go` use `os.*` for logs and paths, bypassing `fs.FS`.
13. **high** `core.NewRunID` has only 16 bits of randomness.
    4 hex chars is collision-prone. increase entropy and add collision checks.

**Audit: Store/FS/Exec**
1. **medium** `fs.FS` is incomplete, forcing `os.*` calls in `store` and elsewhere.
   missing `ReadDir`, `RemoveAll`, `OpenFile`, `CreateTemp` helpers for common flows.
2. **medium** `store` mixes `fs.FS` and direct `os.*` calls.
   scanning and directory creation bypass injected FS, breaking test isolation.
3. **medium** atomic write helpers are split and inconsistent.
   `WriteFileAtomic` uses `fs.FS` while `WriteJSONAtomic` uses `os.*`. pick one and use it everywhere.
4. **high** permissions are inconsistent and too open.
   `.agency/` dirs and logs are created with `0755/0644`; use `0700/0600` for private run data.
5. **high** events.jsonl is also written with 0644 and parent dirs 0755.
   `events.AppendEvent` should respect the same private permissions.
6. **high** env merging rules are inconsistent across runners.
   `exec.Run` appends env keys (ambiguous precedence) while runservice overrides; standardize on deterministic override.
7. **high** run meta schema version is never validated on read.
   `store.ReadMeta` accepts any schema_version; incompatible data can silently pass.
8. **critical** remove paths use raw `os.RemoveAll` without safety checks.
   `store/invocation.go` and `store/integration_worktree.go` delete directories directly; enforce `SafeRemoveAll` or explicit subpath checks.
9. **high** integration worktree and invocation meta schema versions are not validated.
   `ReadIntegrationWorktreeMeta` and `ReadInvocationMeta` accept any schema_version.
10. **medium** scan silently skips repo errors.
    `store.ScanAllRuns` ignores per-repo scan errors; this hides corruption and makes failures non-obvious.
11. **high** scans don’t enforce schema_version.
   `store.ScanInvocationsForRepo` only checks non-empty schema_version; should validate exact supported version.
12. **high** meta updates are non-atomic under concurrency.
   `UpdateMeta`/`UpdateInvocationMeta` do read-modify-write without file locks; concurrent updates can clobber.

**Audit: Tmux**
1. **high** `tmux/capture.go` ignores the shared CommandRunner.
   it defines a separate `Executor` and uses `os/exec`, duplicating behavior and violating the hard rule.

**Audit: Runnerstatus**
1. **high** schema_version is never validated on load.
   `runnerstatus.Load` ignores `SchemaVersion`, so incompatible files can slip through silently.
2. **low** no injected clock.
   `NewInitial` and `Age` use `time.Now()` directly, making tests nondeterministic.
3. **medium** uses `os.*` directly.
   `runnerstatus.Load` bypasses `fs.FS` and cannot be stubbed.

**Audit: Daemonclient**
1. **high** os/exec usage violates the hard rule.
   `autostart.go` spawns the daemon with `osexec.Command` and writes logs directly.
2. **high** `ReadRawLog` is broken (already listed) and hides errors.
3. **high** autostart has no timeout or health verification beyond polling.
   failure modes are silent and can leave zombie daemon processes.
4. **high** `NewClient` ignores context in `DialContext`.
   it calls `net.Dial` directly; cancellations and deadlines don’t apply. use `net.Dialer{}.DialContext`.
5. **medium** HTTP status codes are ignored.
   responses are decoded as JSON regardless of status; non-2xx bodies produce misleading decode errors.
6. **medium** `ReadRawLog` reads unbounded data into memory.
   `io.ReadAll` on large logs can blow memory. cap or stream.

**Audit: Runservice**
1. **high** os/exec usage violates the hard rule.
   setup script execution uses `osexec.CommandContext`; route through `internal/exec`.
2. **medium** time source is not injected consistently.
   `executeSetupScript` uses `time.Now()` directly instead of the service clock.
3. **medium** direct `os.*` file writes ignore `fs.FS`.
   setup logs and env handling are hardwired to the real filesystem, undermining testability.
4. **high** name uniqueness check is best-effort.
   `checkNameUnique` ignores scan errors and can allow duplicate active names; should fail hard.

**Audit: Archive**
1. **high** uses `os/exec` and duplicates `internal/exec` behavior.
   timeouts, env overrides, and cleanup diverge from the core runner.
2. **medium** logs are world-readable.
   `archive.log` uses 0644; use 0600 + 0700 dirs for private run data.
3. **high** env merge is non-deterministic.
   it appends `AGENCY_*` to `os.Environ` without de-duping; duplicate keys can win unpredictably.
4. **medium** time source is not injected.
   archive uses `time.Now()` directly (for duration) even though a service clock exists elsewhere.
5. **medium** filesystem abstraction is ignored.
   `Archive` and `runArchiveScript` use `os.*` instead of `fs.FS`.
6. **medium** script output is unbounded.
   archive logs can grow without limit; introduce max size or rotation.

**Audit: Verify**
1. **high** uses `os/exec` and reimplements process-group control.
   duplicates logic from `internal/exec` and violates the hard rule.
2. **medium** permissions are too open.
   verify logs and record dirs are `0755`, log is `0644`; use `0700/0600`.
3. **medium** time source is not injected.
   `Run` uses `time.Now()` directly; makes tests nondeterministic.
4. **high** verify.json is unbounded and lenient.
   `ReadVerifyJSON` uses `os.ReadFile` with no size cap and accepts any schema_version.
5. **medium** signal recording is inaccurate on timeout/cancel.
   it unconditionally sets `SIGKILL` even if the process exited for another reason.
6. **high** env merging is inconsistent.
   verify runner assumes caller merged env; other flows build env ad-hoc.

**Audit: Worktree**
1. **medium** `worktree.Remove` formats exit code incorrectly.
   it uses `string(rune(exitCode+'0'))`, which breaks for any multi-digit exit. use `fmt.Sprintf("%d", exitCode)`.
2. **low** `worktree.Remove` claims a fallback delete but never does it.
   `FallbackUsed` is dead and no `rm -rf` fallback exists. implement or remove the field and docs.

**Audit: Integration Worktree**
1. **medium** `Remove` returns fatal errors after a successful removal.
   if meta update fails, you report failure even though the worktree is gone; decide on rollback or downgrade to warning + repair.

**Audit: Invocation**
1. **high** sandbox path safety uses `HasPrefix` on cleaned paths.
   same bug as recursion guard: prefix checks can misclassify. use `filepath.Rel` and path boundary checks.
2. **medium** `Create` assumes `IntegrationWorktreeMeta` is non-nil.
   it will panic if the caller passes nil; validate inputs and return `E_INTERNAL` or `E_WORKTREE_NOT_FOUND`.
3. **medium** `HasSandboxMarker` bypasses `fs.FS`.
   it uses `os.Stat` directly, preventing stubs and in-memory fs tests.

**Audit: Verifyservice**
1. **medium** uses `os.Stat` and `os.ReadFile` directly.
   bypasses `fs.FS`, breaks testability, and violates the hard rule for shared filesystem access.
2. **high** events are still best-effort.
   `VerifyRunResult` collects append errors instead of failing. with contractually required events, this should fail hard.
3. **high** uses `LoadAgencyConfig` without validation.
   missing `scripts.verify.path` can slip through; should use `LoadAndValidate`.
4. **high** verify env is non-deterministic and sets repo_root to worktree.
   `buildVerifyEnv` appends to `os.Environ` and uses worktree as repo_root; standardize + inject actual repo root.
5. **high** augmentRecordError is best-effort and uses raw `os.*`.
   if events are required, this should be hard-fail and use `fs.FS`.

**Audit: Checkpoint**
1. **high** event sequence is not monotonic across daemon restarts.
   `eventSeq` is in-memory; after restart, seq resets to 0 while old events remain.
2. **high** checkpoint apply emits `Seq=1` unconditionally.
   breaks monotonic ordering within a single events.jsonl.
3. **critical** events are best-effort and unlocked.
   `appendEvent` ignores errors and does no file locking; JSONL can corrupt under concurrency.
4. **high** schema_version is not validated.
   `loadCheckpoints`/`LoadCheckpointsFile` accept any schema_version.
5. **medium** denylist is hardcoded and non-configurable.
   no way to tune for enterprise policies; also only matches basenames.
6. ~~**medium** fsnotify + polling can thrash large repos.~~
   **resolved**: gitignored directories are now excluded from fsnotify watches. the ignored set is pre-computed from `.gitignore` at invocation start, avoiding FD exhaustion on large trees.
7. **medium** prune errors are ignored.
   `update-ref -d` failures are dropped, leaving orphaned refs.
8. **medium** temp index location is unscoped.
   `os.CreateTemp("", ...)` writes to global temp; should be sandbox-scoped for safety and predictability.
9. **high** `GIT_INDEX_FILE` env override is not isolated.
   staged changes can race with other git operations without repo-level locking.

**Audit: Landing**
1. **medium** uses raw `os.*` for file ops.
   `os.Stat`, `os.RemoveAll`, `os.WriteFile` bypass `fs.FS` and safety checks.
2. **high** event schema is ad hoc and not the shared events contract.
   emits `{event, data}` records, not standard event kind, and ignores errors.
3. **high** repo lock is assumed, not enforced.
   service comments say “under repo lock,” but it doesn’t lock internally.
4. **medium** patch files use 0644.
   landing writes patch artifacts world-readable.
5. **high** missing validation for `repoRoot` and `baseCommit`.
   empty or invalid values can produce confusing git errors; validate up front.
6. **high** `repoRoot` is trusted input.
   no containment check to ensure it matches the expected repo for the invocation/worktree.

**Audit: Stream**
1. **high** normalized event seq is not persisted.
   `seq` resets each daemon run; ordering across restarts is not stable.
2. **high** writes ignore errors.
   raw/stream writes and event marshaling failures are dropped.
3. **medium** no fs abstraction.
   uses `*os.File` directly; tests can’t stub stream sinks.

**Audit: Events**
1. **critical** append is best-effort by design.
   `events.AppendEvent` comment says ignore errors, but we now require events in critical flows.
2. **critical** no file locking.
   concurrent writers can interleave JSONL; must lock or serialize per run.
3. **high** permissive file permissions.
   creates dirs 0755 and files 0644; should be 0700/0600 for private run data.
4. **high** schema_version is not enforced on read.
   if events are contractual, add validation + tooling to detect corrupt events.

**Audit: Push**
1. **high** github.com-only gate blocks enterprise/ssh.
   `ParseOriginHost` + `ParseGitHubOwnerRepo` + PR URL regex reject non-github.com.
2. **high** events are best-effort.
   `appendPushEvent` ignores errors; violates required-events rule.
3. **medium** retries ignore context.
   `viewPRWithRetry` sleeps without checking ctx cancellation.
4. **medium** fallback PR body generation is unbounded.
   `git log`/`diff --name-only` can output massive data; cap with `-n` and `--max-count`.
5. **high** PR URL regex is hard-coded to github.com.
   fails on GH enterprise; must parse host-aware URLs.
6. **medium** report parsing reads unbounded content.
   `report.CheckCompleteness` reads entire report; cap size or stream.
7. **high** gh host is not passed through.
   for enterprise, set `GH_HOST` or equivalent in env; otherwise gh hits github.com.

**Audit: Merge**
1. **high** github.com-only gate blocks enterprise/ssh.
   `parseOriginHost` + `ParseGitHubOwnerRepo` enforce github.com.
2. **high** events are best-effort.
   `appendMergeEvent` ignores errors; violates required-events rule.
3. **medium** retries ignore context.
   `confirmPRMerged` and `viewPRFullWithRetry` sleep without checking ctx cancellation.
4. **high** merge log writes ignore errors and use 0644.
   `executeGHMerge` drops write errors; log file should be 0600.
5. **high** `buildVerifyEnvForMerge` sets `AGENCY_REPO_ROOT` to worktree path.
   likely wrong; should be actual repo root.
6. **medium** multiple origin parsers exist.
   `parseOriginHost` duplicates `git.ParseOriginHost`; unify behavior.
7. **high** gh host is not passed through.
   for enterprise, set `GH_HOST` or equivalent in env; otherwise gh hits github.com.

**Audit: Clean**
1. **high** github.com-only gate blocks enterprise/ssh.
   branch deletion and PR close are skipped for enterprise hosts.
2. **high** events are best-effort.
   all `events.AppendEvent` calls ignore errors.
3. **high** PR close uses `ParseGitHubOwnerRepo` which only supports github.com.
   enterprise PRs will never be closed.
4. **high** gh host is not passed through.
   for enterprise, set `GH_HOST` or equivalent in env; otherwise gh hits github.com.

**Audit: Config**
1. **medium** `parseUserConfigStrict` ignores unknown keys inside `defaults`.
   typos are silently accepted; treat as invalid config.
2. **high** `ResolveRunnerCmd`/`ResolveEditorCmd` allow `..` path traversal.
   relative paths should be cleaned and constrained to `configDir` or rejected.
3. **medium** user config permissions are too open.
   `agency init` creates `config.json` with 0644 and config dir 0755. for a user-only tool, make it 0600/0700 by default.

**Audit: Git / Identity**
1. **high** `ParseOriginHost` rejects valid hosts without dots and ignores `ssh://`.
   that breaks GitHub Enterprise or internal hosts. either support or explicitly error early.
2. **high** repo_id is only 64 bits.
   `RepoIDLen = 16` hex chars is collision-prone. use 128+ bits or full sha256 for “gold standard.”
3. **high** repo_key for github repos ignores host.
   for enterprise, `github:owner/repo` collides across hosts. include host in repo_key, e.g. `github:<host>/<owner>/<repo>`.

**Audit: IDs**
1. **medium** ambiguous run errors are not disambiguating.
   `ids.ErrAmbiguous.Error` prints only run_ids; when the same run_id exists in multiple repos, the message is useless. include repo_id or name.
2. **medium** ambiguous worktree/invocation errors are weak.
   `ErrWorktreeAmbiguous` and `ErrInvocationAmbiguous` don’t include repo_id; collisions across repos are ambiguous and unhelpful to humans.

**Audit: Lock**
1. **high** `RepoLock` contradicts pid-only staleness.
   it steals locks by age when the lock file is unreadable, violating the spec and risking concurrent writers.
2. **high** lock files lack start-time or nonce.
   pid reuse can make stale locks look alive forever; store a start timestamp and verify.

**Audit: Paths**
1. **high** env overrides are not canonicalized.
   `paths.ResolveDirs` accepts relative and symlinked paths; later containment checks and equality comparisons can be wrong. enforce absolute + clean + EvalSymlinks or reject non-absolute.
2. **high** homeDir is assumed absolute but never validated.
   `ResolveDirs` docs require absolute; invalid input silently produces garbage paths. validate or normalize at the boundary.

**Audit: Render**
1. **medium** writer errors are swallowed.
   `render.WriteShowHuman`, `render.WriteConflictCard`, and related helpers discard `fmt.Fprintf` errors and return nil, so broken pipes look like success. return errors or use a checked writer helper.
2. **high** conflict action cards are not shell-safe.
   `render.WriteConflictCard` prints commands with raw `ref` and `worktreePath`. spaces/quotes break copy-paste and enable injection. use `core.ShellEscapePosix` for arguments.
3. **medium** show --json leaks internal storage schema.
   `render.RunDetail.Meta` is `*store.RunMeta`; any internal schema change becomes a public API change. define a stable DTO and map fields explicitly.

**Audit: Errors**
1. **medium** log tailing breaks on long lines.
   `errors.readTail` uses `bufio.Scanner` with the 64k token limit; long log lines cause `ErrTooLong` and drop output. set `scanner.Buffer` or use `bufio.Reader`.
2. **low** error printing ignores write failures.
   `PrintWithOptions` drops `io.WriteString` errors; broken pipe exits 0 and hides real failure. propagate or surface.

**Audit: Pipeline**
1. **high** no rollback/cleanup on partial failure.
   if a mid-step fails, worktrees, run dirs, or tmux sessions can be left behind. add compensating actions or a cleanup step.

**Audit: Process Execution**
1. **high** script execution relies on `sh -lc` everywhere.
   setup/verify/archive and runner spawn go through a shell even when the script path is known and executable. this adds injection risk, hides argv boundaries, and makes quoting bugs inevitable. prefer explicit argv execution and reserve shells for truly shell‑based commands.

**Audit: Watchdog**
1. **low** time source is implicit.
   `watchdog.CheckStall` uses `time.Since` internally; tests are nondeterministic and clock jumps can skew results. inject `now` or a clock interface.

**Audit: Comments**
1. **low** `core.ShellEscapePosix` example is wrong.
   the empty-string example shows a non-ASCII quote character and doesn’t match the actual return value `''`. this is small, but it’s still slop.

**Audit: Repo Hygiene**
1. **low** runtime artifacts committed to source tree.
   `internal/runservice/repos/runs/logs/setup.log` looks like generated data and should live under `testdata/` or be removed.

**Out Of Scope (Explicit Non-Goals)**
1. windows support.
2. crash-durable storage (fsync on files + directories).
3. large-scale indexing beyond ~1k runs.
4. daemon authentication / multi-user isolation.

**Code Smells / Bugs**
1. **high** `ReadRawLog` likely broken and silently fails.
   Uses `http.DefaultClient.Get("file://...")`, which net/http doesn’t handle; returns empty data on error. Use `os.ReadFile` or `fs.FS` and return errors.
2. **high** `Store.NewStore` accepts `Now == nil`, and callers pass nil.
   If any code path calls `s.Now()`, it will panic. Provide a default (`time.Now`) in `NewStore` or enforce non-nil.
3. **medium** Abstraction leaks: direct `os.*` calls bypass `fs.FS`.
   Some command paths bypass `fs.FS` and environment abstraction, reducing testability and portability.
4. **high** `exec.Run` env override behavior is ambiguous.
   It appends `Env` entries without removing existing keys. Duplicate keys can yield platform-dependent results. Normalize env before exec.
5. **high** Run pipeline uses current branch as a proxy for parent in some flows.
   `runservice.checkRepoContextOnly` falls back to current branch when parent is deferred. That’s a semantic mismatch and can mask invalid parent config.
