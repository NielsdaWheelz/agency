.PHONY: build test test-v test-race lint vet fmt fmt-check mod-tidy-check e2e e2e-gh e2e-s5-happy e2e-s5-failure-matrix e2e-local clean install run help check verify completions

-include .env
export

# Default target
all: build

# Run all checks strictly (CI-style)
check: fmt-check lint vet test build
	@echo "all checks passed"

# Run every possible check: fmt, lint, vet, mod tidiness, race tests, e2e, completions, build
verify: fmt-check lint vet mod-tidy-check test-race e2e completions build
	@rm -f agency
	@rm -rf completions
	@echo "all verify checks passed"

# Build the binary
build:
	go build -o agency ./cmd/agency

# Run tests
test:
	go test ./...

# Run tests with verbose output
test-v:
	go test -v ./...

# Run go vet
vet:
	go vet ./...

# Run tests with race detector (platforms that support it)
test-race:
	go test -race -count=1 ./...

# Run golangci-lint against all packages
lint:
	golangci-lint run ./...

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

# Check go.mod/go.sum are tidy
mod-tidy-check:
	go mod tidy
	git diff --exit-code -- go.mod go.sum

# Run e2e checks. Always runs S5 failure matrix.
# GH happy path is opt-in via AGENCY_GH_E2E=1 and requires token.
e2e:
	@echo "running s5 failure-matrix e2e"; \
	$(MAKE) e2e-s5-failure-matrix; \
	if [ "$${AGENCY_GH_E2E:-}" = "1" ]; then \
		token="$${GH_TOKEN:-$${GITHUB_TOKEN:-}}"; \
		if [ -z "$$token" ]; then \
			echo "error: AGENCY_GH_E2E=1 requires GH_TOKEN or GITHUB_TOKEN"; \
			exit 1; \
		fi; \
		echo "running github-backed s5 happy-path e2e"; \
		$(MAKE) e2e-s5-happy; \
	else \
		echo "AGENCY_GH_E2E not set; running local e2e smoke tests (set AGENCY_GH_E2E=1 for github-backed happy path)"; \
		$(MAKE) e2e-local; \
	fi

# Run both S5 e2e suites; happy path requires token.
e2e-gh:
	@token="$${GH_TOKEN:-$${GITHUB_TOKEN:-}}"; \
	if [ -z "$$token" ]; then \
		echo "error: set GH_TOKEN or GITHUB_TOKEN for github-backed e2e"; \
		exit 1; \
	fi; \
	$(MAKE) e2e-s5-failure-matrix; \
	$(MAKE) e2e-s5-happy

# Run S5 failure-matrix e2e suite (no GH token required)
e2e-s5-failure-matrix:
	go test -tags=e2e ./internal/commands -run TestS5E2EWorktreePRSyncMergeFailureMatrix -count=1

# Run GH-backed S5 happy-path e2e suite (requires token)
e2e-s5-happy:
	@token="$${GH_TOKEN:-$${GITHUB_TOKEN:-}}"; \
	if [ -z "$$token" ]; then \
		echo "error: set GH_TOKEN or GITHUB_TOKEN for github-backed e2e happy path"; \
		exit 1; \
	fi; \
	AGENCY_GH_E2E=1 AGENCY_GH_REPO=NielsdaWheelz/agency-test GH_TOKEN="$$token" \
		go test -tags=e2e ./internal/commands -run TestGHE2EWorktreePRSyncMerge -count=1

# Run local black-box CLI e2e smoke tests
e2e-local:
	AGENCY_LOCAL_E2E=1 go test -tags=e2e ./internal/commands -run TestAgentStartCLIE2E -count=1

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
	@echo "  build          - build the agency binary"
	@echo "  verify         - run every check (fmt, lint, vet, mod tidy, race, e2e, completions, build)"
	@echo "  check          - run fast checks (fmt-check, lint, vet, test, build)"
	@echo "  completions    - generate shell completion scripts"
	@echo "  fmt            - gofmt all Go files"
	@echo "  fmt-check      - check formatting without modifying files"
	@echo "  vet            - run go vet"
	@echo "  lint           - run golangci-lint ./..."
	@echo "  mod-tidy-check - check go.mod/go.sum are tidy"
	@echo "  test           - run tests"
	@echo "  test-v         - run tests with verbose output"
	@echo "  test-race      - run tests with race detector"
	@echo "  e2e            - run e2e (failure-matrix + local smoke by default; set AGENCY_GH_E2E=1 for GH happy path)"
	@echo "  e2e-gh         - run both S5 e2e suites (requires GH_TOKEN/GITHUB_TOKEN)"
	@echo "  e2e-s5-happy   - run GH-backed S5 happy-path e2e (requires GH_TOKEN/GITHUB_TOKEN)"
	@echo "  e2e-s5-failure-matrix - run S5 failure-matrix e2e (no GH token)"
	@echo "  e2e-local      - run local black-box CLI e2e smoke tests"
	@echo "  clean          - clean build artifacts"
	@echo "  install        - install to GOBIN"
	@echo "  run            - run from source"
	@echo "  help           - show this help"
