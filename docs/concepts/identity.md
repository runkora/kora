# Identity

In kora, every agent has its own identity and is a first-class citizen of the platform. Agent and human identity follow fundamentally different shapes. Humans are bound to short lived login sessions whilst agents are often long running stable workloads with no login. Humans and agents get issued the same shaped platform tokens by kora, allowing [policy](../architecture/policy.md) to evaluate both in the same way. Every action on the platform can be traced back to who or what performed it. See [identity architecture](../architecture/identity.md) for how kora proves an agent's identity.

## Autonomous and delegated agents

Every agent uses one of two identity models, which is chosen when creating the agent and fixed for its lifetime.

- `autonomous`: the agent acts as itself, using only the access it has been granted.
- `delegated`: the agent acts on behalf of the person who started the run. Its access is limited to the intersection of the agent and that person's permission set.

### Autonomous agents

Autonomous agents run fully dark, without a person attached. They are started by triggers from webhooks, queues, schedules or the API, and are governed only by their own identity, budget and the access granted to it. A person can also start an autonomous agent, but only when they have been granted access to that agent, because the run uses the agent's access rather than their own. Autonomous agents hold standing access, so they should be restricted and managed by platform admins.

### Delegated agents

Delegated agents are for work a person hands down to an agent. The agent works with that person's access and can never do more than they can. If an agent has full access to the Slack MCP server but the person running it can only read messages, the delegated agent can only read messages.

Every delegated run is attributed to the person who started it, and consumes the budget of the person who started it. Delegated agents can only be started by a person, and only that person can steer the run. Because a delegated agent can never exceed the access of the person running it, access can be broadly provided by platform admins.

ADRs: [unified-identity-model](../adr/2026-03-05-unified-identity-model.md), [delegated-agent-identity](../adr/2026-07-23-delegated-agent-identity.md).
