# Keys And Identities

## Scope

This document covers repository, worktree, invocation, and run identity rules.

## Stable Identity

- Repo identity is derived from the repo root plus origin information.
- Directory ids such as `repo_id`, `worktree_id`, `invocation_id`, and `run_id` are canonical identity.
- Human-readable names are labels, not identity.
- If a value's identity changes, it is not the same entity.

## Path Identity

- Path comparisons must use clean, absolute, symlink-resolved paths.
- Persisted repo roots may be preferred over live rediscovery when the persisted value is canonical and still accessible.

## Lookup Rules

- Resolve by id when possible.
- Name-based lookup is for convenience and must still resolve to one canonical id.
