# [p1][daemon][tech-debt] client_request_id is not validated as UUID

labels: `p1`, `type:tech-debt`, `area:daemon`

## summary
client_request_id is not validated as UUID

## context
- section: Audit: Daemon
- source: docs/issues.md
- details:
  - idempotency relies on it but accepts any string; validate format to avoid collisions.

## acceptance criteria
- [ ] define minimal fix + tests

