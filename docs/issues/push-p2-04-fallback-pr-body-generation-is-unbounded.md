# [p2][push][tech-debt] fallback PR body generation is unbounded

labels: `p2`, `type:tech-debt`, `area:push`

## summary
fallback PR body generation is unbounded

## context
- section: Audit: Push
- source: docs/issues.md
- details:
  - `git log`/`diff --name-only` can output massive data; cap with `-n` and `--max-count`.

## acceptance criteria
- [ ] define minimal fix + tests

