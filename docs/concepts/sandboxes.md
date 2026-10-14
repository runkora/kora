# Sandboxes

A sandbox is an isolated pod that an agent provisions for its run when a task needs compute or tooling beyond its runtime, such as building and testing code. Each sandbox is created from an environment definition. An environment definition is a composition of:
- A sandbox image
- Configured system resources
- Optional connectivity and egress

Runs that only use MCP servers do not need a sandbox and can complete their task inside the agent's runtime pod. See [agent execution](../architecture/agent-execution.md) for how the runtime and the sandbox fit together.

## Lifecycle

The agent decides when it needs a sandbox. The runtime MCP server gives it three tools: 
- `list_environments` to see the environments it can use
- `provision_sandbox` to start a sandbox in one of them
- `replace_environment` to switch environments when the task changes.

Each run has at most one sandbox at a time. The sandbox is ephemeral and is deleted when the run ends or is terminated.

## Environments

Environments are the composable definition that the platform uses to expose sandboxes to agents. When [creating an agent](../guides/creating-an-agent.md), you can attach one or more environments to the `agent-type`, this allows the agent to choose the appropriate environment for its task. An agent with no attached environment provisions a sandbox using the `kora-base` at the default size.

## Sandbox images

Platform teams can register custom sandbox images for their agents to use. These are registered as `SandboxImage` resources in a tenant namespace. 

kora provides four sandbox images out of the box:
- kora-base: a basic image with scripting tooling
- kora-go: a basic go base image with tooling and language installed
- kora-node: a basic node image with pnpm
- kora-python: a basic python image

Custom sandbox images are useful when teams want to pre-install dependencies, toolchains and bundle together a `devcontainer` like experience for their agents to use in runs. This can significantly reduce the build and test time of coding agents operating on a repository, and can be useful for packaging your own tools for an agent to use in its sandbox. A custom sandbox must be built on the `kora-sandbox-base`, which is validated by the kora operator.

When registering an image, the platform team can mark it as `warm`, which pre-pulls the image onto every sandbox node so new sandboxes start without waiting for an image download. Paired with autoscaling and capacity warming, a low-priority placeholder holds a fresh node open with the image cached. A new sandbox can be provisioned on this node immediately.

## Egress

A sandbox does not hold any credentials. All of its egress is forced through the `egress-gateway`, which evaluates the request against the agent's identity and policy, then injects a just-in-time credential for the destination. See [connectivity](../architecture/connectivity.md) for the outbound path.