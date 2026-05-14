# Correctness

## Scope

This document covers system abnormality classification and repository-wide correctness invariants.

## Abnormalities

- Expected abnormalities must be modeled in code.
- Expected abnormalities in this repo include daemon restart, missing tmux session, stopped or killed runner processes, stale lock files, and transient git or socket failures inside bounded retry windows.
- Unexpected abnormalities indicate a broken invariant and should trigger investigation.
- See [retries.md](retries.md) for retry policies and exhaustion handling.

## Invariants

- If concurrent execution or crash-and-replay can produce an incorrect result, it is a bug.
- Every operation must correspond to some valid sequential ordering of all concurrent operations.
- Every committed external side effect must be discoverable during recovery from persisted metadata, append-only events, or the filesystem state itself.
- Path comparisons and repo identity checks must use canonicalized paths.
- Read-only views that span daemon, store, git, and tmux state must tolerate transient inconsistency.

## Untrusted Data

- Parse and validate untrusted JSON, CLI input, and daemon request bodies at the boundary.
- Normalize only at ingress. After the boundary, treat the value as trusted and canonical.
- Validate the single expected type and schema. Do not add speculative coercions for “maybe” formats.
