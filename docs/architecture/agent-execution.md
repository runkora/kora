# Agent execution

In runkora, agents run as governed workloads on top of Kubernetes. Each agent process runs inside a shared runtime orchestrated by the `session-manager`. This session manager is responsible for the lifecycle of an agent's run. The session manager runs each agent process under `landlock` and provisions an isolated workspace for the agent to operate within, provisions the tools it has access to and triggers the agent startup. Every agent you create in the platform holds its own identity, these are known as `agent-types`, each agent-type gets its own agent pod with a running session-manager. 

Agents do not have access to credentials in their session or environment. An agent's access outside of its pod is governed by the control plane: tokens are injected on egress from an agent's pod for all requests and evaluated by policy before making a network hop.

![Agent pod anatomy](../diagrams/agent-pod-anatomy.svg)

See [creating an agent](../guides/create-an-agent.md) for configuring an agent-type's identity, behaviour and access.

## Control plane and tenant workloads

Agents are run as tenant workloads within kora. A central `kora-agents` service within the `kora-system` owns the lifecycle of `agent-types` and their `session-managers`. It provisions the Kubernetes workloads into the tenant namespace and is responsible for forwarding run events to the appropriate tenant from triggers and conversations and managing the scaling of resources for agents and sandboxes.

## Agent runtime pod

Each agent type gets a long-lived StatefulSet. Runs are multiplexed onto the `agent-type` instead of requiring a pod per agent run. This allows kora to scale agent runs at a different rate to the [sandboxes](sandboxes.md) those agents require for heavy workloads, reducing the idle state cost of always running agent infrastructure. 

This enables agent runs that do not require a sandbox to run barebones inside their `agent-type` pod, requiring no further compute to complete their tasks. This is typical of agentic use cases that only utilise MCPs to complete their tasks and do not require code-ready environments for build and test like activities.

The `session-manager` container hosts each agent run. `kora-session-runner` wraps the agent harness and confines it with Landlock in its own workspace. The session-manager also hosts a local `kora-runtime-mcp` which provides the harness the tools required to provision sandboxes, and manage its environment. `kora-shell` is the harness's shell. It runs every command inside the run's sandbox and refuses commands until the agent provisions one. Landlock lets the harness execute only its own binary, `kora-shell` and `kora-runtime-mcp`, so no command runs in the agent runtime pod.

| Component | Runs in | Job |
| --- | --- | --- |
| `kora-agents` | `kora-system` | Owns agent types, provisions their workloads, starts runs and provisions sandboxes |
| `session-manager` | Agent runtime pod | Hosts every run on the pod |
| `kora-session-runner` | Agent runtime pod | Wraps the agent harness and confines it to its workspace and its own binaries with Landlock |
| `kora-runtime-mcp` | Agent runtime pod | Gives the harness the tools to provision sandboxes and manage its environment |
| `kora-shell` | Agent runtime pod | Sends the harness's shell commands into the run's sandbox |
| `istio-proxy`, `identity-proxy`, `opa` | Every agent runtime pod and sandbox | Intercept outbound requests, attach the platform token and evaluate policy |

## Per-run sandbox

When an agent's task requires compute or tooling, it can provision a [sandbox](sandboxes.md) for its run. Each run can request its own isolated sandbox. The sandbox is ephemeral, and is torn down when the agent run ends, or is no longer using the idle sandbox. This allows us to provision larger compute nodes to give agents resource appropriate environments to complete their tasks effectively.

## Outbound path

The kora-operator injects the Istio `istio-proxy`, the kora `identity-proxy` and an OPA `ext_authz` sidecar which govern all egress and the chain stamps a [platform credential](../concepts/platform-tokens.md) on all requests.

No credentials are brokered or stored in an agent's environment, session or sandbox. Every outbound request leaves the pod through its sidecar chain:

1. The agent makes a bare request with no credential.
2. `istio-proxy` intercepts the request and passes it to `identity-proxy`.
3. `identity-proxy` attaches the agent's platform token.
4. The `opa` `ext_authz` sidecar evaluates the token and the request against policy, and denies it in the pod or forwards it.

Every request is evaluated in the pod before it makes a network hop to the rest of the platform.

`identity-proxy` obtains that token by exchanging the pod's SPIFFE SVID for a kora platform JWT with the `identity` service. It caches this token for the lifetime of its SPIFFE identity and automatically refreshes. The node's SPIRE agent delivers the SVID to the sidecars through the SPIFFE CSI socket. Agents that run without a sandbox can only connect to platform services, all other egress is denied.

For any external system, the agent must provision a sandbox. The sandbox's egress is forced through Envoy to the `egress-gateway`, which runs the same identity and policy checks and then mints and injects a just-in-time credential for the destination service.

See [connectivity](connectivity.md) for a detailed walkthrough of the outbound path.

## Related decisions

- [Run and session lifecycle](../adr/2026-05-11-run-and-session-lifecycle.md)
- [Agent execution infrastructure](../adr/2026-05-11-agent-execution-infrastructure.md)
- [Per-session home isolation](../adr/2026-06-04-per-session-home-isolation.md)
- [Sandbox provisioning and shell proxy](../adr/2026-05-11-sandbox-provisioning-and-shell-proxy.md)
- [Envoy ext_proc sidecar token injection](../adr/2026-03-21-envoy-ext-proc-sidecar-token-injection.md)
- [SPIFFE and SPIRE workload attestation](../adr/2026-03-05-spiffe-spire-workload-attestation.md)
- [JIT credential brokering in the egress gateway](../adr/2026-06-06-jit-credential-brokering-egress-gateway.md)
