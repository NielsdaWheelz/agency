#!/usr/bin/env bash
set -euo pipefail

go mod download

if ! command -v golangci-lint >/dev/null 2>&1; then
  echo "golangci-lint not found." >&2
  echo "Install with:" >&2
  echo "  go install github.com/golangci/golangci-lint/cmd/golangci-lint@v1.55.2" >&2
  exit 1
fi
