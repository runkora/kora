# What is kora

kora is an open source control plane for running agent factories in production. An agent on a laptop runs with its developer's credentials and leaves no audit trail of what it did. kora makes those agents safe to run at scale.

kora is not an agent framework, a model router, or an LLM wrapper. It runs the agent harnesses you already use, such as Claude Code, Codex and OpenCode, on Kubernetes, and governs everything they do.

## What teams build on kora

- Review and context agents that take part in every pull request.
- Fleet-wide changes shipped as the same pull request to every team.
- Incident agents that triage an alert and start root cause analysis before anyone is paged.
- Bug-hunting agents that run weekly and explore your applications and codebases.
- General purpose coding agents that pick up work from your task or issue boards.

## Where to go next

- [Deploy kora](guides/deployment.md)
- [Runs and sessions](concepts/runs-and-sessions.md)
- [Sandboxes](concepts/sandboxes.md)
- [Identity](concepts/identity.md)
- [Connectivity](concepts/connectivity.md)
