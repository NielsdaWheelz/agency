# [p1][daemonclient][bug] ReadRawLog is broken and unbounded

labels: `p1`, `type:bug`, `area:daemonclient`

## summary
ReadRawLog is broken and unbounded

## context
- section: merged
- source: docs/issues.md (merged)
- merged items:
  - ReadRawLog likely broken and silently fails
  - ReadRawLog is broken (already listed) and hides errors
  - ReadRawLog reads unbounded data into memory
- details:
  - Uses `http.DefaultClient.Get("file://...")`, which net/http doesn’t handle; returns empty data on error. Use `os.ReadFile` or `fs.FS` and return errors.
  -
  - `io.ReadAll` on large logs can blow memory. cap or stream.
  -

## acceptance criteria
- [ ] define minimal fix + tests

