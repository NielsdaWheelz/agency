# [p1][build][tech-debt] CI only runs go test ./...

labels: `p1`, `type:tech-debt`, `area:build`

## summary
CI only runs go test ./...

## context
- section: Audit: CI / Build
- source: docs/issues.md
- details:
  - no `golangci-lint`, `go vet`, `fmt-check`, `mod-tidy-check`, or `-race`. the repo has `make check/verify` but CI ignores it. enforce these gates in CI.

## acceptance criteria
- [ ] define minimal fix + tests

