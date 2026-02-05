# [p1][runtime][refactor] runtime dirs single source of truth (plan)

labels: `p1`, `type:refactor`, `area:runtime`

## summary
runtime dirs single source of truth (plan)

## context
- section: merged
- source: docs/issues.md (merged)
- merged items:
  - Define a single source of truth for runtime dirs
  - Bootstrap once at the boundary
  - Create test-first helpers that enforce the standard
  - Add new “gold-standard” entry points without breaking existing APIs
  - Set the migration rule for code changes
  - Add a low-friction guardrail
  - Decommission overrides gradually
- details:
  - Use a small package, e.g. `internal/runtime` or `internal/appctx`, with a struct like `RuntimeDirs{DataDir, ConfigDir, CacheDir}`. Provide `ResolveRuntimeDirs(env paths.Env, homeDir string) (RuntimeDirs, error)`. Optionally add `ResolveRuntimeDirsFromOS() (RuntimeDirs, error)`.
  -
  - Resolve `RuntimeDirs` in CLI bootstrap (cobra root / command init) and pass it down via a lightweight `CommandContext`.
  -
  - Add `internal/testutil/runtime.go` with `NewRuntimeDirs(t)` that uses `t.TempDir()` and returns a fully-populated `RuntimeDirs`. New tests should use this helper and pass `RuntimeDirs` down instead of `DataDirOverride`.
  -
  - Example: `ResolveRunContextWithDirs(ctx, cr, cwd, repoPath, dirs)` in `internal/commands/runresolver.go`. Keep `ResolveRunContext(...)` as a thin wrapper that calls the new function with resolved dirs from OS/env.
  -
  - When touching any command/service that uses `DataDirOverride`, refactor that unit to accept `RuntimeDirs` (or a lightweight `CommandContext` containing `RuntimeDirs`) and eliminate any local `os.UserHomeDir + paths.ResolveDirs` duplication. No new `DataDirOverride` fields in new opts. Leave untouched code as-is for now.
  -
  - Add a short comment in the new helper or `AGENTS.md` to set expectations: “New tests should use `internal/testutil.NewRuntimeDirs` and pass dirs explicitly.” Optional: add a small `go test` check or `rg`-based CI check later to flag new direct uses of `paths.ResolveDirs` outside the runtime package.
  -
  - As commands are migrated, mark `DataDirOverride` as `Deprecated:` in comments. Only remove `DataDirOverride` once every caller has switched to the injected `RuntimeDirs`.
  -

## acceptance criteria
- [ ] define minimal fix + tests

