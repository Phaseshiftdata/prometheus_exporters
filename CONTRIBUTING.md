# Contributing to Prometheus Exporters

Thank you for your interest in contributing to this project! This document
provides guidelines and information for contributors.

## Code of Conduct

This project adheres to the [Contributor Covenant Code of Conduct](CODE_OF_CONDUCT.md).
By participating, you are expected to uphold this code. Please report
unacceptable behavior to <security@phaseshiftdata.com>.

## How to Contribute

### Reporting Bugs

- Use the [GitHub Issues](https://github.com/phaseshiftdata/prometheus_exporters/issues) tracker
- Search existing issues before creating a new one
- Include steps to reproduce, expected behavior, and actual behavior
- Include your environment details (OS, Go version, container runtime)

### Suggesting Features

- Open a GitHub Issue with the "enhancement" label
- Describe the use case and expected behavior
- Explain why the feature would be useful to other users

### Submitting Changes

1. Fork the repository
2. Create a feature branch from `main` (`git checkout -b feature/my-feature`)
3. Make your changes following the coding standards below
4. Write or update tests as needed
5. Run the full test suite: `make test`
6. Run linters: `make lint`
7. Commit your changes with a clear, descriptive message
8. Push to your fork and submit a Pull Request

### Pull Request Guidelines

- Keep PRs focused on a single change
- Update documentation if your change affects user-facing behavior
- Ensure all CI checks pass
- Respond to review feedback promptly
- Squash fixup commits before merge

## Development Setup

### Prerequisites

- Go 1.22+
- Docker or Podman
- GNU Make

### Getting Started

```bash
git clone https://github.com/phaseshiftdata/prometheus_exporters.git
cd prometheus_exporters
make lint
make test
make build
```

## Coverage

This project enforces comprehensive test coverage across all surfaces. Every
tracked file is classified in `.coverage-policy.yml` — CI fails if an
unclassified file appears.

### Coverage classifications

| Category | Meaning | Examples |
|---|---|---|
| `line_coverable` | Measured by instrumentation tools | Go source (`cmd/`, `internal/`, `src/`), TypeScript source (`site/src/`) |
| `obligation_covered` | Verified by assertion inventory | Dockerfiles (hadolint + molecule), Makefile (check-targets), SQL migrations (pgTAP) |
| `exempt` | Not measured, with documented reason | Workflows, test code, docs, config files, static assets |

### Running coverage locally

```bash
# Go unit test coverage (gates on 98%)
make cover

# Merge unit + molecule coverage profiles
make cover-merge

# Site TypeScript coverage (gates on 95%)
cd site && npx vitest run --coverage

# Verify all files are classified in .coverage-policy.yml
make check-coverage-policy
```

### Codecov flags and floors

Coverage is uploaded to Codecov with two flags:

- **`go`** — covers `cmd/`, `internal/`, `src/`. Floor: **98%**.
- **`typescript`** — covers `site/src/`. Floor: **95%**.

Floors are configured in `codecov.yml`. The ratchet only goes up: if coverage
improves past the floor, the new level becomes the effective minimum. Codecov
will fail the PR status check if coverage drops below the floor.

### Adding coverage for new surfaces

**New Go exporter:**
1. Add source under `cmd/<name>/` and `internal/<name>/` or `src/<name>/`.
2. Write unit tests achieving >= 98% coverage.
3. Add a `Dockerfile` in `cmd/<name>/` (it will be covered by hadolint and molecule).
4. These paths are already classified as `line_coverable` or `obligation_covered`.

**New site page:**
1. Add source under `site/src/pages/<name>.ts`.
2. Add a unit test `site/src/pages/<name>.test.ts`.
3. The path is already classified under `site/src/` as `line_coverable`.

**New SQL migration:**
1. Add the migration file under `src/github/db/migrations/`.
2. Add corresponding pgTAP schema shape or trigger behavior tests.
3. The path is already classified as `obligation_covered`.

**New top-level file or directory:**
1. Add an entry in `.coverage-policy.yml` under the appropriate category.
2. Run `make check-coverage-policy` to verify no unclassified files remain.

## Coding Standards

- Follow standard Go conventions and `gofmt` formatting
- Write meaningful commit messages
- Include unit tests for new functionality
- Use US English spelling in all code, comments, and documentation
- Keep functions focused and well-documented
- Handle errors explicitly; do not ignore returned errors

## License

By contributing, you agree that your contributions will be licensed under the
[MIT License](LICENSE.txt).
