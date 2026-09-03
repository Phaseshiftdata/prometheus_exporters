# Prometheus Exporters

[![codecov](https://codecov.io/gh/phaseshiftdata/prometheus_exporters/graph/badge.svg)](https://codecov.io/gh/phaseshiftdata/prometheus_exporters)

A collection of Prometheus exporter containers for monitoring network
infrastructure, IPsec tunnels, Cloudflare services, and more.

## Exporters

- **cloudflare_exporter** - Cloudflare analytics, Zero Trust, DNS, and certificate metrics
- **github_exporter** - GitHub CI/CD, PR, commit, and release statistics
- **network_exporter** - Network connectivity and performance metrics
- **ipsec_exporter** - IPsec tunnel status and traffic metrics
- **libvirt_exporter** - Libvirt hypervisor and virtual machine metrics
- **relay_exporter** - Prometheus metrics relay proxy for private and loopback targets
- **openbao_exporter** - OpenBao/Vault cluster health, seal status, and native metrics

## Documentation

- [cloudflare_exporter](docs/cloudflare_exporter.md)
- [github_exporter](docs/github_exporter.md)
- [network_exporter](docs/network_exporter.md)
- [ipsec_exporter](docs/ipsec_exporter.md)
- [libvirt_exporter](docs/libvirt_exporter.md)
- [relay_exporter](docs/relay_exporter.md)
- [openbao_exporter](docs/openbao_exporter.md)
- [Full documentation site](https://prometheus_exporters.phaseshiftdata.com)

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for guidelines.

## Security

See [SECURITY.md](SECURITY.md) for the security policy and how to report
vulnerabilities.

## License

MIT License - see [LICENSE.txt](LICENSE.txt) for details.
