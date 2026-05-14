# Agency Project Contract

This file is binding for contributors and agents. If a rule is listed under Binding Rules, it must be enforced in code and tests. If enforcement is missing, add it in the same PR or treat the change as incomplete.

## Binding Rules

1. process execution
   no os/exec outside internal/exec. process spawning, streaming, pty, and process-group control live in internal/exec.

2. events
   events are contractually required in mutating flows. append failure must fail the operation. events must be written atomically, with locking, and private permissions (0700 dirs, 0600 files).

3. env merge
   env merging is deterministic: override wins, no duplicate keys, keys sorted for reproducibility.

4. schema versions
   schema_version is strict. reject unknown/empty versions. no silent fallbacks. delete data dir is an acceptable reset path.

5. safe delete
   no raw os.RemoveAll on user-provided or derived paths. use fs.SafeRemoveAll with containment checks.

6. paths
   path comparisons must use absolute, clean, symlink-resolved paths.

7. locks
   repo/worktree mutations must take repo-level locks.

8. no global chdir
   do not call os.Chdir in command flow. pass working dirs explicitly.

9. github enterprise support
   support ssh and enterprise hosts. no github.com-only assumptions.

## Advisory Rules

- avoid god files and duplication.
- avoid unbounded reads/writes; cap sizes.
- prefer explicit dependencies and injected clocks.

## References

- docs/process-execution.md
- docs/persistence.md
- docs/daemon.md
- docs/testing.md

## Runner Protocol

`.agency/state/runner_status.json` is the only runner contract.

It is the only semantic state contract for an invocation.
Do not model separate semantic, display, or readiness layers in runner output.

Update it at milestones:

| State | When | Required Fields |
|--------|------|-----------------|
| `running` | Actively executing work | `summary` |
| `waiting` | Not executing right now. Use this for both turn-complete idle and waiting for user input. | `summary` |
| `succeeded` | Work is complete and validated enough to hand back. | `summary`, `how_to_test` |
| `failed` | Work cannot complete successfully. | `summary` |

Rules:

- Use exactly one canonical `state`.
- `waiting` covers both done-and-idle and waiting-for-user cases.
- Use `reason` when `waiting` or `failed` needs clarification.
- When `state` is `waiting` because the runner needs a user answer, include `questions[]`.
- `blocked` is removed from the runner and user-facing vocabulary. Do not write it.
- `ready` is removed. Use `succeeded`.
- `needs_input` is removed. Use `waiting`.
- `working` is removed. Use `running`.

Schema:

```json
{
  "schema_version": "2.0",
  "state": "waiting",
  "updated_at": "2026-01-19T12:00:00Z",
  "reason": "awaiting_user_input",
  "summary": "I finished the API refactor and need the preferred webhook path before I update the client.",
  "questions": [
    "Should the webhook stay at /webhooks/github or move to /api/github/webhook?"
  ]
}
```

Before finishing successfully, set `state` to `succeeded` and include `summary` and `how_to_test`.
