.PHONY: fmt fmt-go fmt-python fmt-check fmt-check-go fmt-check-python \
	lint lint-go lint-python lint-golangci \
	test test-go test-python \
	coverage coverage-go coverage-python \
	tidy pre-commit ci \
	migration-stack-config migration-stack-up migration-stack-down migration-stack-status migration-stack-check

# Format Go and Python source. Writes changes in place.
fmt: fmt-go fmt-python

fmt-go:
	gofmt -w ./cmd ./internal

fmt-python:
	cd python && uv run ruff format .

# Check formatting without writing. Mirrors CI go-format / python-format jobs.
# On Windows with autocrlf=true, the working copy may contain CRLF line endings
# and fmt-check-go will report files as unformatted. Use `git config core.autocrlf input`
# or commit through CI to avoid spurious local failures.
fmt-check: fmt-check-go fmt-check-python

fmt-check-go:
	@unformatted=$$(gofmt -l ./cmd ./internal); \
	if [ -n "$$unformatted" ]; then \
		echo "The following files are not gofmt-formatted:"; \
		echo "$$unformatted"; \
		exit 1; \
	fi

fmt-check-python:
	cd python && uv run ruff format --check .

# Lint Go (vet + golangci-lint) and Python (ruff check). Mirrors CI go-lint / python-lint jobs.
lint: lint-go lint-python lint-golangci

lint-go:
	go vet ./...

lint-python:
	cd python && uv run ruff check .

lint-golangci:
	@command -v golangci-lint >/dev/null 2>&1 || { \
		echo "golangci-lint not installed; see https://golangci-lint.run/welcome/install/"; \
		exit 1; \
	}
	golangci-lint run --timeout=5m

# Run unit tests.
test: test-go test-python

test-go:
	go test ./...

test-python:
	cd python && uv run pytest

# Coverage reports. Mirrors CI Codecov uploads.
coverage: coverage-go coverage-python

coverage-go:
	go test -race -coverprofile=coverage.txt -covermode=atomic ./...
	go tool cover -func=coverage.txt | tail -1

coverage-python:
	cd python && uv run pytest --cov=vdb_fingerprint_engine --cov-report=term-missing

# Tidy Go modules and verify go.sum integrity.
tidy:
	go mod tidy
	go mod verify

# Run all pre-commit hooks across the repository.
pre-commit:
	@command -v pre-commit >/dev/null 2>&1 || { \
		echo "pre-commit not installed; install: pip install pre-commit && pre-commit install"; \
		exit 1; \
	}
	pre-commit run --all-files

# Aggregate target that mirrors all PR-blocking CI checks.
ci: fmt-check lint test coverage

# Local migration Docker Compose stack helpers.
migration-stack-config:
	scripts/check-migration-stack.sh config

migration-stack-up:
	docker compose -f deployments/docker-compose.migration.yml up -d

migration-stack-down:
	docker compose -f deployments/docker-compose.migration.yml down

migration-stack-status:
	scripts/check-migration-stack.sh status

migration-stack-check:
	scripts/check-migration-stack.sh postgres
	scripts/check-migration-stack.sh milvus-port
