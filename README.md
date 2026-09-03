# Orbit

[![License](https://img.shields.io/badge/license-Apache%202.0-blue.svg)](LICENSE) [![CI](https://github.com/clear-route/orbit/actions/workflows/ci.yml/badge.svg)](https://github.com/clear-route/orbit/actions/workflows/ci.yml)

Orbit is an open source control plane for managing and running agents at scale. An agent on a laptop runs with its developer's credentials and leaves no audit trail of what it did. Orbit makes those agents safe to run in production.

Orbit is not an agent framework, a model router, or an LLM wrapper. It runs and governs the agents you already use.

- **Identity.** Every agent deploys with its own verifiable identity and its own permissions.
- **Policy.** Every action is evaluated against policy before execution.
- **Connectivity.** Agents never handle credentials. Orbit intercepts outbound calls and injects a scoped, just-in-time credential.
- **Audit.** Every action traces back to the person or system that started it and the policy that authorised it.

## Getting started

- **Run Orbit in a Kubernetes cluster:** Install it with the [deployment guide](docs/guides/deployment.md).
- **Work on Orbit:** Get it running locally with the [local development guide](docs/guides/local-development.md).

## Documentation

- [Architecture](ARCHITECTURE.md): How Orbit works.
- [Local development](docs/guides/local-development.md): Running Orbit locally.
- [Contributing](CONTRIBUTING.md): Start here if you want to contribute.
- [docs/](docs/README.md): Comprehensive platform documentation.

## Security

Report vulnerabilities by following the [security policy](SECURITY.md). Do not open a public issue for a suspected vulnerability.

## License

Licensed under the Apache License, Version 2.0. See [LICENSE](LICENSE).
