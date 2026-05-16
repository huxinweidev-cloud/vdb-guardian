# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- Initial project scaffold with Go + Python monorepo architecture
- Go control plane with CLI and server entrypoints
- Job state model and local verification job runner
- Typed YAML job configuration loader and validator
- Vector database connector interface
- Memory connector for deterministic local verification
- Milvus connector with real Go SDK adapter
- pgvector connector for PostgreSQL with pgvector extension
- Python fingerprint engine with boundary candidate selection and distance metrics
- Local offline verification pipeline
- Docker Compose migration stack (Milvus + PostgreSQL)
- Comprehensive documentation (English + Chinese)
- CI/CD workflow with GitHub Actions
- Pre-commit hooks configuration
- golangci-lint configuration
- Community governance files (SECURITY.md, CONTRIBUTING.md, CODE_OF_CONDUCT.md)
- Dependabot configuration for automated dependency updates
- Machine-readable diagnostic JSON report for `migrate-and-verify` runs
- Read-only `vdbg inspect-milvus` CLI for Milvus metadata inspection and migration planning JSON
- Dry-run `vdbg plan-pgvector-schema` CLI for pgvector schema and DDL planning from inspection plans
- Read-only `vdbg compare-schema-plans` CLI for validating Milvus inspection plans against pgvector schema plans before applying DDL
- Dry-run-by-default `vdbg apply-pgvector-schema` CLI for applying planned pgvector schema DDL with JSON reports
- Read-only `vdbg inspect-pgvector-schema` CLI for inventorying live PostgreSQL/pgvector schema metadata
- Read-only `vdbg compare-applied-schema` CLI for detecting planned-vs-live pgvector schema drift before record migration
- Optional `vdbg migrate --require-schema-match` preflight and `--output` JSON result report for standalone migrations
- Local-artifact `vdbg map-migration-records` CLI for validating full-record mapping plans before execution
- Optional `vdbg migrate --record-mapping` execution for consuming passing mapping artifacts and migrating mapped scalar fields, dynamic metadata, and partition metadata alongside id/vector records
- Local-artifact `vdbg compare-full-records` CLI for full-record equality reports across scalar fields, dynamic metadata, partition metadata, vector hashes, and vector dimensions
- Live read-only `vdbg build-milvus-record-artifact` and `vdbg build-pgvector-record-artifact` CLI builders for producing full-record artifacts from passing mapping plans
- `vdbg migrate-and-verify --full-record-compare` optional orchestration that builds live source/target full-record artifacts, runs full-record equality comparison, and includes the artifacts in Markdown/diagnostic reports
- Artifact-only `vdbg reconcile-target` and explicitly confirmed `vdbg cleanup-target-stale` for auditing and deleting stale pgvector target rows after upsert-style migrations
- pgvector migration write modes (`batch-upsert`, `copy`, and `auto`) with report metrics and an opt-in `make smoke-migration-copy` Docker smoke for COPY-mode validation

### Changed

- N/A

### Deprecated

- N/A

### Removed

- N/A

### Fixed

- Milvus `FLAT` index planning now maps to exact scan/no physical pgvector index instead of emitting unsupported `USING exact_scan` DDL
- Applied schema comparison now treats PostgreSQL `character varying(n)` as equivalent to planned `varchar(n)`

### Security

- N/A

## [0.1.0] - Unreleased

Initial release (not yet published).

[Unreleased]: https://github.com/huxinweidev-cloud/vdb-guardian/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/huxinweidev-cloud/vdb-guardian/releases/tag/v0.1.0
