# Security Policy

## Supported Versions

| Version | Supported |
|---|---|
| 0.1.x | ✅ |

## Reporting a Vulnerability

ChameleonNet takes security seriously. If you discover a security vulnerability, please **do not** open a public issue.

Instead, send a description of the issue to the maintainers via GitHub Security Advisories or reach out directly.

We will acknowledge receipt within 48 hours and provide a timeline for a fix and disclosure.

## Scope

The following areas are in scope for security reports:

- AES-256-GCM implementation and nonce generation
- Key derivation function and salt generation
- Handshake protocol and magic byte verification
- Memory safety and buffer overflow prevention
- Any information leakage through traffic patterns (beyond the intended DPI evasion)

## Out of Scope

- Traffic morphing effectiveness against specific DPI vendors (this is a best-effort feature, not a guarantee)
- Side-channel attacks requiring physical access
- DoS attacks (rate limiting is configurable but not guaranteed against all vectors)
