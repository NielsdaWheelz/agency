# [p2][merge][tech-debt] retries ignore context

labels: `p2`, `type:tech-debt`, `area:merge`

## summary
retries ignore context

## context
- section: Audit: Merge
- source: docs/issues.md
- details:
  - `confirmPRMerged` and `viewPRFullWithRetry` sleep without checking ctx cancellation.

## acceptance criteria
- [ ] define minimal fix + tests

