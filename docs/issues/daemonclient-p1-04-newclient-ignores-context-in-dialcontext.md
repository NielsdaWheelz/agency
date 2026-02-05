# [p1][daemonclient][tech-debt] NewClient ignores context in DialContext

labels: `p1`, `type:tech-debt`, `area:daemonclient`

## summary
NewClient ignores context in DialContext

## context
- section: Audit: Daemonclient
- source: docs/issues.md
- details:
  - it calls `net.Dial` directly; cancellations and deadlines don’t apply. use `net.Dialer{}.DialContext`.

## acceptance criteria
- [ ] define minimal fix + tests

