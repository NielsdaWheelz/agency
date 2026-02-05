# Event Writing Guide

This is binding for any code that writes events.

## Rules

1. use the events API
   do not hand-roll JSONL. use internal/events and the shared append path.

2. required fields
   schema_version, timestamp (RFC3339 UTC), repo_id, run_id, event, and data object.

3. failure behavior
   event append failures must fail the operation in contract flows.

4. locking and atomicity
   event writes must be locked per run/invocation. no interleaved JSON lines.

5. permissions
   event files are private: 0600 file, 0700 parent dirs.

6. stable data
   data keys must be stable, documented, and deterministic. avoid secrets and large payloads.

7. documentation and tests
   when adding or changing event names or data shape:
   - update docs/contracts/events.md
   - add or update tests that assert the event shape

## Checklist

- [ ] event name added to docs/contracts/events.md
- [ ] tests cover success and append failure
- [ ] data keys are stable and deterministic
- [ ] permissions and locking enforced
