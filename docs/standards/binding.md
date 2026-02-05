# Binding Engineering Rules

These rules are required. If a change touches a rule, add or update enforcement (tests, lint rules, or CI checks) in the same PR. If enforcement cannot be added, document why and downgrade to advisory.

## Rules

1. process execution
   no os/exec outside internal/exec. all spawning, streaming, pty, and process-group control must go through internal/exec.

2. events are required
   in mutating flows, event writes are mandatory. append failure must fail the operation. events must be atomic and locked per run.

3. deterministic env merge
   a single merge function. override wins. no duplicate keys. keys sorted for reproducibility.

4. strict schema versions
   reject unknown or empty schema_version. no silent fallbacks. delete data dir is an acceptable reset path.

5. safe delete
   no raw os.RemoveAll on user-provided or derived paths. use fs.SafeRemoveAll with containment checks.

6. private permissions
   private data uses 0700 dirs and 0600 files. do not create world-readable logs or state.

7. canonical paths
   comparisons require absolute, clean, symlink-resolved paths.

8. repo/worktree locks
   any git mutation or worktree mutation must hold the repo lock.

9. no global chdir
   do not use os.Chdir in command flow. pass working dirs explicitly.

10. github enterprise support
    ssh and enterprise hosts must be supported. no github.com-only assumptions.

## Enforcement Expectations

- go test ./... must pass
- lint/vet/format/tidy/race/vuln checks must pass (once wired in CI)
- unit/integration tests must cover error codes and required event flows
