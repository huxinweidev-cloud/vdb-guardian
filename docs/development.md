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

Go dependencies must be added intentionally and kept minimal. YAML configuration loading currently uses `gopkg.in/yaml.v3` because Go has no standard-library YAML parser.

## Configuration development

Configuration changes must update:

- `internal/config` structs and validation tests.
- `configs/*.yaml` examples.
- `docs/config-spec.md`.
- README files when user-facing behavior changes.

Run `go test ./internal/config` during TDD and `make test` before committing.

## Progress reporting

Report progress at phase boundaries:

- Completed work.
- Current work.
- Next action.
- Risks or blockers.
- Test status.
