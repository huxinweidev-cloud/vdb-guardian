# Contributing to vdb-guardian

Thank you for your interest in contributing to vdb-guardian! This document provides guidelines and instructions for contributing to the project.

## Table of Contents

- [Code of Conduct](#code-of-conduct)
- [Getting Started](#getting-started)
- [Development Environment](#development-environment)
- [Development Workflow](#development-workflow)
- [Coding Standards](#coding-standards)
- [Testing Requirements](#testing-requirements)
- [Commit Message Guidelines](#commit-message-guidelines)
- [Pull Request Process](#pull-request-process)
- [Code Review Standards](#code-review-standards)

## Code of Conduct

This project adheres to the Contributor Covenant Code of Conduct. By participating, you are expected to uphold this code. Please report unacceptable behavior to the project maintainers.

## Getting Started

1. Fork the repository on GitHub
2. Clone your fork locally
3. Set up the development environment (see below)
4. Create a feature branch from `main`
5. Make your changes
6. Run tests and linters
7. Submit a pull request

## Development Environment

### Prerequisites

- **Go**: 1.26.1 or later
- **Python**: 3.11 or later
- **uv**: Latest version (Python package manager)
- **Docker**: For running local test databases
- **Make**: For running build commands
- **Git**: For version control

### Setup

```bash
# Clone your fork
git clone https://github.com/YOUR_USERNAME/vdb-guardian.git
cd vdb-guardian

# Install Go dependencies
go mod download

# Install Python dependencies
cd python
uv sync
cd ..

# Install pre-commit hooks (optional but recommended)
pip install pre-commit
pre-commit install

# Start local test environment (optional)
make migration-stack-up
make migration-stack-check
```

## Development Workflow

### Branch Naming

Use descriptive branch names with the following prefixes:

- `feat/` - New features
- `fix/` - Bug fixes
- `docs/` - Documentation changes
- `test/` - Test additions or modifications
- `chore/` - Build process or tooling changes
- `refactor/` - Code refactoring

Example: `feat/add-qdrant-connector`

### Before Committing

Always run the following commands before committing:

```bash
# Format code
make fmt

# Run linters
make lint

# Run all tests
make test
```

All commands must pass before submitting a pull request.

## Coding Standards

### Go Code Standards

1. **Documentation**: All exported types, functions, methods, constants, and variables must have Go doc comments.
2. **Error Handling**: Never ignore errors. Use `fmt.Errorf` with `%w` for error wrapping.
3. **Naming**: Use clear, descriptive names. Follow Go naming conventions.
4. **Testing**: Every new function must have corresponding unit tests.
5. **Formatting**: Use `gofmt` (enforced by CI).

Example:

```go
// SearchVectors performs a vector similarity search against the target database.
//
// It returns the top K most similar vectors based on the query vector and search
// parameters. The method respects the context timeout and returns an error if
// the search fails or times out.
func (c *Connector) SearchVectors(ctx context.Context, query []float32, topK int) ([]Result, error) {
    if topK <= 0 {
        return nil, fmt.Errorf("topK must be positive, got %d", topK)
    }
    // Implementation...
}
```

### Python Code Standards

1. **Documentation**: All public classes, functions, and methods must have docstrings.
2. **Type Hints**: Use type hints for all function signatures.
3. **Formatting**: Use `ruff format` (enforced by CI).
4. **Linting**: Pass `ruff check` (enforced by CI).
5. **Testing**: Every new function must have corresponding unit tests.

Example:

```python
def jaccard_distance(left: set[str], right: set[str]) -> float:
    """Compute the Jaccard distance between two identifier sets.

    The distance is used by the fingerprint engine to quantify how much two
    neighbor or boundary candidate sets differ. A distance of 0.0 means the sets
    are equivalent, while a distance of 1.0 means they have no overlap.

    Args:
        left: The first set of vector identifiers.
        right: The second set of vector identifiers.

    Returns:
        A normalized distance in the inclusive range [0.0, 1.0].
    """
    # Implementation...
```

## Testing Requirements

### Test-Driven Development (TDD)

This project follows strict TDD:

1. Write a failing test first
2. Run the test to confirm it fails
3. Write minimal code to make it pass
4. Run the test to confirm it passes
5. Refactor if necessary
6. Run all tests to ensure nothing broke

### Test Coverage

- New code must have **≥80% test coverage**
- Core modules (jobs, connectors, engine) must have **≥90% coverage**
- PRs must include coverage reports

### Running Tests

```bash
# Run all tests
make test

# Run Go tests only
make test-go

# Run Python tests only
make test-python

# Run Go tests with coverage
go test -coverprofile=coverage.txt ./...
go tool cover -html=coverage.txt

# Run Python tests with coverage
cd python
uv run pytest --cov=vdb_fingerprint_engine --cov-report=html
```

## Commit Message Guidelines

We follow [Conventional Commits](https://www.conventionalcommits.org/):

```
type(scope): short summary

- Detail 1
- Detail 2
- Detail 3
```

### Types

- `feat`: New feature
- `fix`: Bug fix
- `docs`: Documentation changes
- `test`: Test additions or modifications
- `chore`: Build process or tooling changes
- `refactor`: Code refactoring
- `perf`: Performance improvements

### Examples

```
feat(connectors): add Qdrant connector implementation

- Implement Connector interface for Qdrant
- Add connection pooling and health checks
- Add unit tests with 95% coverage
- Update connector documentation
```

```
fix(engine): handle empty result sets in fingerprint calculation

- Add nil check before computing boundary candidates
- Add test case for empty search results
- Fixes issue where empty results caused panic
```

## Pull Request Process

1. **Update Documentation**: Ensure README, docs/, and code comments are updated.
2. **Run Tests**: All tests must pass (`make test`).
3. **Run Linters**: All linters must pass (`make lint`).
4. **Update CHANGELOG**: Add entry to CHANGELOG.md under "Unreleased".
5. **Write Clear PR Description**: Explain what changed and why.
6. **Link Issues**: Reference related issues using `Fixes #123` or `Relates to #456`.
7. **Request Review**: Tag appropriate reviewers.

### PR Title Format

Use the same format as commit messages:

```
feat(connectors): add Qdrant connector
```

### PR Description Template

```markdown
## Summary

Brief description of what this PR does.

## Changes

- Change 1
- Change 2
- Change 3

## Testing

- [ ] Unit tests added/updated
- [ ] Integration tests added/updated (if applicable)
- [ ] Manual testing performed
- [ ] All tests pass locally

## Documentation

- [ ] Code comments updated
- [ ] README updated (if applicable)
- [ ] docs/ updated (if applicable)
- [ ] CHANGELOG.md updated

## Checklist

- [ ] Code follows project style guidelines
- [ ] All tests pass
- [ ] All linters pass
- [ ] No sensitive information committed
```

## Code Review Standards

### For Authors

- Respond to review comments promptly
- Be open to feedback and suggestions
- Make requested changes or explain why you disagree
- Keep PRs focused and reasonably sized

### For Reviewers

- Be respectful and constructive
- Focus on code quality, not personal preferences
- Explain the reasoning behind suggestions
- Approve when the code meets standards, even if you would have done it differently

### Review Checklist

- [ ] Code follows project conventions
- [ ] Tests are comprehensive and pass
- [ ] Documentation is clear and complete
- [ ] No security vulnerabilities introduced
- [ ] No performance regressions
- [ ] Error handling is appropriate
- [ ] Edge cases are handled

## Additional Resources

- [CLAUDE.md](CLAUDE.md) - Detailed development guidelines (Chinese)
- [Architecture Documentation](docs/architecture.md)
- [Connector Specification](docs/connector-spec.md)
- [Engine Protocol](docs/engine-protocol.md)

## Questions?

If you have questions about contributing, please:

1. Check existing documentation
2. Search closed issues and PRs
3. Open a GitHub Discussion
4. Contact the maintainers

Thank you for contributing to vdb-guardian!
