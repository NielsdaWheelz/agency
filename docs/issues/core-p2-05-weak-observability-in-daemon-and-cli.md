# [p2][core][tech-debt] Weak observability in daemon and CLI

labels: `p2`, `type:tech-debt`, `area:core`

## summary
Weak observability in daemon and CLI

## context
- section: Quality Gaps (Global)
- source: docs/issues.md
- details:
  - Plain `fmt.Fprintf` to stderr with no structured logs, levels, or request correlation. Introduce a logger interface and emit structured events.

## acceptance criteria
- [ ] define minimal fix + tests

