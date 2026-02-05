# [p1][daemon][tech-debt] stream parser can allocate unbounded memory on huge lines

labels: `p1`, `type:tech-debt`, `area:daemon`

## summary
stream parser can allocate unbounded memory on huge lines

## context
- section: Audit: Daemon
- source: docs/issues.md
- details:
  - `bufio.ReadBytes('\n')` reads the full line into memory before size checks. enforce hard cap with `ReadSlice` or custom reader.

## acceptance criteria
- [ ] define minimal fix + tests

