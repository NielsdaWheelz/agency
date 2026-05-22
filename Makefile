.PHONY: actionlint build check clean completions e2e e2e-gh e2e-local e2e-pr-failure-matrix e2e-gh-happy fmt fmt-check go-mod-verify goreleaser-check govulncheck help install lint mod-tidy-check run shellcheck shfmt shfmt-check test test-race test-v verify vet

-include .env
export

# Default target
all: build

# Run the fast local gate
check: fmt-check shfmt-check lint vet actionlint shellcheck test build
	@echo "all checks passed"

# Run the full gate
verify: fmt-check shfmt-check lint vet actionlint shellcheck govulncheck mod-tidy-check go-mod-verify test-race goreleaser-check e2e completions build
	@rm -f agency
	@rm -rf completions
	@echo "all verify checks passed"

# Build the binary
build:
	go build -o agency ./cmd/agency

# Run tests
test:
	go test -count=1 -p 2 -parallel 4 ./...

# Run tests with verbose output
test-v:
	go test -v -count=1 -p 2 -parallel 4 ./...

# Run go vet
vet:
	go vet ./...

# Run tests with race detector (platforms that support it)
test-race:
	go test -race -count=1 -p 1 -parallel 2 ./...

# Run golangci-lint against all packages
lint:
	golangci-lint run ./...

# Run the Go vulnerability scanner
govulncheck:
	govulncheck ./...

# Format all Go files
fmt:
	@files="$$(gofmt -l .)"; \
	if [ -n "$$files" ]; then \
		gofmt -w $$files; \
	else \
		echo "gofmt: no changes"; \
	fi

# Check formatting without modifying files
fmt-check:
	@[ -z "$$(gofmt -l .)" ] || (echo "gofmt needed:" && gofmt -l . && exit 1)

