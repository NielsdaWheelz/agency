# Tech Stack

## Scope

This document covers the top-level runtime and tooling stack.

## Stack

- The implementation language is Go.
- The repo targets Go `1.24.x`.
- The CLI surface uses Cobra.
- The terminal workspace UI uses Bubble Tea, Bubbles, and Lip Gloss.
- Process orchestration depends on `git`, `tmux`, and `gh`.
- Persistent runtime state lives in local JSON and JSONL files under `AGENCY_DATA_DIR`.
- Build, lint, test, and e2e flows are driven by `make`, `go test`, `golangci-lint`, and GitHub Actions.
