# vdb-guardian

[![CI](https://github.com/h3xwave/vdb-guardian/workflows/CI/badge.svg)](https://github.com/h3xwave/vdb-guardian/actions)
[![codecov](https://codecov.io/gh/h3xwave/vdb-guardian/branch/main/graph/badge.svg)](https://codecov.io/gh/h3xwave/vdb-guardian)
[![Go Report Card](https://goreportcard.com/badge/github.com/h3xwave/vdb-guardian)](https://goreportcard.com/report/github.com/h3xwave/vdb-guardian)
[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](LICENSE)

中文文档: [README.zh-CN.md](README.zh-CN.md)

`vdb-guardian` is an enterprise-oriented retrieval behavior consistency verifier for heterogeneous vector database migrations.

The project validates whether a target vector database preserves the source database's retrieval behavior after migration. It focuses on retrieval behavior fingerprints, boundary candidates, fingerprint distance, consistency scoring, and explainable difference reports.

## Architecture

The first-stage architecture is a Go + Python monorepo:

- Go control plane: CLI, server entrypoint, job lifecycle, connector contracts, engine contracts, artifact storage contracts.
- Python fingerprint engine: boundary candidate selection, distance metrics, and future retrieval behavior fingerprint algorithms.

The initial deployment shape is modular monorepo + Python subprocess-ready engine. It can later evolve into Go API, Go workers, and a Python gRPC/FastAPI fingerprint service.

## Current scope

Implemented in this scaffold:

- Go module and command entrypoints.
- Job state model.
- Local verification job runner.
- Typed YAML job configuration loader and validator.
- Vector database connector interface.
- Memory connector for deterministic local verification.
- Minimal Milvus connector with real Go SDK adapter for migration MVP source-side search.
- Minimal pgvector connector for migration MVP target-side search.
- Local offline verification pipeline.
- Offline verify fixture CLI command.
- Local Milvus and pgvector migration Docker Compose stack.
- Milvus synthetic fixture seeding.
- `vdbg seed-milvus` real Milvus fixture seeding CLI.
- `vdbg search-milvus` real Milvus search smoke CLI.
- `vdbg build-milvus-artifact` real Milvus fingerprint artifact CLI.
- pgvector synthetic fixture seeding.
- `vdbg seed-pgvector` real pgvector fixture seeding CLI.
- `vdbg search-pgvector` real pgvector search smoke CLI.
- `vdbg build-pgvector-artifact` real pgvector fingerprint artifact CLI.
- `vdbg compare-artifacts` real source/target fingerprint artifact comparison CLI.
- Synthetic vector dataset generator.
- Fingerprint artifact builder.
- Fingerprint engine interface.
- Python subprocess engine runner.
- In-memory artifact store.
- Python boundary candidate selection.
- Python compare CLI using the Go/Python JSON engine protocol.
- Artifact-backed fingerprint comparison.
- Python Jaccard distance, boundary flip rate, and weighted fingerprint distance.
- Unit tests for all implemented methods.
- Makefile quality gates.

Planned but not yet implemented:

- Milvus seed CLI integration tests against the local migration stack.
- pgvector seed CLI integration tests against the local migration stack.
- Real migration and verification CLI command.
- API routes.
- Persistent job storage.
- Full report generation.

## Development requirements

Read `CLAUDE.md` before development. The project requires:

- TDD: tests before production code.
- Public Go APIs with Go doc comments.
- Public Python APIs with docstrings.
- Unit tests for each method or function.
- Formatting, linting, and tests before commit.
- No secrets, real tokens, or production connection strings in the repository.

## Local commands

```bash
make fmt
make lint
make test
make migration-stack-config
```

Go only:

```bash
make test-go
make lint-go
```

Python only:

```bash
cd python
uv sync
uv run pytest
uv run ruff format .
uv run ruff check .
```

## CLI smoke check

```bash
go run ./cmd/vdbg --version
go run ./cmd/vdb-guardian-server
go run ./cmd/vdbg offline-verify --fixture testdata/offline/basic.json --artifact-dir /tmp/vdb-guardian-offline
go run ./cmd/vdbg generate-synthetic-fixture --output testdata/migration/synthetic-small.json --seed 42 --dimension 8 --records 100 --queries 10 --metric cosine
go run ./cmd/vdbg seed-milvus --fixture testdata/migration/synthetic-small.json --address localhost:19530
go run ./cmd/vdbg search-milvus --fixture testdata/migration/synthetic-small.json --address localhost:19530 --top-k 3 --expand-k 5
go run ./cmd/vdbg build-milvus-artifact --fixture testdata/migration/synthetic-small.json --address localhost:19530 --output /tmp/vdb-guardian-source-fingerprint.json --top-k 3 --expand-k 5 --stable-k 2 --boundary-k 1
go run ./cmd/vdbg seed-pgvector --fixture testdata/migration/synthetic-small.json --connection-url '[REDACTED]'
go run ./cmd/vdbg search-pgvector --fixture testdata/migration/synthetic-small.json --connection-url '[REDACTED]' --top-k 3 --expand-k 5
go run ./cmd/vdbg build-pgvector-artifact --fixture testdata/migration/synthetic-small.json --connection-url '[REDACTED]' --output /tmp/vdb-guardian-target-fingerprint.json --top-k 3 --expand-k 5 --stable-k 2 --boundary-k 1
go run ./cmd/vdbg compare-artifacts --source /tmp/vdb-guardian-source-fingerprint.json --target /tmp/vdb-guardian-target-fingerprint.json --artifact-dir /tmp/vdb-guardian-compare --job-id real-artifact-smoke
```

## Engine protocol

The Go control plane can invoke the Python fingerprint engine through `internal/engine.PythonRunner`. The current subprocess command is:

```bash
cd python
uv run python -m vdb_fingerprint_engine.cli compare --input /tmp/vdb-engine-input.json --output /tmp/vdb-engine-output.json
```

See `docs/engine-protocol.md` for the JSON input/output contract and `docs/fingerprint-artifact-format.md` for the artifact schema. The current compare command reads source and target fingerprint artifacts and returns artifact-backed consistency metrics.

## Memory connector

The memory connector lives in `internal/connectors`. It returns deterministic precomputed ranked hits through the same `Connector` interface that future Milvus and pgvector connectors will implement.

See `docs/memory-connector.md` for local verification usage and limitations.

## Milvus connector

The minimal Milvus connector lives in `internal/connectors`. It validates Milvus configuration, normalizes source-side search hits, and keeps real Milvus SDK calls behind an adapter boundary for the migration MVP.

See `docs/milvus-connector.md` for current scope, score normalization, safety rules, and MVP limitations.

## pgvector connector

The minimal pgvector connector lives in `internal/connectors`. It validates pgvector configuration, checks that the `vector` extension is installed, counts rows, and executes cosine/L2 vector search through PostgreSQL with `pgx`.

See `docs/pgvector-connector.md` for current scope, query behavior, safety rules, and MVP limitations.

## Fingerprint artifact builder

The Go fingerprint artifact builder lives in `internal/fingerprints`. It converts normalized search results into Python-compatible retrieval behavior fingerprint artifacts by deriving `stable_neighbors`, `boundary_candidates`, and `top_k_ids` from ranked hits.

See `docs/fingerprint-artifact-builder.md` for the builder workflow and validation rules.

## Local verification runner

The Go local verification runner lives in `internal/jobs`. It accepts source and target fingerprint artifact paths, invokes an `engine.Engine`, and writes a structured result artifact:

```text
<artifact-dir>/<job-id>-result.json
```

See `docs/local-verification-runner.md` for the current workflow and limitations.

## Local offline pipeline

The local offline pipeline lives in `internal/pipeline`. It connects source and target connectors, fingerprint artifact building, and the verification runner into a database-free end-to-end verification path.

See `docs/local-offline-pipeline.md` for the workflow, generated artifacts, and current limitations.

## Offline verify fixture command

The `vdbg offline-verify` command runs the local offline pipeline from a JSON fixture and writes fingerprint/result artifacts without Docker or real databases.

See `docs/offline-verify-fixture.md` and `testdata/offline/basic.json` for the fixture format and smoke command.

## Local migration stack

The local migration stack defines Milvus standalone and PostgreSQL pgvector services for the upcoming migration-and-verification MVP.

Validate the Compose file without starting containers:

```bash
make migration-stack-config
```

Start the stack only when you are ready to create local Docker containers, networks, and volumes:

```bash
make migration-stack-up
```

See `docs/local-migration-stack.md` for ports, local-only credentials, health checks, and limitations.

## Milvus fixture seeding

The Milvus fixture seeder lives in `internal/migration`. It prepares a minimal collection boundary and inserts deterministic synthetic records through an injected database adapter.

The `vdbg seed-milvus` command wires that seeder to a real Milvus Go SDK connection:

```bash
go run ./cmd/vdbg seed-milvus \
  --fixture testdata/migration/synthetic-small.json \
  --address localhost:19530 \
  --collection items \
  --id-field id \
  --vector-field embedding \
  --metric cosine
```

See `docs/milvus-fixture-seeding.md` for adapter behavior and validation rules. See `docs/seed-milvus-cli.md` for the real database CLI workflow and current limitations.

## Milvus search smoke

The `vdbg search-milvus` command reuses the real Milvus connector to count seeded rows and search one query vector from a synthetic fixture:

```bash
go run ./cmd/vdbg search-milvus \
  --fixture testdata/migration/synthetic-small.json \
  --address localhost:19530 \
  --collection items \
  --id-field id \
  --vector-field embedding \
  --top-k 3 \
  --expand-k 5 \
  --query-index 0 \
  --metric cosine
```

See `docs/search-milvus-cli.md` for the source-side read smoke workflow and limitations.

## Milvus fingerprint artifact

The `vdbg build-milvus-artifact` command searches every fixture query against real Milvus and writes a Python-compatible source fingerprint artifact:

```bash
go run ./cmd/vdbg build-milvus-artifact \
  --fixture testdata/migration/synthetic-small.json \
  --address localhost:19530 \
  --output /tmp/vdb-guardian-source-fingerprint.json \
  --collection items \
  --id-field id \
  --vector-field embedding \
  --top-k 3 \
  --expand-k 5 \
  --stable-k 2 \
  --boundary-k 1 \
  --metric cosine
```

See `docs/build-milvus-artifact-cli.md` for the source artifact workflow and limitations.

## pgvector fixture seeding

The pgvector fixture seeder lives in `internal/migration`. It creates the pgvector extension/table and upserts deterministic synthetic records through an injected database adapter.

The `vdbg seed-pgvector` command wires that seeder to a real pgx-backed PostgreSQL connection:

```bash
go run ./cmd/vdbg seed-pgvector \
  --fixture testdata/migration/synthetic-small.json \
  --connection-url '[REDACTED]' \
  --table items \
  --id-column id \
  --vector-column embedding
```

See `docs/pgvector-fixture-seeding.md` for SQL behavior and validation rules. See `docs/seed-pgvector-cli.md` for the real database CLI workflow and current integration-test limitations.

## pgvector search smoke

The `vdbg search-pgvector` command reuses the real pgvector connector to count seeded rows and search one query vector from a synthetic fixture:

```bash
go run ./cmd/vdbg search-pgvector \
  --fixture testdata/migration/synthetic-small.json \
  --connection-url '[REDACTED]' \
  --table items \
  --top-k 3 \
  --expand-k 5 \
  --query-index 0 \
  --metric cosine
```

See `docs/search-pgvector-cli.md` for the read-only smoke workflow and limitations.

## pgvector fingerprint artifact CLI

The `vdbg build-pgvector-artifact` command searches every fixture query through the real pgvector connector and writes a Python-compatible target fingerprint artifact:

```bash
go run ./cmd/vdbg build-pgvector-artifact \
  --fixture testdata/migration/synthetic-small.json \
  --connection-url '[REDACTED]' \
  --output /tmp/vdb-guardian-target-fingerprint.json \
  --table items \
  --top-k 3 \
  --expand-k 5 \
  --stable-k 2 \
  --boundary-k 1 \
  --metric cosine
```

See `docs/build-pgvector-artifact-cli.md` for the target-side artifact workflow and limitations.

## Source/target artifact comparison

The `vdbg compare-artifacts` command compares existing source and target fingerprint artifacts through the Python engine and writes a normalized result artifact:

```bash
go run ./cmd/vdbg compare-artifacts \
  --source /tmp/vdb-guardian-source-fingerprint.json \
  --target /tmp/vdb-guardian-target-fingerprint.json \
  --artifact-dir /tmp/vdb-guardian-compare \
  --job-id real-artifact-smoke
```

See `docs/compare-artifacts-cli.md` for the comparison workflow and result schema.

## Synthetic vector fixtures

The `vdbg generate-synthetic-fixture` command creates deterministic vector records and query vectors for upcoming Milvus to pgvector migration tests.

```bash
go run ./cmd/vdbg generate-synthetic-fixture \
  --output testdata/migration/synthetic-small.json \
  --seed 42 \
  --dimension 8 \
  --records 100 \
  --queries 10 \
  --metric cosine
```

The first MVP supports dimensions `1..2000`, matching pgvector `vector` compatibility. See `docs/synthetic-vector-fixtures.md` for the JSON format and recommended test stages.

## Configuration examples

Example configuration files live in `configs/`:

- `configs/local.yaml`
- `configs/milvus-to-pgvector.example.yaml`

The typed configuration loader lives in `internal/config` and validates job name, source/target connector type, query bounds, fingerprint weights, artifact store type, and report formats before a job can run.

See `docs/config-spec.md` for the full configuration schema and validation rules.

Example files must never contain real credentials. Use `[REDACTED]` for sensitive values.