# Format shell scripts
shfmt:
	shfmt -w -i 2 -ci scripts/*.sh

# Check shell formatting without modifying files
shfmt-check:
	shfmt -d -i 2 -ci scripts/*.sh

# Run shellcheck against shell scripts
shellcheck:
	shellcheck scripts/*.sh

# Run actionlint against GitHub Actions workflows
actionlint:
	actionlint

# Check go.mod/go.sum are tidy
mod-tidy-check:
	go mod tidy -diff

# Verify downloaded modules match go.sum
go-mod-verify:
	go mod verify

# Run e2e checks. Always runs worktree PR failure matrix.
# GH happy path is opt-in via AGENCY_GH_E2E=1 and requires token.
e2e:
	@echo "running worktree PR failure-matrix e2e"; \
	$(MAKE) e2e-pr-failure-matrix && \
	if [ "$${AGENCY_GH_E2E:-}" = "1" ]; then \
		token="$${GH_TOKEN:-$${GITHUB_TOKEN:-}}"; \
		if [ -z "$$token" ]; then \
			echo "error: AGENCY_GH_E2E=1 requires GH_TOKEN or GITHUB_TOKEN"; \
			exit 1; \
		fi; \
		echo "running github-backed worktree PR happy-path e2e"; \
		$(MAKE) e2e-gh-happy; \
	else \
		echo "AGENCY_GH_E2E not set; running local e2e smoke tests (set AGENCY_GH_E2E=1 for github-backed happy path)"; \
		$(MAKE) e2e-local; \
	fi

# Run both worktree PR e2e suites; happy path requires token.
e2e-gh:
	@token="$${GH_TOKEN:-$${GITHUB_TOKEN:-}}"; \
	if [ -z "$$token" ]; then \
		echo "error: set GH_TOKEN or GITHUB_TOKEN for github-backed e2e"; \
		exit 1; \
	fi; \
	$(MAKE) e2e-pr-failure-matrix; \
	$(MAKE) e2e-gh-happy

# Run worktree PR failure-matrix e2e suite (no GH token required)
e2e-pr-failure-matrix:
	go test -tags=e2e -count=1 -p 1 -parallel 1 ./internal/commands -run TestWorktreePRSyncMergeFailureMatrixE2E

# Run GH-backed worktree PR happy-path e2e suite (requires token)
e2e-gh-happy:
	@token="$${GH_TOKEN:-$${GITHUB_TOKEN:-}}"; \
	if [ -z "$$token" ]; then \
		echo "error: set GH_TOKEN or GITHUB_TOKEN for github-backed e2e happy path"; \
		exit 1; \
	fi; \
	AGENCY_GH_E2E=1 AGENCY_GH_REPO=NielsdaWheelz/agency-test GH_TOKEN="$$token" \
		go test -tags=e2e -count=1 -p 1 -parallel 1 ./internal/commands -run TestGHE2EWorktreePRSyncMerge

# Run local black-box CLI e2e smoke tests
e2e-local:
	AGENCY_LOCAL_E2E=1 go test -tags=e2e -count=1 -p 1 -parallel 1 ./internal/commands -run TestAgentStartCLIE2E

# Validate GoReleaser configuration
goreleaser-check:
	goreleaser check

# Clean build artifacts
clean:
	rm -f agency
	go clean

# Install to GOBIN
install:
	go install ./cmd/agency

# Run from source
run:
	go run ./cmd/agency

# Generate shell completion scripts
completions:
	@mkdir -p completions
	go run ./cmd/agency completion --output completions/agency.bash bash
	go run ./cmd/agency completion --output completions/_agency zsh
	@test -s completions/agency.bash || (echo "error: completions/agency.bash is empty" && exit 1)
	@test -s completions/_agency || (echo "error: completions/_agency is empty" && exit 1)
	@echo "completions generated: completions/agency.bash completions/_agency"

# Show help
help:
	@echo "available targets:"
	@echo "  actionlint     - lint GitHub Actions workflows"
	@echo "  build          - build the agency binary"
	@echo "  check          - run the fast resource-budgeted gate (fmt-check, shfmt-check, lint, vet, actionlint, shellcheck, test, build)"
	@echo "  completions    - generate shell completion scripts"
	@echo "  fmt            - gofmt all Go files"
	@echo "  fmt-check      - check formatting without modifying files"
	@echo "  go-mod-verify  - verify downloaded modules match go.sum"
	@echo "  goreleaser-check - validate .goreleaser.yaml"
	@echo "  govulncheck    - run the Go vulnerability scanner"
	@echo "  lint           - run golangci-lint ./..."
	@echo "  mod-tidy-check - check go.mod/go.sum are tidy"
	@echo "  test           - run resource-budgeted tests"
	@echo "  test-v         - run resource-budgeted tests with verbose output"
	@echo "  test-race      - run resource-budgeted tests with race detector"
	@echo "  shellcheck     - lint shell scripts"
	@echo "  shfmt          - format shell scripts"
	@echo "  shfmt-check    - check shell formatting without modifying files"
	@echo "  verify         - run the full resource-budgeted gate (fmt-check, shfmt-check, lint, vet, actionlint, shellcheck, govulncheck, mod tidy, mod verify, race, goreleaser, e2e, completions, build)"
	@echo "  vet            - run go vet"
	@echo "  e2e            - run e2e (failure-matrix + local smoke by default; set AGENCY_GH_E2E=1 for GH happy path)"
	@echo "  e2e-gh         - run both worktree PR e2e suites (requires GH_TOKEN/GITHUB_TOKEN)"
	@echo "  e2e-gh-happy   - run GH-backed worktree PR happy-path e2e (requires GH_TOKEN/GITHUB_TOKEN)"
	@echo "  e2e-pr-failure-matrix - run worktree PR failure-matrix e2e (no GH token)"
	@echo "  e2e-local      - run local black-box CLI e2e smoke tests"
	@echo "  clean          - clean build artifacts"
	@echo "  install        - install to GOBIN"
	@echo "  run            - run from source"
	@echo "  help           - show this help"
