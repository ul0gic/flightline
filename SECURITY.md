# Security Policy

## Reporting a vulnerability

Do not open a public issue for a security problem.

Report it privately via [GitHub's private vulnerability reporting](https://github.com/ul0gic/flightline/security/advisories/new). You will get an acknowledgment within 72 hours and a status update within 14 days.

Fixed vulnerabilities are published as GitHub Security Advisories after a patched release ships.

## Scope

Flightline handles App Store Connect credentials: a `.p8` private key, key IDs, and issuer IDs. Reports in these areas are especially welcome:

- Credential exposure in output, logs, error messages, or state files
- JWT construction or signing weaknesses
- Anything that causes a request to leave `api.appstoreconnect.apple.com` over an insecure channel

## Supported versions

Pre-1.0, only the latest release is supported. Update before reporting if possible.
