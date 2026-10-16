# Documentation

Use this index to find kora's guides, architecture, engineering reference, and decision records.

## Guides

- [Deployment](guides/deployment.md): install kora in a Kubernetes cluster.
- [Tenants](guides/tenants.md): onboard a tenant so its team can sign in and get their own workspace.
- [Model providers](guides/register-inference-provider.md): connect the platform to upstream inference providers.
- [Connections](guides/register-connection.md): register external services agents can reach without holding credentials.
- [OAuth clients](guides/oauth-clients.md): configure clients that authenticate users against kora.
- [MCP servers](guides/register-mcp-server.md): give agents and humans governed access to a set of tools.
- [Local development](contributing/local-development.md): set up the development environment and work against the local cluster.
- [Troubleshooting](contributing/troubleshooting.md): diagnose common failures in the local environment.
- [Backend golden path](contributing/backend-golden-path.md): add a backend feature from storage through to its HTTP API.
- [Policy and roles](guides/policy-and-roles.md): configure roles, grants, and route access.
- [Frontend testing](contributing/frontend-testing.md): run and write frontend tests.

## Architecture

The [architecture overview](../ARCHITECTURE.md) explains the platform as a whole. The pages here go deeper into the three questions kora answers for every production agent.

### Who is this agent?

- [Identity](architecture/identity.md): human and workload identity, OIDC, SPIFFE exchange, sessions, and platform tokens.
- [Agent execution](architecture/agent-execution.md): how an agent run is attributed, scheduled, isolated, and observed.

### What can it access?

- [Policy](architecture/policy.md): the authorisation model and where policy is enforced.
- [Policy plane](architecture/policy-plane.md): the policy services, bundles, and request path.

### How does it connect?

- [Connectivity](architecture/connectivity.md): the end-to-end connection model and credential brokering path.
- [Egress gateway](architecture/egress-gateway.md): governed outbound traffic and just-in-time credential injection.
- [MCP tool gateway](architecture/mcp-tool-gateway.md): tool discovery, policy enforcement, and execution.
- [Inference gateway](architecture/inference-gateway.md): model-provider routing and the OpenAI- and Anthropic-compatible APIs.

[Runtime topology](architecture/runtime-topology.md) maps these concerns onto the deployed services and namespaces.

## Engineering reference

- [Code organisation](code-organization.md): binaries, ports, feature packages, dependency boundaries, and repository structure.
- [Configuration](contributing/configuration.md): typed service configuration and environment overlays.
- [Secrets](reference/secrets.md): secret sources, projection, and rotation responsibilities.
- [Observability](contributing/observability.md): logging, tracing, and telemetry configuration.
- [Frontend documentation](../frontend/docs/README.md): Hades, component placement, state patterns, and frontend conventions.
- [Agent development guide](../AGENTS.md): the complete machine-enforced coding rules used by development agents.

## Decision records

kora uses a PRD to agree the problem and scope, then an ADR to record the technical decision before significant implementation begins.

- [Product requirements](prd/README.md): accepted requirements and feature scope.
- [Architecture decisions](adr/README.md): the technical decisions and their consequences.
- [PRD template](prd/template.md) and [ADR template](adr/template.md): starting points for new records.

ADRs are named `YYYY-MM-DD-kebab-slug.md` and cited by slug. Bug fixes and minor changes do not require a PRD or ADR. The full workflow is in [CONTRIBUTING.md](../CONTRIBUTING.md).
