#!/usr/bin/env bash
set -euo pipefail

if [ -f "$AGENCY_REPO_ROOT/.env" ]; then
  cp "$AGENCY_REPO_ROOT/.env" "$AGENCY_WORKSPACE_ROOT/.env"
fi

go mod download

install_bin="$(go env GOPATH)/bin"
export PATH="$install_bin:$PATH"
golangci_lint_version="2.11.4"

installed_golangci_lint_version=""
if command -v golangci-lint >/dev/null 2>&1; then
  installed_golangci_lint_version="$(golangci-lint version | awk '/version/ { print $4; exit }')"
fi

if [ "$installed_golangci_lint_version" != "$golangci_lint_version" ]; then
  echo "Installing golangci-lint v${golangci_lint_version} from the official release path." >&2
  curl -sSfL https://golangci-lint.run/install.sh | sh -s -- -b "$install_bin" "v${golangci_lint_version}"
fi
