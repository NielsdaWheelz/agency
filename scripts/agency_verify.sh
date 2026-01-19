#!/usr/bin/env bash
set -euo pipefail

unset GIT_DIR GIT_WORK_TREE GIT_COMMON_DIR GIT_INDEX_FILE GIT_OBJECT_DIRECTORY GIT_ALTERNATE_OBJECT_DIRECTORIES

go test -count=1 ./...

os="$(go env GOOS)"
arch="$(go env GOARCH)"
case "${os}/${arch}" in
  darwin/amd64|darwin/arm64|linux/amd64|linux/arm64|windows/amd64)
    go test -race -count=1 ./...
    ;;
  *)
    echo "skipping -race on ${os}/${arch} (unsupported)"
    ;;
esac

go vet ./...

unformatted="$(gofmt -l .)"
if [ -n "$unformatted" ]; then
  echo "gofmt needed:"
  echo "$unformatted"
  exit 1
fi

go mod tidy
git diff --exit-code -- go.mod go.sum

if ! command -v golangci-lint >/dev/null 2>&1; then
  echo "golangci-lint not found." >&2
  echo "Install with:" >&2
  echo "  go install github.com/golangci/golangci-lint/cmd/golangci-lint@v1.55.2" >&2
  exit 1
fi

golangci-lint run
