# Naming

## Scope

This document covers repository-wide naming rules for Go identifiers and stable external labels.

## Go Identifiers

- Package names are lowercase.
- Exported Go identifiers use `PascalCase`.
- Unexported Go identifiers use `camelCase`.

## Stable External Names

- Error codes use `E_UPPER_SNAKE_CASE`.
- Environment variables use `AGENCY_UPPER_SNAKE_CASE`.
- Persisted JSON field names use `snake_case`.
- Persisted enum and status values use one canonical lowercase spelling.
- Canonical runner ids use lowercase names with dashes.

## Identity Names

- Request ids, repo ids, worktree ids, invocation ids, and run ids are identifiers, not labels.
- Human-readable names such as worktree names and invocation names are not identity.
