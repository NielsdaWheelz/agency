# [p2][errors][tech-debt] log tailing breaks on long lines

labels: `p2`, `type:tech-debt`, `area:errors`

## summary
log tailing breaks on long lines

## context
- section: Audit: Errors
- source: docs/issues.md
- details:
  - `errors.readTail` uses `bufio.Scanner` with the 64k token limit; long log lines cause `ErrTooLong` and drop output. set `scanner.Buffer` or use `bufio.Reader`.

## acceptance criteria
- [ ] define minimal fix + tests

