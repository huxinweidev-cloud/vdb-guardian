.PHONY: fmt fmt-go fmt-python lint lint-go lint-python test test-go test-python migration-stack-config migration-stack-up migration-stack-down migration-stack-status migration-stack-check

fmt: fmt-go fmt-python

fmt-go:
	gofmt -w ./cmd ./internal

fmt-python:
	cd python && uv run ruff format .

lint: lint-go lint-python

lint-go:
	go vet ./...

lint-python:
	cd python && uv run ruff check .

test: test-go test-python

test-go:
	go test ./...

test-python:
	cd python && uv run pytest

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
