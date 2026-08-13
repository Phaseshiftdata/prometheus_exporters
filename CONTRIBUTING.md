# Contributing to Prometheus Exporters

Thank you for your interest in contributing to this project! This document
provides guidelines and information for contributors.

## Code of Conduct

This project adheres to the [Contributor Covenant Code of Conduct](CODE_OF_CONDUCT.md).
By participating, you are expected to uphold this code. Please report
unacceptable behavior to <security@asymmetric-effort.com>.

## How to Contribute

### Reporting Bugs

- Use the [GitHub Issues](https://github.com/asymmetric-effort/prometheus_exporters/issues) tracker
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
git clone https://github.com/asymmetric-effort/prometheus_exporters.git
cd prometheus_exporters
make lint
make test
make build
```

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
