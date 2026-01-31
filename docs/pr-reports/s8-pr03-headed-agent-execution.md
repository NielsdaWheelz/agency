# Slice 8 PR-03: Headed Agent Execution Report

## Overview

This PR implements **headed (interactive) agent execution** inside sandbox worktrees using tmux for lifecycle control. It's part of Slice 8's goal to move Agency from "run = everything" into a composable orchestration system where agents work in isolated sandboxes.

## What Changed

### New Commands

| Command | Description |
|---------|-------------|
| `agency agent start --worktree <ref>` | Start agent in sandbox with tmux (was sandbox-only in PR-02) |
| `agency agent attach <id>` | Attach to running headed invocation |
| `agency agent stop <id>` | Send graceful interrupt (Ctrl-C) |
| `agency agent kill <id>` | Forcefully terminate tmux session |

### New Flags

| Flag | Command | Description |
|------|---------|-------------|
| `--detached` | `agent start` | Start but don't attach |

### Modified Files

| File | Changes |
|------|---------|
| `internal/store/invocation.go` | Added `stop_requested_at`, `InvocationFlags` struct |
| `internal/errors/errors.go` | Added `E_INVOCATION_INVALID_MODE`, `E_INVOCATION_START_FAILED`, etc. |
| `internal/commands/agent.go` | Added tmux execution to `AgentStart`, implemented `AgentAttach`, `AgentStop`, `AgentKill` |
| `internal/cli/cobra/agent.go` | Added cobra commands for attach/stop/kill, `--detached` flag |
| `internal/tmux/client.go` | Added comment about SessionName (already existed in capture.go) |

### New Test File

| File | Description |
|------|-------------|
| `internal/commands/agent_test.go` | Tests for headed execution, attach/stop/kill behavior |

## Architecture

```
agent start --worktree foo
    │
    ├─► Create sandbox (PR-02 logic)
    │     • Verify INTEGRATION_MARKER
    │     • Create sandbox worktree
    │     • Write SANDBOX_MARKER
    │     • Write invocation meta (status=starting)
    │
    ├─► Resolve runner command
    │     • Load user config
    │     • Find claude/codex on PATH
    │
    ├─► Preflight check
    │     • tmux.HasSession(agency_<id>)
    │     • Abort if exists (E_TMUX_SESSION_EXISTS)
    │
    ├─► Create tmux session
    │     • CWD = sandbox tree path
    │     • argv = [runnerCmd] (no shell wrapper)
    │
    └─► Update meta (status=running, tmux_session set)
```

## Invariants Enforced

1. **Runners never execute in integration trees** — INTEGRATION_MARKER checked before sandbox creation
2. **Each invocation owns one sandbox** — 1:1 mapping, no sharing
3. **No state mutation on read** — `agent ls`, `agent show` never modify records
4. **Explicit lifecycle** — Only `start`, `stop`, `kill` modify invocation state

## Error Handling

| Scenario | Behavior |
|----------|----------|
| tmux session exists before start | Abort with `E_TMUX_SESSION_EXISTS` |
| tmux creation fails | Mark `status=failed`, `exit_reason=start_failed` |
| Attach to headless | `E_INVOCATION_INVALID_MODE` |
| Kill when session gone | Still update meta (user intent honored) |

## Testing

```bash
# Unit tests
go test ./internal/commands/... -run TestAgent -v

# Full suite
go test ./...
```

### Test Coverage

| Test | Validates |
|------|-----------|
| `TestAgentStart_HeadedMode_CreatesSession` | Session created with sandbox CWD |
| `TestAgentStart_HeadedMode_ExistingSessionFails` | Preflight rejects duplicate sessions |
| `TestAgentAttach_HeadlessInvocation_ReturnsInvalidMode` | Mode validation |
| `TestAgentAttach_HeadedInvocation_SessionMissing` | Session existence check |
| `TestAgentStop_HeadedInvocation_SendsCtrlC` | C-c sent, metadata updated |
| `TestAgentKill_HeadedInvocation_KillsSession` | Session killed, status=failed |
| `TestAgentKill_SessionAlreadyGone_StillUpdatesMetadata` | Idempotent metadata update |

## Usage Examples

```bash
# Create integration worktree
agency worktree create --name my-feature

# Start headed agent (attaches automatically)
agency agent start --worktree my-feature

# Or start detached
agency agent start --worktree my-feature --detached
agency agent attach 20260131

# Graceful stop
agency agent stop 20260131

# Force kill
agency agent kill 20260131
```

## What's Next (Future PRs)

- **PR-04**: Headless execution (subprocess, raw.jsonl logging)
- **PR-05**: Stream parsing for semantic status
- **PR-06**: Checkpointing via private refs
- **PR-07**: Landing workflow (diff/land/discard)
- **PR-08**: Watch TUI
