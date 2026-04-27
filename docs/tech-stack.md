# Tech Stack

## Scope

This document covers the top-level runtime and tooling stack.

## Stack

- The implementation language is Go.
- The repo targets Go `1.26.1`.
- The CLI surface uses Cobra.
- The terminal workspace UI uses Bubble Tea, Bubbles, and Lip Gloss.
- Process orchestration depends on `git`, `tmux`, and `gh`.
- Persistent runtime state lives in local JSON and JSONL files under `AGENCY_DATA_DIR`.
- The core Go tooling is `gofmt`, `go vet`, resource-budgeted `go test`, resource-budgeted `go test -race`, `go mod verify`, `govulncheck`, and `golangci-lint v2.11.4`.
- Repo and release checks also use `actionlint`, `shfmt`, `shellcheck`, and `goreleaser check`.
- Build, lint, test, vet, and e2e flows are driven by `make`, `go test`, `go vet`, `golangci-lint`, and GitHub Actions.
