# [p1][daemonclient][tech-debt] autostart has no timeout or health verification beyond polling

labels: `p1`, `type:tech-debt`, `area:daemonclient`

## summary
autostart has no timeout or health verification beyond polling

## context
- section: Audit: Daemonclient
- source: docs/issues.md
- details:
  - failure modes are silent and can leave zombie daemon processes.

## acceptance criteria
- [ ] define minimal fix + tests

