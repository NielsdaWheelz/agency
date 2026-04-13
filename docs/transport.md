# Transport

## Scope

This document covers transport lifecycle ownership.

## Lifecycle

- The daemon transport is a local Unix socket carrying strict JSON requests and responses.
- Transport layers should not own business policy that belongs in commands, handlers, or lower-level services.
- API version checks must happen before a client relies on daemon behavior.
- Request ids should propagate through transport responses for correlation.
- Recovery after disconnect should come from durable daemon state, not from transport-local memory.
