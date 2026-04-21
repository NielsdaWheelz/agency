# Daemon

## Scope

This document covers daemon ownership, lifecycle, and mutation rules.

## Rules

- The daemon is the canonical mutable owner for repo registry, integration worktree, and invocation control-plane state.
- CLI commands may validate inputs locally, but should route daemon-owned mutations through the daemon client.
- `agency worktree <worktree-ref> pr merge` is daemon-owned end to end: acceptance, durable state transitions, execution, archive cleanup, and recovery all live under daemon control.
- Daemon socket, pid, and log paths live under `AGENCY_DATA_DIR`.
- Clients must check daemon API compatibility before relying on daemon behavior.
- Mutating daemon handlers must preserve request ids and stable JSON envelopes.
- Recovery after restart must derive from durable state, not from transport-local memory.
- The daemon must persist worktree merge lifecycle state around external merge and archive steps so restart can resume or explain the outcome.
- Git-mutating daemon flows must take the repo lock.
- Accepted worktree merge lifecycles continue independently of the transport connection; disconnects do not cancel accepted work.
- Clients should use explicit merge-status reads instead of inferring worktree merge completion from the POST lifecycle.
- Headed recreate is daemon-owned: it preserves the invocation id and sandbox, and recreates only tmux/session supervision state.
