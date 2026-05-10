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
- Typed YAML job configuration loader and validator.
- Vector database connector interface.
- Fingerprint engine interface.
- In-memory artifact store.
- Python boundary candidate selection.
- Python Jaccard distance, boundary flip rate, and weighted fingerprint distance.
- Unit tests for all implemented methods.
- Makefile quality gates.

Planned but not yet implemented:

- Real Milvus connector.
- Real pgvector connector.
- Docker-based integration environment.
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
```

## Configuration examples

Example configuration files live in `configs/`:

- `configs/local.yaml`
- `configs/milvus-to-pgvector.example.yaml`

The typed configuration loader lives in `internal/config` and validates job name, source/target connector type, query bounds, fingerprint weights, artifact store type, and report formats before a job can run.

See `docs/config-spec.md` for the full configuration schema and validation rules.

Example files must never contain real credentials. Use `[REDACTED]` for sensitive values.
