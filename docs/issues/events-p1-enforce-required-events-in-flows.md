# [p1][events][tech-debt] enforce required events in command flows

labels: `p1`, `type:tech-debt`, `area:events`

## summary
enforce required events in command flows

## context
- section: merged
- source: docs/issues.md (merged)
- merged items:
  - events are now contractually required, but code treats them as optional
  - event logging errors are swallowed
  - show --capture emits events best-effort
  - resume emits events best-effort
  - events are best-effort
  - events are best-effort
  - events are best-effort
  - events are still best-effort
- details:
  - `events.AppendEvent` is marked best-effort and callers ignore errors. make it a hard failure or move it out of the critical path intentionally.
  -
  - `events.AppendEvent` failures are ignored in `clean`/`resume`/`merge`/`push`. If events matter, handle errors; if not, remove the calls.
  -
  - capture is mutating and should fail hard on event write errors.
  -
  - these are lifecycle events; if they’re required, failing to append should fail the command.
  -
  - `appendPushEvent` ignores errors; violates required-events rule.
  -
  - `appendMergeEvent` ignores errors; violates required-events rule.
  -
  - all `events.AppendEvent` calls ignore errors.
  -
  - `VerifyRunResult` collects append errors instead of failing. with contractually required events, this should fail hard.
  -

## acceptance criteria
- [ ] define minimal fix + tests

