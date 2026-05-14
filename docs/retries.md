# Retries

## Scope

This document covers retry boundary semantics and repository-wide retry policies.

## Boundaries

- Retry the smallest unit of work that can be retried safely.
- A retryable failure should be classified at the layer that owns the retry policy.
- Retry exhaustion should become a stable user-facing error or an explicit internal defect.

## Policies

- Keep retry constants near the operation they govern.
- Bounded daemon startup, socket health, git, and GitHub retries are acceptable when the dependency is transient.
- Do not add hidden or unbounded retries.
- Retry schedules follow the duration rules in [timing.md](timing.md).

## Exhaustion

- Exhaustion should leave the system in a state that recovery can explain from durable metadata.
- Exhaustion in user-facing flows must surface a stable error code.

## Placement

- Place retry logic as close to the operation being retried as possible.
- Retry the smallest necessary unit of work.
- Do not structure retries so it is ambiguous whether a deeper layer is already retrying the same work.

## In-Memory State

- Do not carry mutable attempt state across a retry boundary when each attempt must start from a clean view.
- Create attempt-local state inside the retryable block if it must reset on each attempt.
