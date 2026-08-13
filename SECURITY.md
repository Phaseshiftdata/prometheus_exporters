# Security Policy

## Supported Versions

| Version | Supported          |
| ------- | ------------------ |
| latest  | :white_check_mark: |

## Reporting a Vulnerability

If you discover a security vulnerability in this project, please report it
responsibly.

**Do not open a public GitHub issue for security vulnerabilities.**

Instead, please send an email to <security@asymmetric-effort.com> with the
following information:

- Description of the vulnerability
- Steps to reproduce the issue
- Potential impact
- Suggested fix (if any)

### What to Expect

- **Acknowledgment**: We will acknowledge receipt of your report within 48 hours
- **Assessment**: We will assess the vulnerability and determine its severity
  within 5 business days
- **Resolution**: We will work to resolve critical vulnerabilities as quickly as
  possible and will keep you informed of our progress
- **Disclosure**: We will coordinate with you on public disclosure timing

### Scope

The following are in scope for security reports:

- Container image vulnerabilities
- Authentication or authorization bypasses
- Credential or secret exposure
- Denial of service vulnerabilities in exporter endpoints
- Supply chain vulnerabilities in dependencies

### Out of Scope

- Issues in third-party dependencies that are already publicly known
- Vulnerabilities requiring physical access
- Social engineering attacks

## Security Best Practices

When deploying these exporters:

- Run containers as a non-root user
- Use read-only root filesystems where possible
- Limit network exposure to the metrics endpoint
- Use TLS for metrics endpoints in production
- Regularly update to the latest version
- Scan container images with your organization's security tooling
