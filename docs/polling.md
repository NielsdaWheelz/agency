# Polling

## Scope

This document covers polling rules.

## Rules

- Avoid polling by default.
- Prefer daemon health checks, stream processing, and append-only state over blind polling loops.
- If polling is unavoidable, keep cadence and termination rules together.
- UI refresh polling is acceptable in `watch` because it is a read-only view over daemon state.
- Mutation flows should not depend on unbounded polling for correctness.
