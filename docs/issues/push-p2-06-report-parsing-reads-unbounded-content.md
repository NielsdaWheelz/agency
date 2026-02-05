# [p2][push][tech-debt] report parsing reads unbounded content

labels: `p2`, `type:tech-debt`, `area:push`

## summary
report parsing reads unbounded content

## context
- section: Audit: Push
- source: docs/issues.md
- details:
  - `report.CheckCompleteness` reads entire report; cap size or stream.

## acceptance criteria
- [ ] define minimal fix + tests

