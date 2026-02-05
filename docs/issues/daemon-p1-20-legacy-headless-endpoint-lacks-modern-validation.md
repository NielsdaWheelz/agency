# [p1][daemon][tech-debt] legacy headless endpoint lacks modern validation

labels: `p1`, `type:tech-debt`, `area:daemon`

## summary
legacy headless endpoint lacks modern validation

## context
- section: Audit: Daemon
- source: docs/issues.md
- details:
  - `/invocations/{id}/start_headless` doesn’t enforce prompt size or stricter request validation like control-plane.

## acceptance criteria
- [ ] define minimal fix + tests

