# kora: The Control Plane for AI Agents

## Table of Contents

1. [Foundational Concepts](#foundational-concepts)
2. [The Three Core Questions](#the-three-core-questions)
3. [Architecture Overview](#architecture-overview)
4. [System Components](#system-components)
5. [Data Flow and Execution Model](#data-flow-and-execution-model)
6. [Identity and Authentication](#identity-and-authentication)
7. [Authorization and Policy Enforcement](#authorization-and-policy-enforcement)
8. [Credential Management](#credential-management)
9. [Agent Lifecycle](#agent-lifecycle)
10. [Deployment Model](#deployment-model)
11. [Key Abstractions](#key-abstractions)
12. [Development Philosophy](#development-philosophy)

---

## Foundational Concepts

### What is kora?

kora is **not an agent framework**. It is not an LLM wrapper. It is not a model router. 

kora is the **infrastructure layer that makes autonomous agents safe and governable in production**.

Today, when you run a Claude Code or Codex instance on your laptop, it has access to your credentials. It can run any command you could run. It can read any file you can read. It operates with your full privileges. This works fine on your laptop because you trust yourself and control what the agent does moment-to-moment.

The moment you want that agent to run unsupervised in a shared infrastructure environment—to execute tasks on behalf of other users, to make decisions at scale, to run continuously—the security model breaks. An agent that has access to everyone's credentials can compromise everyone. An agent that can execute any system call can be exploited to compromise the system. An agent whose actions are not traced cannot be held accountable for what it did.

kora solves this by inserting **policy enforcement at every boundary**. Before an agent can take any action—call an API, read a file, execute a command—kora intercepts the request, checks it against a policy defined by the system administrator, and either allows it or rejects it. The agent never sees raw credentials. The system always knows who initiated each action and what policy authorized it.

### Why This Matters

Autonomous agents are powerful but dangerous. They make decisions without human approval. They can execute millions of operations per second. They integrate with external systems on behalf of users. Without guardrails, an agent can:

- **Access data it shouldn't see**: If an agent runs as your identity, it can access anything you can access. In a multi-tenant system, this means an agent could leak customer data or compromise other users' workloads.
- **Perform unauthorized actions**: An agent could trigger expensive operations (cloud infrastructure, API calls) without approval, wasting budget or breaking systems.
- **Introduce security vulnerabilities**: An agent given access to a deployment system could be exploited to push malicious code to production.
- **Become unaccountable**: If you can't trace which agent performed which action, you cannot investigate incidents or prove compliance.

kora makes agents safe by answering three critical questions for every operation:

---

## The Three Core Questions

Every architectural decision in kora traces back to these three questions. Every policy enforcement point is designed to answer them. When you propose a feature or change, you must be explicit about which question it touches and how it preserves the others.

### Question 1: Who is this agent?

An agent must have a **machine-verifiable identity** at deploy time. This identity must be unforgeable and auditable. It must tie back to the human who deployed the agent and the policy that authorized the deployment.

**Why this matters:**
- Without identity, you cannot attribute actions to a specific agent.
- Without identity, you cannot enforce policies that are agent-specific.
- Without identity, a compromised agent cannot be contained—you have no way to isolate it from other systems.
- Without identity, compliance audits fail: "Who did this?" becomes unanswerable.

**How kora solves it:**
- Every agent receives a **SPIFFE identity** (Secure Production Identity Framework for Everyone) at deploy time.
- This is not a token the agent can fabricate or steal. It is cryptographic proof of identity enforced by the host platform.
- The identity includes a reference to the human operator who deployed the agent.
- The identity is bound to the pod/instance running the agent and cannot be reused elsewhere.
- Every action taken by the agent is tagged with this identity and logged.

**Key insight:** SPIFFE identity is infrastructure-level, not application-level. The agent does not need to know about it. The platform authenticates the agent to itself and includes that proof in every outbound call.

### Question 2: What can it access?

For every action an agent attempts to take, kora evaluates a **policy** that defines what this specific agent is allowed to do. The policy check happens **before execution**, not after.

**Why this matters:**
- By default, an agent can do nothing. This is the principle of least privilege.
- An administrator explicitly grants permissions to specific agents for specific actions.
- A permission might be scoped: "Agent X can read files in /workspace/ but not /root/".
- A permission might be time-limited: "Agent X can call the OpenAI API until 2024-12-31".
- A permission can be revoked instantly without redeploying the agent.
- Policies are declarative and centralized, not scattered across application code.

**How kora solves it:**
- kora stores policies in **OPA (Open Policy Agent)** bundles.
- Policies are written in Rego, a policy-as-code language. They are human-readable and version-controlled.
- For every action (API call, file read, command execution), kora evaluates the policy against:
  - The agent's identity
  - The specific action being requested
  - The resource being accessed
  - Contextual information (time, user principal, request headers)
- The policy engine runs in the **policy-gateway** service and is called from **Envoy proxies** at the edge (inside each pod).
- If the policy says "no", the action is rejected immediately and logged. The agent never sees a response that could be exploited.
- If the policy says "yes", the action proceeds to the next step (credential injection, etc.).

**Key insight:** Policy enforcement happens at the edge, not in business logic. This means policies are enforced consistently across all services and cannot be bypassed by changes to application code.

### Question 3: How does it connect?

An agent must **never handle credentials directly**. Instead, kora intercepts outbound calls from the agent and injects credentials just-in-time, just before the call reaches an external system.

**Why this matters:**
- If an agent stores a database password in memory, a memory dump reveals it.
- If an agent stores an API key on disk, a compromised filesystem leaks it.
- If an agent is captured by an attacker, it reveals all credentials it has ever handled.
- Instead, if an agent never sees credentials, there is nothing to steal.
- The upstream system (database, API provider, cloud platform) remains the source of truth for what access a user has.

**How kora solves it:**
- When a developer or agent wants to call an external system, they use a **token exchange protocol**.
- They send a request to kora with proof of identity (the SPIFFE identity) and a description of what they want to access.
- kora verifies that the policy allows this access (Question 2).
- kora then exchanges the agent's identity for credentials from the upstream system (e.g., "get temporary credentials for this database user").
- These credentials are injected into the outbound call at the network layer (inside the Envoy proxy).
- The agent never sees the credentials. The external system sees the agent's request with valid, temporary credentials attached.
- When the call completes, the credentials are discarded and never logged.

**Key insight:** This is credential brokering. It decouples the agent's identity from the credentials it uses. The agent proves who it is to kora; kora proves on the agent's behalf to external systems.

---

## Architecture Overview

### The Big Picture

```
┌─────────────────────────────────────────────────────────────────────────┐
│                         Kubernetes Control Plane                         │
│  (SPIFFE Root CA, CRD controllers, admission webhooks)                   │
└─────────────────────────────────────────────────────────────────────────┘
                                    ▲
                                    │
        ┌───────────────────────────┴───────────────────────────┐
        │                                                       │
┌───────▼────────────────────────────┐          ┌──────────────▼─────────┐
│    Agent Pod (with SPIFFE cert)    │          │   Operator             │
│  ┌──────────────────────────────┐  │          │  (CRD reconciliation)  │
│  │  Agent Process               │  │          │                        │
│  │  (Claude Code, etc.)         │  │          │  - Injects sidecars    │
│  │                              │  │          │  - Projects resources  │
│  └──────────────────────────────┘  │          │  - Manages identity    │
│  ┌──────────────────────────────┐  │          └────────────────────────┘
│  │  Envoy Proxy Sidecar         │  │
│  │  - Intercepts all traffic    │  │   ┌──────────────────────────────┐
│  │  - Injects SPIFFE identity   │  │   │   Policy Service             │
│  │  - Injects credentials       │  │   │  (PDP/PAP)                   │
│  │  - Enforces mTLS             │  │   │                              │
│  └──────────────────────────────┘  │   │  - Stores grants and roles   │
│  ┌──────────────────────────────┐  │   │  - Generates OPA bundles    │
│  │  Identity Proxy sidecar      │  │   │  - Provides policy decisions │
│  │  - Caches SPIFFE cert        │  │   └──────────────────────────────┘
│  │  - Manages credential cache  │  │
│  └──────────────────────────────┘  │   ┌──────────────────────────────┐
└──────────┬──────────────────────────┘   │  Policy Gateway              │
           │                              │  (PEP)                       │
           │                              │                              │
           │  mTLS traffic               │  - Evaluates OPA policies    │
           │  (authenticated & encrypted)│  - Decides allow/deny        │
           │                              │  - Logs decisions           │
           │ ┌────────────────────────────▶──────────────────────────────┘
           │ │
           ▼ ▼
    ┌────────────────────────────┐
    │  External System           │
    │  (Database, API, Cloud)    │
    │  ← Receives authenticated  │
    │    call with credentials   │
    └────────────────────────────┘
```

### Logical Layers

**Layer 1: Infrastructure (Kubernetes)**
- SPIFFE Root CA issues and rotates workload certificates.
- Custom Resource Definitions (Tenants, Platforms) allow declarative agent configuration.
- Admission webhooks enforce policies before resources are created.

**Layer 2: Identity (Agent SPIFFE Certificate)**
- Every agent pod has a SPIFFE certificate mounted at `/var/run/secrets/spiffe/cert.pem`.
- This certificate is cryptographically bound to the pod's identity and cannot be stolen or reused.
- It is proof of identity to every other component in kora.

**Layer 3: Policy (OPA Engine)**
- Policy Service generates OPA Rego bundles that define who can do what.
- These bundles are embedded in Envoy proxies.
- Policy decisions are evaluated at request time, not design time.

**Layer 4: Enforcement (Envoy Proxy Sidecars)**
- Envoy runs as a sidecar in every agent pod.
- It intercepts all outbound traffic from the agent.
- It verifies the SPIFFE certificate.
- It calls the Policy Gateway to evaluate policies.
- It injects credentials into requests.
- It enforces mTLS with other services.

**Layer 5: Credential Brokering (Identity Proxy + Token Exchange)**
- Identity Proxy is an ext_proc handler in Envoy that manages credential injection.
- For each outbound request, it exchanges the agent's SPIFFE identity for temporary credentials.
- Credentials are injected into the request and never logged or exposed to the agent.

**Layer 6: Control Plane (REST APIs)**
- Identity Service: manages agent identities, sessions, and PATs.
- Policy Service: stores and manages grants and roles.
- Inference Gateway: routes LLM calls and tracks usage.
- Kora Agents: orchestrates agent lifecycle and execution.
- Triggers: manages webhook ingestion and cron scheduling.
- Skills Service: serves agent skills from GitHub repositories.

---

## System Components

### Core Services

#### 1. Identity Service (port 8080)
**What it does:** Acts as the root of trust for the kora platform. Manages identities for agents, humans, and API clients.

**Responsibilities:**
- Issues and rotates SPIFFE certificates for agent pods.
- Issues PATs (Personal Access Tokens) for developers and CI/CD systems.
- Manages user sessions and token refresh.
- Stores the canonical tenant registry (which teams, which agents, which permissions).
- Issues temporary credentials for credential brokering (e.g., database passwords, API keys).
- Provides the OAuth2-like Device Authorization Grant flow for user login.

**Data owned:** 
- Agent identities and metadata
- User accounts and sessions
- PATs and their scopes
- Tenant information
- Token issue/revocation records

**Key interfaces:**
- `/v1/identities` — list and query agent identities
- `/v1/sessions` — create and refresh user sessions
- `/v1/pats` — manage personal access tokens
- `/v1/credentials` — exchange identity for temporary credentials
- `/v1/tenants` — query tenant information

**First-principles insight:** Identity Service is the root of trust. Every other component trusts it to answer the question "Is this agent who it claims to be?" Every policy decision and every credential issued traces back to a decision made by Identity Service.

---

#### 2. Policy Service (port 8083)
**What it does:** Stores and evaluates authorization policies. It is both the Policy Data Point (PDP — decides what is allowed) and the Policy Administration Point (PAP — stores the policies).

**Responsibilities:**
- Stores grants: associations between principals (agents, users) and permissions (e.g., "Agent X can call API Y").
- Stores roles: named collections of permissions (e.g., the "data-analyst" role has permissions to read certain tables).
- Generates OPA Rego bundles from the stored policies.
- Publishes these bundles to Envoy proxies so policies can be evaluated at the edge.
- Tracks policy decisions for audit and observability.

**Data owned:**
- Grants (agent → permission associations)
- Roles (named permission collections)
- Policy versions (history of policy changes)
- OPA bundle metadata

**Key interfaces:**
- `/v1/grants` — create, update, delete grants
- `/v1/roles` — manage roles
- `/v1/policies/evaluate` — evaluate a specific policy decision (used for debugging)
- `/v1/bundles` — serve OPA bundles to Envoy proxies

**First-principles insight:** Policy Service separates the storage of policy (PAP) from the evaluation of policy (PDP). This allows policies to be centrally managed and pushed to the edge where they are evaluated at request time. It also allows policy decisions to be made by Envoy (in-band, low-latency) rather than calling a central service for every request.

---

#### 3. Policy Gateway (port 8084)
**What it does:** Evaluates policies at request time and makes allow/deny decisions. It is the Policy Enforcement Point (PEP) when policies cannot be evaluated at the edge.

**Responsibilities:**
- Loads OPA bundles from Policy Service.
- Evaluates incoming policy queries (usually from Envoy ext_proc handlers).
- Returns allow/deny decisions with reasoning.
- Logs all policy decisions for audit.

**Data owned:** OPA bundles (in-memory, fetched from Policy Service)

**Key interfaces:**
- `/v1/decisions` — evaluate a policy query
- `/metrics` — Prometheus metrics for decision counts and latency

**First-principles insight:** Policy Gateway is a thin layer over OPA. It exists because OPA cannot be embedded directly in Envoy (though there is ongoing work in the Envoy ecosystem to support this). By keeping it separate, kora can eventually remove this component when Envoy gains native OPA support.

---

#### 4. Inference Gateway (port 8081)
**What it does:** Routes and authenticates LLM API calls. It acts as a proxy between agents and LLM providers (OpenAI, Anthropic, Bedrock, etc.).

**Responsibilities:**
- Accepts chat completion requests in OpenAI-compatible format.
- Authenticates requests using the agent's SPIFFE identity.
- Routes requests to the configured LLM provider.
- Tracks token usage and costs per agent and model.
- Enforces rate limits and budget caps.
- Manages per-agent model override policies (e.g., "Agent X must use gpt-4, not gpt-3.5").

**Data owned:**
- Routing configuration (provider, API key, endpoints)
- Usage aggregates (tokens, cost) per agent
- Model pricing and overrides
- Rate limit configurations

**Key interfaces:**
- `POST /v1/chat/completions` — OpenAI-compatible chat endpoint
- `GET /v1/models` — list available models
- `/v1/usage/agents/{agentId}` — query usage for an agent

**First-principles insight:** Inference Gateway is an LLM-specific policy enforcement point. It allows kora to meter and govern LLM usage separately from general API access. It can enforce tenant-specific SLAs, fallback models, and budget caps that are specific to LLMs.

---

#### 5. Kora Agents (Agent Control Plane)
**What it does:** Orchestrates the lifecycle of agent workloads. It is the coordination layer for agent deployment, execution, and monitoring.

**Responsibilities:**
- Manages agent definitions and deployment specs.
- Provisions and schedules agent pods on Kubernetes.
- Tracks agent runs (execution instances of an agent).
- Manages run queues and scheduling.
- Streams events from agent executions.
- Manages agent sessions (ephemeral execution contexts).
- Coordinates with the Operator for CRD management.

**Data owned:**
- Agent definitions (code, config, entrypoints)
- Run records (execution history, status, duration)
- Run queues and scheduling state
- Session state (agent execution context)

**Key interfaces:**
- `/v1/agents` — list and query agents
- `/v1/agents/{id}/runs` — create and manage runs
- `/v1/agents/{id}/runs/{runId}/stream` — WebSocket stream of events
- `/v1/sessions` — manage agent execution sessions

**First-principles insight:** Kora Agents is not the agent framework itself. It does not execute agent code. It orchestrates the infrastructure that runs agents. The actual agent process (Claude Code, a Python script, etc.) runs inside a pod; Kora Agents manages the pod lifecycle.

---

#### 6. Kora Triggers (port 8097)
**What it does:** Manages event-driven execution of agents. It is the trigger engine.

**Responsibilities:**
- Accepts webhooks from external systems (GitHub, Stripe, CI/CD, etc.).
- Stores triggers: mappings from events to agents (e.g., "when a PR is opened, run the code-review agent").
- Manages delivery: ensures every event is delivered to the target agent exactly once.
- Handles retries, exponential backoff, and dead-letter queues.
- Manages cron-scheduled triggers.
- Tracks delivery status and provides delivery logs.

**Data owned:**
- Trigger definitions (event source, target agent, filter rules)
- Deliveries (individual event→agent executions)
- Delivery logs and status
- Dead-letter queue entries

**Key interfaces:**
- `POST /v1/webhooks/{webhookId}` — receive webhook events
- `GET /v1/triggers` — list and manage triggers
- `GET /v1/deliveries` — query delivery status
- `/v1/cron/schedule` — manage cron triggers

**First-principles insight:** Triggers is a durable queue. It guarantees that every event is delivered to the target agent at least once and uses distributed coordination to ensure exactly-once delivery semantics. This is separate from Kora Agents to allow triggers to be managed independently of agent execution.

---

#### 7. Skills Service (port 8098)
**What it does:** Serves agent skills (tools) from GitHub repositories. It acts as a skill marketplace.

**Responsibilities:**
- Indexes skills from GitHub repositories.
- Caches skill definitions and metadata in Redis.
- Serves skill catalogs to agents.
- Manages skill versions and updates.
- Tracks skill usage and popularity.

**Data owned:**
- Skill index (name, version, metadata)
- Cached skill definitions
- Skill usage metrics

**Key interfaces:**
- `/v1/skills` — list available skills
- `/v1/skills/{skillId}` — get skill definition
- `/v1/skills/search` — search for skills

**First-principles insight:** Skills Service decouples skill management from agent execution. An agent doesn't need to know where its tools come from; Skills Service handles discovery and delivery. This allows skills to be published, versioned, and updated independently of agent code.

---

#### 8. Operator (Kubernetes Custom Resource Controller)
**What it does:** Reconciles Kubernetes Custom Resource Definitions (Tenants, Platforms, Agents) to their desired state.

**Responsibilities:**
- Watches Tenant and Platform CRDs.
- Creates and updates agent workloads when CRDs change.
- Injects sidecars (Envoy, Identity Proxy) into agent pods.
- Configures RBAC and network policies.
- Manages TLS certificates and SPIFFE identity.
- Projects kora platform resources into Kubernetes (ConfigMaps, Secrets).

**Data owned:** None (all state stored in Kubernetes etcd via CRDs)

**Key interfaces:** Kubernetes API (no HTTP endpoints)

**First-principles insight:** The Operator is the integration point between kora (the control plane) and Kubernetes (the infrastructure). It allows agent deployments to be declared as Kubernetes resources, which makes them versionable, auditable, and manageable with standard Kubernetes tooling.

---

### Supporting Components

#### Envoy Proxy Sidecars
- **What it does:** Intercepts all traffic from agent pods.
- **Where:** Inside each agent pod as a sidecar container.
- **How it enforces policy:** Calls Policy Gateway for every outbound request.
- **How it injects credentials:** Via ext_proc handler (Identity Proxy).
- **Why it exists:** Policies are enforced at the network layer, not application layer. This is transparent to the agent code.

#### Identity Proxy (ext_proc Handler)
- **What it does:** Manages credential injection for outbound requests.
- **Where:** Runs as part of Envoy's extension architecture.
- **How it works:** 
  1. Intercepts outbound request.
  2. Extracts identity from SPIFFE certificate.
  3. Calls Identity Service to exchange identity for credentials.
  4. Injects credentials into the request.
  5. Forwards request to external system.
- **Why it exists:** Decouples agent identity from external credentials.

#### Session Manager (port 8090)
- **What it does:** Manages per-tenant agent execution environments.
- **Responsibilities:**
  - Runs one instance per tenant (multi-tenant architecture).
  - Manages the subprocess that executes agent code.
  - Handles signal forwarding and I/O redirection.
- **Why it exists:** Isolation and multi-tenancy. Each tenant's agent executes in its own session.

#### SPIFFE Setup
- **What it does:** Manages SPIFFE identity provisioning.
- **Components:**
  - SPIRE Server: Root certificate authority.
  - SPIRE Agents: Run on each Kubernetes node, issue certificates to workloads.
  - SPIFFE Workload API: Provides certificates to workloads at runtime.
- **Why it matters:** SPIFFE identity is infrastructure-level, not application-level. This makes it unforgeable and tamper-proof.

---

## Data Flow and Execution Model

### Scenario: An Agent Calls an External API

Here is how the three core questions are answered in a real execution flow:

```
1. Agent code runs inside a pod:
   agent.py: response = requests.get('https://api.example.com/data')

2. Envoy proxy sidecar intercepts the outgoing connection:
   - Checks SPIFFE certificate (attached to the pod)
   → Question 1 answered: "Who is this agent?" = Agent identity from cert

3. Envoy calls Policy Gateway to evaluate the policy:
   Policy query: "Can agent-xyz call api.example.com?"
   → Question 2 answered: "What can it access?" = Policy decision (allow/deny)

4. If policy allows, Identity Proxy prepares credential injection:
   - Calls Identity Service: "Exchange my SPIFFE identity for credentials to api.example.com"
   - Identity Service checks: "What credentials does agent-xyz have access to?"
   - Returns temporary API key or token
   → Question 3 answered: "How does it connect?" = Temporary credentials injected

5. Envoy injects credentials into the outgoing request:
   - Original: GET /data HTTP/1.1
   - Modified: GET /data HTTP/1.1\nAuthorization: Bearer <temp-token>

6. Request is sent to api.example.com:
   - api.example.com validates the token
   - api.example.com has no idea it came from an agent
   - api.example.com thinks it came from a legitimate user

7. Response comes back:
   - Envoy passes response to agent process
   - Agent code receives response as if it called the API directly

8. Everything is logged:
   - Identity Service logs: "Issued credential for agent-xyz"
   - Policy Service logs: "Allowed agent-xyz to call api.example.com"
   - Envoy logs: "Request from agent-xyz to api.example.com"
```

### Key Insights from This Flow

1. **The agent never sees credentials.** It calls the API normally. Credentials are injected at the network layer.

2. **The agent never knows about policies.** It tries to do something; the policy layer says yes or no. The agent receives success or a network error, the same as if it had no credentials.

3. **Identity is proven at the infrastructure level.** The agent does not prove its identity by sending a token. The pod itself is proven to be the agent via SPIFFE.

4. **The external system (api.example.com) doesn't know it's talking to an agent.** It sees a normal API call with valid credentials.

5. **Everything is auditable.** Every step is logged: who initiated, what policy was evaluated, what credentials were used, what response came back.

---

## Identity and Authentication

### SPIFFE Identity Model

SPIFFE (Secure Production Identity Framework for Everyone) is the mechanism kora uses to prove agent identity.

**How SPIFFE works:**

1. **Root CA:** A SPIRE Server runs in the Kubernetes control plane. It acts as a Root Certificate Authority.

2. **Agent on Each Node:** A SPIRE Agent runs on each Kubernetes node. It has a strong identity (verified by Kubernetes) and can issue certificates to workloads on that node.

3. **Workload (Agent Pod):** When an agent pod starts, SPIRE Agent detects it via Kubernetes API. It verifies:
   - The pod matches the declared SPIFFE identity (via ServiceAccount name, namespace, etc.)
   - The pod is running in a healthy Kubernetes cluster

4. **Certificate Issued:** SPIRE Agent issues an X.509 certificate to the pod with:
   - Subject Name: `spiffe://kora.example.com/ns/prod/sa/agent-xyz`
   - Validity: 1 hour (rotated continuously)
   - Public Key: Generated by the workload (private key never leaves the pod)

5. **Certificate Mounted:** The certificate is mounted at `/var/run/secrets/spiffe/cert.pem`. The private key is mounted at `/var/run/secrets/spiffe/key.pem` but never exposed to user code.

6. **Certificate Used:** Envoy uses this certificate to:
   - Prove identity to Policy Gateway
   - Prove identity to Identity Service
   - Establish mTLS with other services

**Why SPIFFE, not API Keys:**

| Aspect | API Keys | SPIFFE |
|--------|----------|--------|
| **Storage** | Agent code must store key | Key never leaves pod, kernel-managed |
| **Rotation** | Manual, error-prone | Automatic, continuous |
| **Scope** | Static (key has same permissions forever) | Dynamic (permissions can change, certificate unchanged) |
| **Reusability** | Key can be stolen, copied, used elsewhere | Certificate is bound to pod, cannot be reused |
| **Revocation** | Difficult (key still in code) | Instant (new certificate not issued) |
| **Multi-tenancy** | Need separate key per tenant | One pod, identity includes tenant |

**Key insight:** SPIFFE identity proves what the pod **is**, not what it **can do**. The "what it can do" part is handled by policies (Question 2). This separation is critical: identity is permanent; policies are changeable.

---

### User Authentication

For humans accessing kora (via the dashboard or CLI), kora uses **Device Authorization Grant** (RFC 8628), an OAuth2 flow designed for devices and CLIs.

**Flow:**

1. User runs `kora login` or opens the dashboard.
2. kora generates a device code and user code.
3. User is directed to a browser: "Visit https://kora.example.com/auth?code=ABCD1234"
4. User authenticates (OIDC provider, e.g., Okta, Auth0).
5. Browser redirects back to CLI.
6. CLI polls kora for confirmation.
7. kora issues a refresh token and session token.
8. CLI stores tokens in secure storage (macOS Keychain, Linux keyring).

**Why Device Authorization Grant:**
- Works for CLI tools (no local web server needed).
- More secure than entering password directly.
- Supports 2FA and other OIDC features.
- Tokens can be revoked instantly.

---

## Authorization and Policy Enforcement

### The Policy Model

Policies in kora answer: "Can this agent perform this action on this resource?"

**Policy Structure:**

```rego
// OPA Rego policy language
// Stored in Policy Service, embedded in Envoy

allow {
  # Who is the principal?
  input.principal.type == "agent"
  input.principal.id == "agent-xyz"
  
  # What action?
  input.action == "read_file"
  
  # On what resource?
  input.resource.path == "/workspace/src/*"
  
  # Under what conditions?
  input.time < time.now_ns() + 86400000000000  # expires tomorrow
}
```

**Policy Decision Flow:**

1. **Policy Service** stores policies as Rego code.
2. **Policy Service** compiles policies into OPA bundles.
3. **Policy Service** publishes bundles to Envoy proxies.
4. **Envoy** evaluates policies locally (low latency, no network call).
5. **If policy cannot be evaluated locally** (e.g., complex conditions), **Envoy** calls **Policy Gateway**.
6. **Policy Gateway** evaluates policy against OPA and returns decision.
7. **Envoy** enforces the decision: allows or denies the request.

**Why Policies Are Code:**

- **Composable:** Policies can reference other policies.
- **Versionable:** Policies are stored in Git, auditable, rollback-able.
- **Testable:** OPA policies can be unit tested.
- **Searchable:** Grep to find all policies that affect a resource.
- **Expressive:** Complex conditions (time-based, quota-based, etc.) are easy.

---

### Enforcement Points

Kora enforces policies at multiple points:

| Point | What it controls | Example |
|-------|------------------|---------|
| **Agent Pod Admission** | Can this workload be created? | "Only admins can create agents" |
| **API Access** | Can this agent call this API? | "Agent can call /v1/models but not /v1/admin/*" |
| **File Access** | Can this agent read/write this file? | "Agent can read /workspace but not /secret" |
| **Command Execution** | Can this agent run this command? | "Agent can run `python` but not `rm -rf`" |
| **Resource Creation** | Can this agent create this Kubernetes resource? | "Agent cannot create Pods directly" |
| **Credential Access** | Can this agent get credentials for this resource? | "Agent can get database password but not admin password" |

Each enforcement point is controlled by policies stored in Policy Service.

---

## Credential Management

### The Credential Brokering Model

Instead of storing credentials, kora exchanges identities for credentials.

**Flow:**

```
Agent wants to access database.

1. Agent code: db.connect("host=db.example.com")
   (No password provided by agent)

2. Envoy intercepts the connection attempt.

3. Envoy calls Identity Proxy:
   "I have an agent with identity spiffe://kora.example.com/ns/prod/sa/agent-xyz
    It wants to connect to the database.
    What credentials should I inject?"

4. Identity Proxy calls Identity Service:
   "What credentials does this agent have access to?"

5. Identity Service checks:
   - Is the agent authorized to access this database? (Policy check)
   - What credentials are available? (Database passwords, API keys, etc.)
   - Are credentials still valid? (Check with upstream system if needed)

6. Identity Service exchanges SPIFFE identity for database credentials.
   (Inside kora, this might mean calling AWS STS, Vault, or a credential provider)

7. Identity Service returns temporary credentials:
   "Use username=agent_xyz password=temp_pwd_12345 until 14:30 UTC"

8. Identity Proxy injects credentials into the connection:
   db.connect("host=db.example.com user=agent_xyz password=temp_pwd_12345")

9. Agent code receives connection object, unaware credentials were injected.

10. Later, Identity Service revokes the temporary credentials automatically.
    If agent code tries to use the connection after expiry, it gets an auth error.
```

### Credential Providers

Identity Service can fetch credentials from multiple providers:

| Provider | What it manages | Example |
|----------|-----------------|---------|
| **Vault** | Secrets (API keys, passwords, certificates) | Agent gets temporary database password |
| **AWS STS** | AWS credentials (AssumeRole) | Agent gets temporary AWS access |
| **Kubernetes Secrets** | Inline secrets | Agent gets API key from ConfigMap |
| **External API** | Credentials from external system | Agent gets GitHub token via GitHub API |

Each provider has a driver that knows how to exchange identity for credentials.

---

## Agent Lifecycle

### Deployment Phase

```
1. Operator watches for new Agent CRD.

2. Operator creates a Pod:
   - Specifies ServiceAccount (determines SPIFFE identity)
   - Mounts agent code as ConfigMap
   - Specifies resource limits
   - Labels pod with agent ID, version, etc.

3. Kubernetes schedules pod on a node.

4. SPIRE Agent on the node:
   - Detects new pod
   - Verifies pod identity
   - Issues SPIFFE certificate
   - Mounts certificate at /var/run/secrets/spiffe/

5. Envoy sidecar starts:
   - Reads SPIFFE certificate
   - Registers with mTLS proxy mesh (Istio)
   - Loads policy bundles from Policy Service
   - Ready to intercept traffic

6. Agent process starts:
   - Reads environment variables (agent config)
   - Connects to Session Manager (for multi-tenant isolation)
   - Ready to execute

Status: Agent is now running.
```

### Execution Phase

```
1. Trigger fires (webhook, cron, manual invocation).

2. Kora Agents receives execution request:
   - Validates trigger is authorized
   - Creates a Run record (tracks this execution)
   - Queues the run for execution

3. Session Manager or sandbox-exec picks up the run:
   - Allocates resources
   - Sets up execution environment
   - Executes agent code
   - Captures stdout/stderr

4. During execution, agent code calls external systems:
   - Database queries
   - API calls
   - File operations

5. Each call is intercepted by Envoy:
   - Policy check (Question 2: what can it access?)
   - Credential injection (Question 3: how does it connect?)

6. Agent code receives responses:
   - Thinks it called the system normally
   - Unaware of policy checks or credential injection

7. Execution completes:
   - Session Manager captures exit status
   - Logs are streamed back to Kora Agents
   - Run record is updated

Status: Run is complete, result is stored.
```

### Cleanup Phase

```
1. Pod ages out or is manually deleted.

2. Operator detects pod deletion.

3. Operator cleans up:
   - Removes Envoy sidecars
   - Revokes SPIFFE certificate (not renewed)
   - Removes network policies
   - Deletes Secrets/ConfigMaps

4. SPIRE stops issuing certificates to pod (certificate expires, not renewed).

5. Pod terminates gracefully or is killed after grace period.

Status: Agent is deprovisioned.
```

---

## Deployment Model

### Multi-Binary Architecture

kora is deployed as **multiple independent binaries**, not a monolith.

**Why:**

- **Independent scaling:** Policy Gateway can scale separately from Identity Service.
- **Independent deployment:** An update to Skills Service doesn't require redeploying Triggers.
- **Failure isolation:** A crash in Triggers doesn't take down the Identity Service.
- **Clear boundaries:** Each binary has a clear responsibility.
- **Team ownership:** Different teams can own different binaries.

**Binaries:**

| Binary | Port | Responsibility |
|--------|------|-----------------|
| `identity` | 8080 | Root of trust, session management, credential exchange |
| `inference-gateway` | 8081 | LLM routing and metering |
| `policy-service` | 8083 | Policy storage and OPA bundle generation |
| `policy-gateway` | 8084 | Policy enforcement (when not at edge) |
| `kora-agents` | — | Agent orchestration (no public port) |
| `kora-triggers` | 8097 | Webhook ingestion and trigger management |
| `skills-service` | 8098 | Skill discovery and delivery |
| `session-manager` | 8090 | Per-tenant agent execution context |
| `operator` | — | Kubernetes reconciliation (no public port) |
| `migrate` | — | Database schema migration (batch job) |

### Kubernetes Manifests

Agents are declared as Kubernetes Custom Resources:

```yaml
# Agent specification
apiVersion: kora.example.com/v1alpha1
kind: Agent
metadata:
  name: code-review-agent
  namespace: prod
spec:
  image: ghcr.io/example/code-review-agent:v1
  entrypoint: /app/agent.py
  resources:
    cpu: 500m
    memory: 512Mi
  triggers:
    - webhookId: github-pr-opened
  env:
    - name: LOG_LEVEL
      value: info

---
# Trigger specification
apiVersion: kora.example.com/v1alpha1
kind: Trigger
metadata:
  name: github-pr-opened
  namespace: prod
spec:
  source: github
  filter:
    event: pull_request
    action: opened
  target:
    agent: code-review-agent
  retryPolicy:
    maxRetries: 3
    backoff: exponential
```

The Operator watches these resources and reconciles them to Kubernetes Pods.

---

## Key Abstractions

### Principal

A **Principal** is an identity in the system. It can be:

- **Agent:** An autonomous workload (e.g., a Claude Code instance).
- **User:** A human (e.g., a developer or admin).
- **Service:** A non-agent service (e.g., a GitHub Action).

Each principal has:
- A unique ID (UUID)
- A type (agent, user, service)
- Metadata (name, tags, owner)
- A set of permissions (grants and roles)

### Grant

A **Grant** is a permission given to a principal. It has:

- **Principal:** Who is granted the permission (agent, user, or service).
- **Permission:** What they are allowed to do (e.g., `read_file`, `call_api`, `execute_command`).
- **Resource:** What they are allowed to do it to (e.g., `/workspace/*`, `api.example.com`, `*`).
- **Scope:** Scoping conditions (time-based, quota-based, IP-based, etc.).

Example:
```
Grant principal=agent-xyz
      permission=call_api
      resource=https://api.github.com/*
      scope=expires_at:2024-12-31
```

### Role

A **Role** is a named collection of grants. Instead of granting permissions individually, you can assign a principal to a role.

Example:
```
Role: data-analyst
  - Grant: call_api, https://api.example.com/data/*
  - Grant: read_file, /data/*
  - Grant: call_api, https://analytics.example.com/*

Agent agent-xyz assigned to Role data-analyst
→ Agent inherits all grants in the role
```

### Policy

A **Policy** is a Rego rule that evaluates grants and roles to make an allow/deny decision.

Example:
```rego
allow {
  # Check if principal has a relevant grant
  grants[_].principal == input.principal.id
  grants[_].permission == input.action
  grants[_].resource matches input.resource
  
  # Check time-based scope
  input.time < grants[_].scope.expires_at
}

deny {
  # Explicitly deny certain actions
  input.action in ["delete_all", "drop_table"]
}
```

---

## Development Philosophy

### First Principles

1. **Security First:** Every component assumes potential attackers. Defense in depth is standard, not optional.

2. **Identity-Driven:** All decisions are tied to verified identity. You can never ask "what did the agent do?" and get "I don't know" as an answer.

3. **Policy as Code:** Policies are version-controlled, testable, and auditable. No magic configuration files.

4. **Clear Boundaries:** Each service has a clear responsibility. Clear boundaries make it easy to reason about and test.

5. **No Hidden Magic:** If something looks like it's being done for you, understand how before relying on it. Magic hides bugs.

6. **Observability Built-in:** Every component emits detailed logs and metrics. Debugging is not a surprise; it is the default.

### Architecture Principles

1. **Feature Independence:** Features do not import each other. They communicate through the platform layer (`internal/platform/`).

2. **Repository Pattern:** Data access is abstracted behind interfaces. The Postgres implementation is one implementation; others could exist.

3. **Multi-Binary Deployment:** Each service is an independent binary. Services can be scaled and deployed independently.

4. **Standards-Based:** Use Open standards (SPIFFE, OPA, OpenTelemetry) rather than proprietary solutions. This avoids lock-in.

5. **Kubernetes-Native:** Leverage Kubernetes (CRDs, admission webhooks, RBAC) rather than duplicating it. This makes kora manageable with standard tools.

### Code Organization

```
backend/
├── cmd/                          # Each service is a binary
│   ├── identity/                 # Root of trust
│   │   ├── main.go
│   │   ├── config/
│   │   └── migrations/
│   ├── policy-service/
│   ├── inference-gateway/
│   ├── kora-agents/
│   └── ...
│
├── internal/
│   ├── platform/                 # Shared infrastructure (all services can use)
│   │   ├── observability/        # Logging, tracing, metrics
│   │   ├── storage/              # Database and Redis clients
│   │   ├── config/               # Configuration loading
│   │   ├── clients/              # Service-to-service HTTP clients
│   │   ├── identity/             # SPIFFE identity handling
│   │   └── ...
│   │
│   └── {feature}/                # Feature packages (domain logic)
│       ├── repository.go         # Storage interface
│       ├── postgres.go           # PostgreSQL implementation
│       ├── service.go            # Business logic
│       ├── handlers.go           # HTTP handlers
│       └── models.go             # Domain models
│
└── tests/                        # Integration tests
```

### Testing Strategy

**Unit Tests:** Table-driven, fast, no database. These run in `make check`.

**Postgres Tests:** Test SQL logic explicitly (windows, locking, atomicity). Gated on `KORA_TEST_DB_URL`. These do NOT run in CI.

**Integration Tests:** Full cluster, gated on build tag `integration`. These do NOT run in CI.

**CI Pipeline:** Runs unit tests only (fast feedback). Postgres and integration tests run locally before pushing.

---

## Conclusion

kora answers three core questions for every action an agent takes:

1. **Who is this agent?** Identity proven via SPIFFE certificates.
2. **What can it access?** Policy checked before every action.
3. **How does it connect?** Credentials injected transparently, never exposed to the agent.

The architecture is built on standards (SPIFFE, OPA, OpenTelemetry, Kubernetes) and principles (security first, policy as code, clear boundaries). Every component has a clear responsibility. Every action is auditable. Every policy is versionable.

This makes agents safe for production: governed, accountable, and observable.
