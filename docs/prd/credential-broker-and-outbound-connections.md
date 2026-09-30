# Credential Broker & Outbound Connections - Product Requirements Document

**Status:** Draft
**Created:** 2026-06-06
**Last Updated:** 2026-06-06
**Owner:** Platform Engineering
**Stakeholders:** Tenant Admins, Agent Authors, Security/Compliance, Platform Engineering

---

## Executive Summary

kora agents can think, but they cannot yet *act on the outside world* in a governed way. This feature lets a tenant connect agents to third-party services — GitHub, cloud providers, Kubernetes, OAuth/OIDC apps, and arbitrary SaaS — so an agent running in a kora sandbox can do real work (pull and push a repo, read an object from S3, call an API) **without the agent ever holding the credential**. An admin registers a *connection* once for the tenant; agents are then *granted access* to it through policy. The secret lives in the platform, every outbound call is authorized before it happens, and every call is attributed to the agent and the run that made it.

This is the "How does it connect?" pillar of kora, and it is the largest single unlock for agent usefulness: an agent that can only talk to a model is a chatbot; an agent that can connect out — under identity, policy, and audit — is a coworker.

---

## Problem Statement

### Current State

Agents run in isolated kora sandboxes and can reach kora's own services (the inference gateway, the control plane). They **cannot** reach external services in any governed way. There is no mechanism to give an agent access to GitHub, a cloud account, a Kubernetes cluster, or a SaaS API. The only options available to operators today are bad ones:

- **Bake a credential into the agent's environment** — the agent process can read, log, or exfiltrate it; a single prompt-injection or a compromised tool leaks a long-lived secret.
- **Don't let the agent connect out at all** — which is the current reality, and it caps agents at advisory work.

There is no way to say "this agent may push to *these* repos" or "this agent may read from *that* S3 bucket" and have the platform enforce it.

### User Pain Points

- **Agent Authors** can build an agent that reasons about code but cannot have it open a pull request, run a deployment, or touch any real system.
- **Tenant Admins** have no safe way to delegate third-party access to an agent — it's all-or-nothing, with the credential exposed.
- **Security/Compliance** cannot answer "what did this agent touch, on what system, with what permission, when?" because there is no governed exit point and no per-call record.
- **Developers** using an agent have to hand it their personal tokens, which is both a security risk and a constant source of friction.

### Business Impact

- Agents are stuck at "suggest" and cannot reach "do," which is where the value is.
- Every customer evaluation hits the same wall: "this is neat, but I can't let it touch our GitHub / AWS / cluster."
- Without a governed credential story, kora cannot be sold into security-conscious enterprises — the exact buyer the platform targets.

---

## Goals and Objectives

### Primary Goals

1. **Let agents connect out** to third-party services from the sandbox, using the tools they already run (`git`, `gh`, `aws`, `kubectl`, SDKs, `curl`) with no modification.
2. **Keep the credential out of the agent.** The agent never sees, holds, or can exfiltrate the real secret. This is a structural guarantee, not a policy the agent is trusted to follow.
3. **Govern every connection.** An agent can only reach a service it has been explicitly granted, and every outbound call is authorized before it executes.
4. **Make adding a connection simple and self-service** for a tenant admin — register once, grant to agents, done — and reusable across an open-ended set of services.
5. **Attribute and audit everything.** Every external call traces back to the agent, the run, and the connection used.

### Success Metrics

- An agent can `git push` to a granted repository with **zero credentials present in the sandbox**, verified by inspection of the agent environment and process memory.
- A tenant admin can register a new GitHub connection and grant an agent access to it in **under 5 minutes**, without filing a platform engineering ticket.
- **100%** of outbound external calls are authorized against policy before execution and produce an audit record naming the agent, run, and connection.
- Revoking an agent's access to a connection takes effect for **new calls immediately** and requires a single action.
- Adding support for a *new* SaaS that uses an already-supported authentication style (e.g. an API-key service) requires **no engineering work** — only a tenant-level registration.

### Non-Goals

- **Delegated ("act as the human") authority.** In this release an agent acts under its *own* identity (e.g. as a GitHub App / bot), and any human with the right scope can run it. Acting on behalf of a specific human, bounded by that human's own permissions, is a deferred follow-on.
- **A platform-wide event/audit spine.** Audit records are produced, but the general event-spine subsystem is out of scope here.
- **Building a secrets vault.** The platform stores connection secrets using existing secret mechanisms; a dedicated secret-management product is not in scope.
- **Inbound access to agents** (agents exposing services) — this PRD is about *outbound* connections only.
- **Non-HTTP protocols** beyond what the named tools require (e.g. raw TCP services, git-over-SSH — SSH connections are explicitly not supported; HTTPS is used instead).

