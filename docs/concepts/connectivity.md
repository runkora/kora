# Connectivity

In kora, agents reach models, tools and external systems without ever holding a credential. Every request leaves the agent's pod through its sidecars. The sidecars attach the agent's platform token and evaluate the request against policy before it reaches a kora gateway. The gateway holds the credential for the destination and injects it in flight. See [connectivity architecture](../architecture/connectivity.md) for the request path.

## Models

Every model call in kora goes through the inference gateway. The gateway holds the upstream keys for every inference provider a platform team registers.

Platform teams offer models to their teams and agents as virtual models. A virtual model is a stable alias for a model. Humans and agents call the alias without knowing the provider's model name. This allows platform teams to upgrade the model behind an alias or route to another provider without affecting the downstream agent or user.

A virtual model has basic configuration to fail over between providers, regions and models. A platform team can route a Sydney endpoint to a US endpoint when it is unavailable, or fall back to a different model. Enterprises already running a model gateway like LiteLLM or Portkey can route a virtual model to it through a `compatible` endpoint.

Each virtual model publishes its own scope for agents and humans to be granted. Every call is recorded against the run and the agent or person that made it. This allows platform teams to track model spend and set budgets for tenants, roles, agents and people. See [registering an inference provider](../guides/register-inference-provider.md) for more.

## MCP servers and connections

For agents to be effective, they need the ability to connect to the outside world. Agents reach other systems in two ways. An MCP server offers an agent a published set of tools, such as searching issues or creating a ticket. Many tasks do not fit a predefined toolset: cloning a repository, running kubectl or using any CLI all need direct access to the system's own API. A connection gives an agent's sandbox that access. The agent makes the request without a credential, and kora injects the credential on the way out.

The two take different paths through kora. MCP calls go from the agent runtime pod through the tool gateway and need no sandbox. Connections are used from a sandbox through the egress gateway. Platform teams register an MCP server for a system with a defined set of actions to offer, and can register a connection when agents need the system's own CLI or API.

## MCP servers

Platform teams can [register an MCP server](../guides/register-mcp-server.md) with the tool gateway to provide access to the systems agents require.

The tool gateway routes every MCP call and serves an `OAuth 2.1` protected endpoint for humans and agents alike. On a configurable interval, the gateway introspects each server's tools and publishes a scope per tool, such as `mcp:github:create_issue:use`. This allows platform teams to grant an agent the tools it needs from a server and nothing more. When an MCP client or agent calls the MCP `tools/list`, it is provided only the tools it has access to, even if the server has more tools available.

## Connections

Platform teams [register a connection](../guides/register-connection.md) for each external system, such as GitHub or a Kubernetes cluster. A connection holds the credential kora uses for that system, such as a GitHub App key or a ServiceAccount token. Every outbound request from a [sandbox](sandboxes.md) is intercepted and routed through the egress gateway. When the request is for a connected system, the egress gateway checks the agent's grant, mints a just-in-time credential and injects it into the request. The agent receives the response and never sees the credential.

Each connection publishes a scope for every action it supports, such as reading repository contents or writing pull requests on GitHub. A grant can be narrowed to specific resources, such as a set of repositories or a single Kubernetes namespace. This allows platform teams to register one GitHub connection and give each agent only the actions and repositories it needs.

Each environment also lists hosts its sandboxes may reach without a connection, such as a package registry. kora denies every other host. Connections and hosts can be attached to an agent directly or through its environments. An agent with several environments can use the connections and hosts of all of them.

ADRs: [mcp-tool-gateway](../adr/2026-06-29-mcp-tool-gateway.md), [jit-credential-brokering-egress-gateway](../adr/2026-06-06-jit-credential-brokering-egress-gateway.md), [agent-pod-egress-lockdown](../adr/2026-09-30-agent-pod-egress-lockdown.md), [sandbox-environments](../adr/2026-10-03-sandbox-environments.md).
