# Daemon

## Scope

This document covers daemon ownership, lifecycle, and mutation rules.

## Rules

- The daemon is the canonical mutable owner for integration worktree and invocation control-plane state.
- CLI commands may validate inputs locally, but should route daemon-owned mutations through the daemon client.
- Daemon socket, pid, and log paths live under `AGENCY_DATA_DIR`.
- Clients must check daemon API compatibility before relying on daemon behavior.
- Mutating daemon handlers must preserve request ids and stable JSON envelopes.
- Recovery after restart must derive from durable state, not from transport-local memory.
- Git-mutating daemon flows must take the repo lock.
