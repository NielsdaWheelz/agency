#!/usr/bin/env bash
set -euo pipefail

if [ -f "$AGENCY_REPO_ROOT/.env" ]; then
  cp "$AGENCY_REPO_ROOT/.env" "$AGENCY_WORKSPACE_ROOT/.env"
fi

go mod download

install_bin="$(go env GOPATH)/bin"
export PATH="$install_bin:$PATH"
mkdir -p "$install_bin"

golangci_lint_version="2.11.4"
govulncheck_version="v1.1.4"
actionlint_version="v1.7.8"
shfmt_version="v3.13.0"
goreleaser_version="v2.14.3"
shellcheck_version="v0.11.0"

installed_golangci_lint_version=""
if command -v golangci-lint >/dev/null 2>&1; then
  installed_golangci_lint_version="$(golangci-lint version | awk '/version/ { print $4; exit }')"
fi

if [ "$installed_golangci_lint_version" != "$golangci_lint_version" ]; then
  echo "Installing golangci-lint v${golangci_lint_version} from the official release path." >&2
  curl -sSfL https://golangci-lint.run/install.sh | sh -s -- -b "$install_bin" "v${golangci_lint_version}"
fi

go install "golang.org/x/vuln/cmd/govulncheck@${govulncheck_version}"
go install "github.com/rhysd/actionlint/cmd/actionlint@${actionlint_version}"
go install "mvdan.cc/sh/v3/cmd/shfmt@${shfmt_version}"
go install "github.com/goreleaser/goreleaser/v2@${goreleaser_version}"

if ! command -v shellcheck >/dev/null 2>&1; then
  shellcheck_os="$(uname -s)"
  shellcheck_arch="$(uname -m)"

  case "${shellcheck_os}-${shellcheck_arch}" in
    Darwin-arm64 | Darwin-aarch64)
      shellcheck_asset="shellcheck-${shellcheck_version}.darwin.aarch64.tar.xz"
      ;;
    Darwin-x86_64 | Darwin-amd64)
      shellcheck_asset="shellcheck-${shellcheck_version}.darwin.x86_64.tar.xz"
      ;;
    Linux-x86_64 | Linux-amd64)
      shellcheck_asset="shellcheck-${shellcheck_version}.linux.x86_64.tar.xz"
      ;;
    Linux-arm64 | Linux-aarch64)
      shellcheck_asset="shellcheck-${shellcheck_version}.linux.aarch64.tar.xz"
      ;;
    *)
      echo "error: unsupported shellcheck platform ${shellcheck_os}-${shellcheck_arch}" >&2
      exit 1
      ;;
  esac

  shellcheck_dir="$(mktemp -d)"
  curl -sSfL "https://github.com/koalaman/shellcheck/releases/download/${shellcheck_version}/${shellcheck_asset}" | tar -xJ -C "$shellcheck_dir"
  install -m 0755 "$shellcheck_dir"/shellcheck-*/shellcheck "${install_bin}/shellcheck"
  rm -rf "$shellcheck_dir"
fi
