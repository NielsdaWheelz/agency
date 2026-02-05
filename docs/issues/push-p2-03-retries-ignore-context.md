# [p2][push][tech-debt] retries ignore context

labels: `p2`, `type:tech-debt`, `area:push`

## summary
retries ignore context

## context
- section: Audit: Push
- source: docs/issues.md
- details:
  - `viewPRWithRetry` sleeps without checking ctx cancellation.

## acceptance criteria
- [ ] define minimal fix + tests

