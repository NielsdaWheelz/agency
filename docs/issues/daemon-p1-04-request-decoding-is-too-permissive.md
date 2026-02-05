# [p1][daemon][tech-debt] request decoding is too permissive

labels: `p1`, `type:tech-debt`, `area:daemon`

## summary
request decoding is too permissive

## context
- section: Audit: Daemon
- source: docs/issues.md
- details:
  - no `DisallowUnknownFields` and no size limits on request bodies; easy to accept garbage silently.

## acceptance criteria
- [ ] define minimal fix + tests

