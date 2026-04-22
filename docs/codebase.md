# Codebase

## Scope

This document covers repository-wide package organization and ownership boundaries.

## Layout

- `cmd/agency/` is the binary entrypoint.
- `internal/cli/cobra/` owns Cobra command wiring and flag parsing.
- `internal/commands/` owns the canonical user-facing command contracts.
- `internal/config/` owns user config, repo config, current-context files, and config resolution.
- `internal/daemon/` owns daemon lifecycle, handlers, reconciliation, streaming, checkpointing, landing flows, and the durable worktree PR merge lifecycle.
- `internal/daemonclient/` owns the daemon transport client.
- `internal/integrationworktree/`, `internal/invocation/`, `internal/mergeflow/`, and `internal/verify/` own domain logic below the command boundary.
- `internal/ids/` and `internal/identity/` own reference resolution and durable repo identity rules.
- `internal/store/` owns on-disk layout, schema types, scans, load/save behavior, and persisted merge-state files.
- `internal/watch/` owns the read-model and Bubble Tea workspace UI.
- `internal/render/`, `internal/tui/`, and `internal/tty/` own shared rendering and terminal primitives.
- `internal/events/`, `internal/jsonl/`, and `internal/runnerstatus/` own shared event, log, and runner-status formats.
- `internal/exec/`, `internal/git/`, `internal/tmux/`, `internal/fs/`, and `internal/lock/` are infrastructure seams.
- Package-specific docs live in [modules/index.md](modules/index.md).

## Boundaries

- Lower-level packages do not print to stdout or stderr.
- Lower-level packages do not parse Cobra flags or inspect terminals directly.
- Command packages may render output and translate DTOs into user-facing text.
- Store packages own file schemas and paths, not git or tmux policy.
- Daemon packages own mutating control-plane policy when a daemon surface exists, including accepted worktree merge execution and recovery.
- Infrastructure packages should remain reusable and stub-friendly.
