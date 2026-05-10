# vdb-guardian Architecture

## Purpose

`vdb-guardian` verifies retrieval behavior consistency after heterogeneous vector database migrations. It is not a generic benchmark harness and not just a data-count checker. Its core domain model is the retrieval behavior fingerprint.

## First-stage architecture

```text
Go CLI/API-ready control plane
        |
        | stable JSON/artifact protocol
        v
Python fingerprint engine
```

Go owns enterprise reliability concerns:

- CLI and future server entrypoints.
- Job lifecycle states.
- Local verification job orchestration.
- Local offline verification pipeline orchestration.
- Typed YAML configuration loading and validation.
- Connector interfaces.
- Memory connector for deterministic local verification.
- Fingerprint artifact building from normalized search results.
- Artifact storage abstraction.
- Engine invocation boundary.
- Python subprocess runner using a stable JSON protocol.
- Future observability and deployment hooks.

Python owns algorithm velocity:

- Boundary candidate selection.
- Artifact-backed source/target fingerprint comparison.
- Stable-neighbor and fingerprint metrics.
- Fingerprint distance.
- Consistency scoring.
- Future statistical analysis and report helpers.

## Core packages

- `internal/jobs`: durable job lifecycle states and local verification runner.
- `internal/pipeline`: local offline verification pipeline orchestration.
- `internal/config`: typed YAML job configuration loading and validation.
- `internal/connectors`: normalized vector database connector contract, memory connector, minimal Milvus connector, and minimal pgvector connector.
- `internal/migration`: write-side migration and fixture seeding orchestration.
- `internal/engine`: Go-to-engine comparison contract and Python subprocess runner.
- `internal/fingerprints`: artifact builder from normalized search results.
- `internal/artifacts`: storage abstraction for fingerprints, reports, and intermediate files.
- `python/vdb_fingerprint_engine`: retrieval behavior fingerprint algorithms.

## Evolution path

1. Modular monorepo with Go CLI and Python engine.
2. Add Milvus and pgvector connectors.
3. Add local Docker integration tests.
4. Add API routes and persistent job storage.
5. Evolve Python subprocess engine to remote Python service if scaling requires it.
