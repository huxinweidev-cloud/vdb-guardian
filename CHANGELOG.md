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

### Changed

- N/A

### Deprecated

- N/A

### Removed

- N/A

### Fixed

- N/A

### Security

- N/A

## [0.1.0] - Unreleased

Initial release (not yet published).

[Unreleased]: https://github.com/huxinweidev-cloud/vdb-guardian/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/huxinweidev-cloud/vdb-guardian/releases/tag/v0.1.0
