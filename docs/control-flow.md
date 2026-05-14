# Control Flow

## Scope

This document covers exhaustive branching and race-safety rules.

## Exhaustiveness

- When branching on a finite set of internal states or error codes, use explicit branches for each known value.
- Do not use default branches that silently accept new internal enum values.
- Persisted or API-visible state strings should be represented by typed constants in the owning package.
- When handling errors by code, branch on named codes instead of substring matching.

## Races

- Do not race destructive or non-idempotent operations unless losing the result is acceptable.
- If competing operations need to coordinate around one repo, sandbox, or tmux session, route them through one serialization point.
