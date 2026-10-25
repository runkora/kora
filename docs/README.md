# Documentation

Use this index to find kora's concepts, guides, architecture, reference, contributor docs and decision records.

## Concepts

Learning about kora.

- [Runs and sessions](concepts/runs-and-sessions.md): what a run is, how it starts, its states and how to steer it.
- [Sandboxes](concepts/sandboxes.md): the isolated pods agents provision for compute, and the environments and images behind them.
- [Identity](concepts/identity.md): agent identity, and the difference between autonomous and delegated agents.
- [Connectivity](concepts/connectivity.md): how agents reach models, MCP servers and external systems without holding credentials.

## Guides

Tasks for platform teams running kora.

- [Deployment](guides/deployment.md): install kora in a Kubernetes cluster.
- [Tenants](guides/tenants.md): onboard a tenant so its team can sign in and get their own workspace.
- [Register an inference provider](guides/register-inference-provider.md): connect the platform to upstream inference providers and offer models to agents.
- [Register an MCP server](guides/register-mcp-server.md): give agents and humans governed access to a set of tools.
- [Register a connection](guides/register-connection.md): give agents access to an external system without holding its credentials.
- [OAuth clients](guides/oauth-clients.md): configure clients that authenticate users against kora.
- [Policy and roles](guides/policy-and-roles.md): configure roles, grants and route access.
- [Declarative resources](guides/declarative-resources.md): define virtual models, environments, agents, triggers and roles as Kubernetes resources in Git.
- [Advanced customisations](guides/advanced-customisations.md): patch the Kubernetes resources kora provisions for your agents.

## Architecture

The [architecture overview](../ARCHITECTURE.md) explains the platform as a whole. These pages cover the moving parts behind each concept.

### Agent execution

- [Agent execution](architecture/agent-execution.md): the agent runtime pod, the session-manager and the control plane that runs agents.
- [Sandboxes](architecture/sandboxes.md): the sandbox pod, how commands reach it, placement modes and warming.

### Identity

- [Identity](architecture/identity.md): human and workload identity, OIDC, SPIFFE exchange, sessions and platform tokens.

### Policy

- [Policy](architecture/policy.md): the authorisation model and where policy is enforced.
- [Policy plane](architecture/policy-plane.md): the policy services, bundles and request path.

### Connectivity

- [Connectivity](architecture/connectivity.md): the request path for MCP calls and connections.
- [Inference gateway](architecture/inference-gateway.md): model-provider routing and the OpenAI- and Anthropic-compatible APIs.
- [MCP tool gateway](architecture/mcp-tool-gateway.md): tool discovery, policy enforcement and execution.
- [Egress gateway](architecture/egress-gateway.md): governed outbound traffic and just-in-time credential injection.

[Runtime topology](architecture/runtime-topology.md) maps these onto the deployed services and namespaces.

## Reference

- [Secrets](reference/secrets.md): secret sources, projection and rotation responsibilities.
- [Telemetry configuration](reference/telemetry.md): the observability settings for logs, traces and exporters.

## Contributing

People working on kora itself start with [CONTRIBUTING.md](../CONTRIBUTING.md).

- [Local development](contributing/local-development.md): set up the development environment and work against the local cluster.
- [Troubleshooting](contributing/troubleshooting.md): diagnose common failures in the local environment.
- [Code organisation](code-organization.md): binaries, ports, feature packages, dependency boundaries and repository structure.
- [Backend golden path](contributing/backend-golden-path.md): add a backend feature from storage through to its HTTP API.
- [Configuration](contributing/configuration.md): typed service configuration and environment overlays.
- [Observability](contributing/observability.md), [logging](contributing/logging.md) and [tracing](contributing/tracing.md): instrumenting kora's services.
- [Frontend testing](contributing/frontend-testing.md): run and write frontend tests.
- [Frontend documentation](../frontend/docs/README.md): Hades, component placement, state patterns and frontend conventions.
- [Agent development guide](../AGENTS.md): the machine-enforced coding rules used by development agents.

## Decision records

kora uses a PRD to agree the problem and scope, then an ADR to record the technical decision before significant implementation begins.

- [Product requirements](prd/README.md): accepted requirements and feature scope.
- [Architecture decisions](adr/README.md): the technical decisions and their consequences.
- [PRD template](prd/template.md) and [ADR template](adr/template.md): starting points for new records.

ADRs are named `YYYY-MM-DD-kebab-slug.md` and cited by slug. Bug fixes and minor changes do not require a PRD or ADR. The full workflow is in [CONTRIBUTING.md](../CONTRIBUTING.md).
