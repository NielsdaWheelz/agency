# Transport

## Scope

This document covers transport lifecycle ownership.

## Lifecycle

- The daemon transport is a local Unix socket carrying strict JSON requests and responses.
- Transport layers should not own business policy that belongs in commands, handlers, or lower-level services.
- Transport may acknowledge `worktree pr merge` request acceptance, but it does not own subsequent merge execution or recovery.
- API version checks must happen before a client relies on daemon behavior.
- Request ids should propagate through transport responses for correlation.
- Recovery after disconnect should come from durable daemon state, not from transport-local memory.
- Transport disconnects, client exits, and request-context cancellation must not cancel accepted `worktree pr merge` lifecycles.
- Callers that need merge progress or outcome should perform explicit daemon status reads; a POST response is not a long-lived status channel.
