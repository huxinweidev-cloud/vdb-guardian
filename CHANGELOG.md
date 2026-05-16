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
