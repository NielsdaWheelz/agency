# [p1][core][tech-debt] Daemon HTTP server has no timeouts or size limits

labels: `p1`, `type:tech-debt`, `area:core`

## summary
Daemon HTTP server has no timeouts or size limits

## context
- section: Quality Gaps (Global)
- source: docs/issues.md
- details:
  - `http.Server{Handler: mux}` uses zero timeouts; request bodies are decoded without size caps or `DisallowUnknownFields`. This is a DoS and correctness risk.

## acceptance criteria
- [ ] define minimal fix + tests

