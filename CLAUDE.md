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

- docs/standards/binding.md
- docs/contracts/events.md
- .claude/prompts/test-writing.md
- .claude/prompts/event-writing.md

## Runner Protocol

Update `.agency/state/runner_status.json` at milestones:

| Status | When | Required Fields |
|--------|------|-----------------|
| `working` | Actively making progress | `summary` |
| `needs_input` | Waiting for user answer | `summary`, `questions[]` |
| `blocked` | Cannot proceed | `summary`, `blockers[]` |
| `ready_for_review` | Work complete | `summary`, `how_to_test` |

Schema:

```json
{
  "schema_version": "1.0",
  "status": "working",
  "updated_at": "2026-01-19T12:00:00Z",
  "summary": "Implementing user authentication",
  "questions": [],
  "blockers": [],
  "how_to_test": "",
  "risks": []
}
```

Before `ready_for_review`, update `.agency/report.md` with summary, decisions, testing instructions, and risks.
