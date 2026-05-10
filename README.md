# vdb-guardian

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
- Minimal Milvus connector for migration MVP source-side search.
- Minimal pgvector connector for migration MVP target-side search.
- Local offline verification pipeline.
- Offline verify fixture CLI command.
- Local Milvus and pgvector migration Docker Compose stack.
- pgvector synthetic fixture seeding.
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

- Milvus real SDK adapter, fixture seeding, and integration tests.
- pgvector real database seeding CLI and integration tests.
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

## pgvector fixture seeding

The pgvector fixture seeder lives in `internal/migration`. It creates the pgvector extension/table and upserts deterministic synthetic records through an injected database adapter.

See `docs/pgvector-fixture-seeding.md` for SQL behavior, validation rules, and current limitations.

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