---

## Personas

- **Tenant Admin** — registers connections for the tenant, supplies the third-party credential or installs the kora app, sets the ceiling of what the connection may ever do, and grants agents access.
- **Agent Author** — builds an agent type and requests which connections it needs; does not handle secrets.
- **Developer / Operator** — runs an agent to get work done; never configures or sees credentials.
- **Security / Compliance** — reviews which agents have which connections and audits what was done with them.

---

## What We Support: Connections

A **connection** is a governed link from the tenant to one external service. The platform supports connections by the *style of authentication* the service uses, which means one supported style covers many services. The launch set, in priority order:

### Launch (v1)

| Connection | What the agent can do | What the admin provides |
|---|---|---|
| **GitHub** | `git` clone / pull / push / commit over HTTPS, `gh` CLI, and the GitHub REST/GraphQL API, scoped to chosen repositories | Installs the **kora GitHub App** on the org and selects repositories + permission ceiling (e.g. contents, pull requests) |

GitHub is the flagship and proves the end-to-end pattern. The agent acts as the GitHub App (a bot identity); commits and actions are attributed to the app, and through kora, to the run.

### Fast-follow (v1.x — same architecture, additional connection styles)

| Connection | What the agent can do | What the admin provides |
|---|---|---|
| **Generic API-key SaaS** | Call any HTTPS API that authenticates with a static API key (e.g. Stripe, SendGrid, internal APIs) | Base URL, the API key, and where the key goes (header or query) |
| **Generic OAuth2 service (client credentials)** | Call any service that issues tokens via the OAuth2 client-credentials grant — the user's "register a third-party OIDC/OAuth app" case | Token endpoint, client ID + secret, allowed scopes |
| **Amazon Web Services** | `aws` CLI and AWS SDK calls (e.g. S3, etc.) under a role the tenant authorizes | An IAM role kora may assume (cross-account trust) and a permissions ceiling |
| **Kubernetes** | `kubectl` and Kubernetes SDK calls against an authorized cluster | Cluster API endpoint + CA, and a credential/role the tenant authorizes |
| **Custom HTTP connection** | Call any HTTPS endpoint with a configurable authentication style | Base URL + chosen auth style and its inputs |

### Later (deferred)

- **Act-as-the-human (delegated) connections** — the agent uses the *initiating user's* authority (e.g. GitHub acting as the user, Microsoft Graph reading the user's mail), bounded by what that user can do. The launch architecture is designed to add this without rework.
- **Google Cloud / Azure** cloud connections.
- **Governed MCP-server connections** — connections consumed by managed MCP tool servers rather than the sandbox directly.

The list is intentionally a mix of *specific* connections (GitHub, AWS, Kubernetes) and *generic* ones (API-key, OAuth2, Custom HTTP). The generic styles mean the supported surface is open-ended: most new SaaS a customer asks for will already be covered by "API-key" or "OAuth2 client-credentials" and need only a registration, not a platform change.

---

## User Experience: Adding a Connection to Your Tenant

The experience has three distinct moments, deliberately separated so that *holding a secret* and *using a connection* are never the same action.

### 1. Register the connection (Tenant Admin, once per service)

1. Admin opens **Tenant → Connections → Add connection**.
2. Chooses a connection type (GitHub, AWS, Kubernetes, OAuth2 service, API key, Custom HTTP).
3. Completes the type-specific setup:
   - **GitHub:** clicks "Install kora GitHub App," is taken through GitHub's install flow, selects the org and the specific repositories, and chooses the permission ceiling. No token is ever copy-pasted.
   - **AWS:** provides the role ARN kora may assume and confirms the cross-account trust; sets a permissions boundary.
   - **Kubernetes:** provides the cluster endpoint and CA and the credential kora should use.
   - **OAuth2 service:** enters the token endpoint, client ID and secret, and the scopes kora may request.
   - **API key:** enters the base URL and the key, and where it should be sent.
4. Sets the connection's **ceiling** — the maximum it may ever do (e.g. which repos, which scopes). Nothing granted to an agent can exceed this.
5. Saves. The connection now appears as a governed resource in the tenant, showing its type, ceiling, and which agents (if any) can use it.

