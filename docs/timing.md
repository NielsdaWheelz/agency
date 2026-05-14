# Timing

## Scope

This document covers timing parameters and schedule-shape rules.

## Schedules

- Retry and polling schedules should be self-bounding.
- Keep a schedule's cadence and termination behavior in the same schedule definition.
- Do not split one policy across unrelated constants in different files.

## Constants

- Timing parameters such as retry intervals, backoff caps, timeouts, and polling periods should be named constants.
- Represent timing parameters as `time.Duration` values.
- Avoid raw numbers and anonymous inline durations in business logic.
- If a duration is only meaningful as part of one named policy constant, it may be embedded inside that constant.
