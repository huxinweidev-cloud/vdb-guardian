# Development Workflow

## Rules

Read `CLAUDE.md` before changing code. Development must follow TDD and every public method must have documentation and tests.

## Quality gates

Run these commands before committing:

```bash
make fmt
make lint
make test
git diff --check
```

## Dependency management

Python dependencies are managed with `uv`. Do not use global `pip install` for project dependencies unless you are explicitly repairing the local environment and have documented the reason.

Go dependencies must be added intentionally and kept minimal. YAML configuration loading currently uses `gopkg.in/yaml.v3` because Go has no standard-library YAML parser. PostgreSQL connectivity for the pgvector connector uses `github.com/jackc/pgx/v5`. The minimal Milvus connector currently uses an adapter boundary without pulling the Milvus SDK into the build; add the SDK only when implementing real network calls and integration tests.

## Configuration development

Configuration changes must update:

- `internal/config` structs and validation tests.
- `configs/*.yaml` examples.
- `docs/config-spec.md`.
- README files when user-facing behavior changes.

Run `go test ./internal/config` during TDD and `make test` before committing.

## Engine protocol development

Go/Python engine protocol changes must update:

- `internal/engine` runner tests.
- `python/vdb_fingerprint_engine` schemas and CLI tests.
- `docs/engine-protocol.md`.
- README files when user-facing commands change.

Run `go test ./internal/engine`, `cd python && uv run pytest tests/test_cli.py`, and `make test` before committing.

## Job runner development

Local job runner changes must update:

- `internal/jobs` runner tests.
- result artifact documentation when output fields change.
- `docs/local-verification-runner.md`.
- README files when user-facing workflow changes.

Run `go test ./internal/jobs`, `make test`, and `git diff --check` before committing.

## Connector development

Connector changes must update:

- `internal/connectors` interface or implementation tests.
- connector-specific documentation under `docs/`.
- README files when local verification behavior changes.

Run `go test ./internal/connectors`, `make test`, and `git diff --check` before committing.

## Fingerprint artifact builder development

Fingerprint builder changes must update:

- `internal/fingerprints` builder tests.
- `docs/fingerprint-artifact-builder.md`.
- `docs/fingerprint-artifact-format.md` when the artifact contract changes.
- README files when the local workflow changes.

Run `go test ./internal/fingerprints`, `make test`, and `git diff --check` before committing.

## Pipeline development

Pipeline changes must update:

- `internal/pipeline` orchestration tests.
- connector, fingerprint, or runner docs when their workflow contract changes.
- `docs/local-offline-pipeline.md`.
- README files when local verification behavior changes.

Run `go test ./internal/pipeline`, `make test`, and `git diff --check` before committing.

## CLI development

CLI changes must update:

- `cmd/vdbg` command tests.
- user-facing command documentation under `docs/`.
- README files when command names, arguments, or smoke checks change.

Run `go test ./cmd/vdbg`, the documented smoke command when practical, `make test`, and `git diff --check` before committing.

## Progress reporting

Report progress at phase boundaries:

- Completed work.
- Current work.
- Next action.
- Risks or blockers.
- Test status.