The credential the admin provided is stored by the platform and is **never shown again** and never returned by any API. From here on, the admin manages *access*, not the secret.

### 2. Grant an agent access (Tenant Admin / Agent Author)

1. On an agent type (or in the policy view), the admin sees the tenant's connections and ticks the ones this agent may use, choosing the action level (e.g. read vs. write) within the connection's ceiling.
2. Saving creates a governed grant. The agent type now lists its connections; the connection lists its agents. There is one place to see "who can touch what."

An agent with no grant for a connection simply cannot reach it — attempts are denied and recorded.

### 3. Use it (Developer / Operator — zero configuration)

The developer just uses the agent. When the agent runs `git push`, calls the GitHub API, or runs `aws s3 cp`, it works — the tool behaves exactly as it would on a laptop. The developer never configures a token, never sees a secret, and never thinks about credentials. Behind the scenes the platform supplies the right credential, scoped and short-lived, only for that call.

### 4. Audit & revoke (Security / Compliance, Admin)

- **Audit:** every external call is recorded with the agent, the run, the connection, the action, and the result — so "what did this agent do on GitHub at 14:32" is answerable.
- **Revoke:** removing a grant (or disabling the connection) stops new calls immediately. Rotating the underlying secret is an admin action that does not require touching any agent.

---

## User Stories

**As a** Tenant Admin,
**I want to** register our GitHub organization as a connection and choose exactly which repositories agents may touch,
**So that** I can let agents work on our code without handing any agent a GitHub token.

**Acceptance Criteria:**
- [ ] I can install the kora GitHub App and pick specific repositories and a permission ceiling.
- [ ] No GitHub token is ever displayed to me or stored anywhere an agent can read it.
- [ ] After registration, the connection is visible as a governed resource with its ceiling.

---

**As a** Tenant Admin,
**I want to** grant a specific agent access to a connection at a chosen action level,
**So that** only the agents that need a service can reach it, and only as far as I allow.

**Acceptance Criteria:**
- [ ] I can grant an agent read or write access to a connection within its ceiling.
- [ ] Removing the grant stops the agent's access to that connection for new calls immediately.

---

**As an** Agent Author,
**I want to** declare which connections my agent needs,
**So that** I can build agents that take real action without ever handling credentials.

**Acceptance Criteria:**
- [ ] I can request connection access for an agent type without entering any secret.
- [ ] My agent's runs can use a granted connection through ordinary tools (`git`, `gh`, SDKs).

---

**As a** Developer using an agent,
**I want to** have the agent push code or call an API without giving it my personal tokens,
**So that** I get the work done without exposing my own credentials.

**Acceptance Criteria:**
- [ ] Running the agent against a granted connection "just works" with no credential setup.
- [ ] My personal tokens are never required for the agent to act.

---

**As a** Security reviewer,
**I want to** see every external call an agent made and confirm credentials never entered the agent,
**So that** I can sign off on agents acting on production systems.

**Acceptance Criteria:**
- [ ] Every outbound call is attributed to an agent, run, and connection in an audit record.
- [ ] Inspection of the agent environment shows no third-party credential present.
- [ ] An unauthorized call is denied before execution and recorded as denied.

---

## Rollout

1. **GitHub connection (bot authority)** end-to-end as the launch milestone — register, grant, `git push`, audit.
2. **Generic connection styles** (API-key, OAuth2 client-credentials) to make the supported surface open-ended.
3. **AWS and Kubernetes** connections.
4. **Delegated ("act as the human") authority** as the first major follow-on.

---

## Open Questions

- For GitHub, do we require the kora GitHub App install flow only, or also allow an admin to bring an existing App's credentials directly?
- What is the right default token lifetime / caching window per connection type, balancing freshness against rate limits?
- How should connection registration and grants surface in the existing policy administration dashboard versus a dedicated Connections area?
- What is the minimum audit record we ship in v1 given the event spine is out of scope?

---

## Related Documents

- ADR: *JIT Credential Brokering via a Per-Tenant Egress Gateway* (`docs/adr/2026-06-06-jit-credential-brokering-egress-gateway.md`) — the technical design and per-tool flows.
- Supersedes the direction in ADR *Credential-Aware Outbound Gateway* (`docs/adr/2026-03-05-credential-aware-outbound-gateway.md`).
