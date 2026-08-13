# Prometheus Exporters

A collection of Prometheus exporter containers for monitoring network
infrastructure, IPsec tunnels, and Cloudflare services.

## Exporters

| Exporter | Description | Default Port |
| --- | --- | --- |
| `network_exporter` | Network connectivity and performance metrics | 9101 |
| `ipsec_exporter` | IPsec tunnel status and traffic metrics | 9102 |
| `cloudflare_exporter` | Cloudflare zone and DNS analytics metrics | 9103 |

## Quick Start

```bash
# Build all exporter images
make build

# Run linters
make lint

# Run all tests
make test

# Push images to GHCR
make deploy
```

## Container Images

Images are published to GitHub Container Registry (GHCR):

```
ghcr.io/asymmetric-effort/network_exporter
ghcr.io/asymmetric-effort/ipsec_exporter
ghcr.io/asymmetric-effort/cloudflare_exporter
```

### Tagging Policy

| Trigger | Tag |
| --- | --- |
| Push to feature branch / PR | `:<commit-sha>` |
| Push to `main` | `:main` |
| Git tag (e.g. `v1.0.0`) | `:<git-tag>` |

## Development

### Prerequisites

- Go 1.22+
- Docker (or Podman)
- GNU Make

### Project Structure

```
prometheus_exporters/
  cmd/
    network_exporter/
    ipsec_exporter/
    cloudflare_exporter/
  internal/
  site/                    # GitHub Pages website
  .github/
    workflows/
    dependabot.yml
  Makefile
```

### Make Targets

| Target | Description |
| --- | --- |
| `make build` | Build container images |
| `make test` | Run all tests (unit, integration, e2e) |
| `make lint` | Run all linters |
| `make clean` | Remove build artifacts |
| `make deploy` | Push container images to GHCR |
| `make help` | Show available targets |

## Documentation

Visit [prometheus_exporters.asymmetric-effort.com](https://prometheus_exporters.asymmetric-effort.com)
for full documentation.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for guidelines.

## Security

See [SECURITY.md](SECURITY.md) for the security policy and how to report
vulnerabilities.

## License

MIT License - see [LICENSE.txt](LICENSE.txt) for details.
