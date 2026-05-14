# Keep-Alive

## Scope

This document covers liveness and health-check policies.

## Policies

- Liveness checks for the daemon, tmux-backed sessions, and supervised runners should use named intervals and bounded wait windows.
- Health and readiness checks belong in the daemon or daemon client layers, not scattered across command handlers.
- Keep-alive intervals are not data-retention or expiry rules.
