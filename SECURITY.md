# Security Policy

## Overview

Orbit is a platform orchestrator for AI agents that handles identity, credentials, policy enforcement, and agent execution. Security is fundamental to our mission of making autonomous agents production-grade and safe.

**If you discover a security vulnerability, please report it responsibly** as described below.

## Supported Versions

| Version | Supported          |
| ------- | ------------------ |
| main    | :white_check_mark: |

As Orbit is in early development, we currently support only the main branch. Once we release versioned releases, this table will be updated to reflect our support policy.

## Reporting a Vulnerability

**DO NOT** open a public GitHub issue for security vulnerabilities.

Instead, please report security issues via one of these methods:

### GitHub Security Advisories (Preferred)

1. Go to the [Security Advisories page](https://github.com/clear-route/orbit/security/advisories)
2. Click "Report a vulnerability"
3. Fill out the advisory form with details

### Email

Alternatively, email security reports to: **security@clearroute.io**

Please include:
- Description of the vulnerability
- Steps to reproduce
- Potential impact
- Any suggested fixes (if applicable)

## Response Timeline

- **Initial Response**: Within 48 hours of report
- **Status Update**: Within 5 business days
- **Fix Timeline**: Depends on severity (critical issues prioritized)

## Security Considerations for Orbit

Given Orbit's role as infrastructure for autonomous agents, the following areas are particularly security-sensitive:

### Identity & Authentication
- Agent identity validation
- Human authentication integration (SSO, Entra, etc.)
- Token issuance and validation
- Credential brokering

### Policy Enforcement
- Policy bypass vulnerabilities
- Policy injection attacks
- Authorization boundary violations

### Agent Execution
- Sandbox escapes
- Tool permission escalation
- Resource exhaustion (DoS)
- Malicious agent behavior

### Secrets Management
- Credential leakage
- Secrets in logs or events
- Key material handling

### Supply Chain
- Dependency vulnerabilities
- Build/release integrity
- Container image security

## Security Best Practices for Contributors

When contributing to Orbit:

1. **Never commit secrets** - No API keys, tokens, or credentials in code
2. **Validate all inputs** - Especially agent-provided data and user inputs
3. **Principle of least privilege** - Grant minimal permissions necessary
4. **Secure defaults** - Default configurations should be secure
5. **Audit logging** - Security-relevant actions must be logged
6. **Dependency hygiene** - Keep dependencies updated and minimal

## Disclosure Policy

Once a vulnerability is fixed:

1. We will publish a security advisory
2. Credit will be given to the reporter (unless anonymity requested)
3. CVE may be requested for significant vulnerabilities
4. Detailed timeline and impact will be disclosed

## Security Updates

Subscribe to security advisories:
- Watch this repository and enable "Security alerts"
- Follow releases for security patches
- Monitor our security advisories page

Thank you for helping keep Orbit and its users safe!