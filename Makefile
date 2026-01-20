.PHONY: build test test-v lint fmt fmt-check e2e clean install run help check

-include .env
export

# Default target
all: build

# Run all checks strictly (CI-style)
check: fmt fmt-check lint test e2e build
	@echo "all checks passed"

# Build the binary
build:
	go build -o agency ./cmd/agency

# Run tests
test:
	go test ./...

# Run tests with verbose output
test-v:
	go test -v ./...

# Run golangci-lint (requires golangci-lint on PATH)
lint:
	golangci-lint run

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

# Run GH e2e test (requires env vars)
e2e:
	AGENCY_GH_E2E=1 AGENCY_GH_REPO=NielsdaWheelz/agency-test GH_TOKEN=$${GH_TOKEN:?} \
		go test ./... -run TestGHE2EPushMerge -count=1

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

# Show help
help:
	@echo "available targets:"
	@echo "  build    - build the agency binary"
	@echo "  check    - run all checks (fmt-check, lint, test, e2e, build)"
	@echo "  fmt      - gofmt all Go files"
	@echo "  fmt-check- check formatting without modifying files"
	@echo "  lint     - run golangci-lint"
	@echo "  test     - run tests"
	@echo "  test-v   - run tests with verbose output"
	@echo "  e2e      - run GH e2e test (requires GH_TOKEN)"
	@echo "  clean    - clean build artifacts"
	@echo "  install  - install to GOBIN"
	@echo "  run      - run from source"
	@echo "  help     - show this help"
