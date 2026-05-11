# Security Policy

## Supported Versions

We release patches for security vulnerabilities in the following versions:

| Version | Supported          |
| ------- | ------------------ |
| 0.1.x   | :white_check_mark: |

## Reporting a Vulnerability

We take the security of vdb-guardian seriously. If you believe you have found a security vulnerability, please report it to us as described below.

**Please do not report security vulnerabilities through public GitHub issues.**

### How to Report

Please report security vulnerabilities by emailing the project maintainers. Include the following information:

- Type of issue (e.g., buffer overflow, SQL injection, cross-site scripting, etc.)
- Full paths of source file(s) related to the manifestation of the issue
- The location of the affected source code (tag/branch/commit or direct URL)
- Any special configuration required to reproduce the issue
- Step-by-step instructions to reproduce the issue
- Proof-of-concept or exploit code (if possible)
- Impact of the issue, including how an attacker might exploit it

### Response Timeline

- **Acknowledgment**: We will acknowledge receipt of your vulnerability report within 48 hours.
- **Initial Assessment**: We will provide an initial assessment of the report within 5 business days.
- **Resolution**: We aim to resolve critical vulnerabilities within 30 days of acknowledgment.

### Disclosure Policy

- We will coordinate with you to determine an appropriate disclosure timeline.
- We will credit you in the security advisory unless you prefer to remain anonymous.
- We follow responsible disclosure practices and request that you do not publicly disclose the vulnerability until we have released a fix.

## Security Best Practices

When deploying vdb-guardian:

1. **Credentials**: Never commit credentials, API keys, or connection strings to version control.
2. **Network**: Use TLS/SSL for all database connections in production.
3. **Access Control**: Restrict access to vector databases using appropriate authentication and authorization.
4. **Updates**: Keep dependencies up to date using Dependabot or similar tools.
5. **Scanning**: Regularly scan Docker images with Trivy or similar security scanners.
6. **Least Privilege**: Run containers as non-root users.

## Known Security Considerations

### Vector Database Connections

- Milvus and pgvector connections should use encrypted channels in production.
- Connection strings in configuration files should use environment variable substitution.
- Example configurations use placeholder credentials (`[REDACTED]`) and should never be used in production.

### Python Subprocess Execution

- The fingerprint engine runs Python as a subprocess with JSON I/O.
- Input validation is performed on both Go and Python sides.
- Subprocess execution is sandboxed and does not execute arbitrary user code.

### Artifact Storage

- Local artifact storage should have appropriate file permissions.
- S3/MinIO artifact storage should use IAM roles or service accounts, not hardcoded credentials.

## Security Updates

Security updates will be announced through:

- GitHub Security Advisories
- Release notes in CHANGELOG.md
- Git tags with security fix annotations

## Contact

For security-related questions that are not vulnerabilities, please open a GitHub Discussion or contact the maintainers.
