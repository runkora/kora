# Orbit Architecture

## Overview

**Orbit: The Nervous System for Scalable AI Agents**

Orbit provides the coordination, identity, and control infrastructure required to safely scale autonomous agents inside real systems. Just as a biological nervous system coordinates complex organisms, Orbit coordinates autonomous agents — ensuring every decision is contextual, every action is authorized, every interaction is observable, and every agent is accountable.

**Orbit is not a framework. It is not a model router. It is not a wrapper around LLM APIs.**

Orbit is the control plane for agents.

## Core Problem

Agents don't fail because they're unintelligent.  
They fail because they lack **coordination, identity, and control**.

Agents can think. They can act. They can decide.  
But without coordination, they are isolated reflexes.

- Without **identity**, they are unsafe
- Without **policy**, they are unpredictable  
- Without **signaling**, they are blind

Most systems today:
- Proxy LLM calls
- Stitch together tools
- Log outputs

They do **not**:
- Enforce identity
- Govern autonomy
- Control tool permissions
- Integrate with enterprise IAM
- Track decisions at runtime
- Provide agent lifecycle management

**Orbit does.**

## The Nervous System Metaphor

Orbit is architected as a nervous system for autonomous agents:

| Component | Nervous System Function | Purpose |
|-----------|------------------------|---------|
| **Identity & Auth** | Sensory input | Who is acting? What context? |
| **Policy Enforcement** | Reflex control | What's allowed? What requires approval? |
| **Credential Broker** | Motor function | How is action executed? With what permissions? |
| **Event Spine** | Neural signaling | Awareness, feedback, learning |
| **Autonomy Levels** | Brain stem vs cortex | Which decisions are automatic vs deliberate? |
| **Observability** | Feedback loop | Learning and adjustment from outcomes |

This metaphor guides our architectural decisions: coordination over computation, signaling over storage, governance over control.

## What Orbit Actually Is

Orbit is a runtime platform that:
- Authenticates humans and agents
- Enforces policy before actions execute
- Brokers credentials securely
- Orchestrates agent runs
- Routes model and tool calls
- Emits structured events
- Tracks cost, autonomy, and risk
- Integrates with enterprise identity (Entra, SSO, etc.)
- Provides a programmable control plane

### Positioning

Think of Orbit as:
- **Kubernetes** — but for agent execution
- **Temporal** — but for agent cognition
- **Envoy** — but identity-aware

Orbit becomes the layer agents run **on**, not just **through**.

Scaling intelligence requires coordination. Orbit makes coordination possible.

## Core Components

### 1. Identity & Authentication Layer

**Purpose**: Establish identity for both humans and agents

**Capabilities**:
- Human authentication via enterprise SSO (OIDC, SAML)
- Agent identity and registration
- Service account management
- Token issuance and validation
- Integration with existing identity providers (Entra, Okta, etc.)

**Key Concepts**:
- Every actor (human or agent) has a verified identity
- Agents are first-class identity principals
- Identity flows through the entire execution chain

### 2. Policy Enforcement Layer

**Purpose**: Govern what agents can do before they do it

**Capabilities**:
- Pre-execution policy evaluation
- OPA-compatible policy engine
- RBAC (Role-Based Access Control)
- PBAC (Policy-Based Access Control)
- Autonomy level enforcement
- Approval workflow integration
- Dynamic policy updates

**Key Concepts**:
- Policies are evaluated **before** actions execute
- Policies can deny, allow, or require human approval
- Policies are tenant-aware and context-sensitive

### 3. Credential Broker

**Purpose**: Securely provide agents with credentials without direct access

**Capabilities**:
- Just-in-time credential provisioning
- Scoped credential access
- Credential rotation
- Secrets management integration (Vault, cloud KMS)
- Audit trail of credential usage

**Key Concepts**:
- Agents never see raw secrets
- Credentials are scoped to specific operations
- All credential access is logged and auditable

### 4. Agent Run Control Plane

**Purpose**: Manage the lifecycle of agent execution

**Capabilities**:
- Agent run orchestration
- Multi-agent workflow coordination
- Execution state management
- Run scheduling and queueing
- Resource allocation
- Cost tracking per run

**Key Concepts**:
- Agents are deployed as workloads
- Runs are scheduled and managed centrally
- Each run has complete audit trail

### 5. Model Gateway

**Purpose**: Route and manage model inference calls

**Capabilities**:
- Model routing (pluggable providers)
- Request/response transformation
- Rate limiting and quotas
- Cost attribution
- Model fallback and retry
- Caching layer

**Key Concepts**:
- Model calls go through Orbit
- All inference is tracked and attributed
- Policies can control model access

### 6. Tool/MCP Gateway

**Purpose**: Broker tool and external system access

**Capabilities**:
- Tool registration and discovery
- MCP (Model Context Protocol) integration
- Tool permission enforcement
- Request validation
- Response sanitization
- Tool usage tracking

**Key Concepts**:
- Tools are policy-controlled resources
- Agents request tool access, Orbit enforces
- All tool invocations are auditable

### 7. Event Spine

**Purpose**: Emit structured events for observability and audit

**Capabilities**:
- Structured event emission
- Event filtering and routing
- Integration with observability platforms
- Audit log generation
- Real-time event streaming
- Event replay for debugging

**Key Concepts**:
- Every decision is an event
- Events are structured and queryable
- Complete audit trail of agent behavior

## Architecture Diagram

```
┌─────────────────────────────────────────────────────────────┐
│                         Orbit Control Plane                  │
├─────────────────────────────────────────────────────────────┤
│                                                               │
│  ┌──────────────┐      ┌──────────────┐   ┌──────────────┐ │
│  │   Identity   │      │    Policy    │   │  Credential  │ │
│  │     & Auth   │◄────►│ Enforcement  │◄─►│    Broker    │ │
│  └──────────────┘      └──────────────┘   └──────────────┘ │
│         │                      │                    │        │
│         ▼                      ▼                    ▼        │
│  ┌────────────────────────────────────────────────────────┐ │
│  │           Agent Run Control Plane                      │ │
│  │  • Run Orchestration  • State Management  • Scheduling │ │
│  └────────────────────────────────────────────────────────┘ │
│         │                      │                    │        │
│         ▼                      ▼                    ▼        │
│  ┌──────────────┐      ┌──────────────┐   ┌──────────────┐ │
│  │    Model     │      │  Tool/MCP    │   │    Event     │ │
│  │   Gateway    │      │   Gateway    │   │    Spine     │ │
│  └──────────────┘      └──────────────┘   └──────────────┘ │
│         │                      │                    │        │
└─────────┼──────────────────────┼────────────────────┼────────┘
          │                      │                    │
          ▼                      ▼                    ▼
    ┌─────────┐          ┌─────────────┐      ┌──────────────┐
    │  Model  │          │   Tools &   │      │ Observability│
    │Providers│          │   Systems   │      │  Platforms   │
    └─────────┘          └─────────────┘      └──────────────┘
```

## What Orbit Enables

With Orbit, organizations can:
- Deploy agents as first-class system workloads
- Define autonomy levels (fully autonomous, human-in-loop, approval-only)
- Attach approval workflows to sensitive operations
- Enforce RBAC and PBAC dynamically
- Run multi-agent workflows safely
- Attribute cost and usage per tenant or agent
- Maintain complete audit trails for every decision
- Integrate agents with existing enterprise systems securely

## Design Principles

1. **Security First**: Every component designed with security as primary concern
2. **Identity-Driven**: All actions tied to verified identities
3. **Policy-Governed**: Policies enforced before execution, not after
4. **Observable**: Complete visibility into agent behavior
5. **Extensible**: Pluggable integrations for models, tools, identity providers
6. **Production-Grade**: Built for enterprise reliability and scale
7. **Open Standards**: OPA for policy, OIDC/SAML for identity, MCP for tools

## Technical Stack (Planned)

- **Language**: Go (control plane), potentially Rust for critical paths
- **Policy Engine**: Open Policy Agent (OPA)
- **Identity**: OIDC/SAML integration, custom token service
- **Storage**: Pluggable (PostgreSQL, etc.)
- **Events**: Structured event streaming
- **Deployment**: Kubernetes-native, but also standalone

## Development Status

Orbit is in **early development**. This architecture document represents the vision and design direction. Implementation is ongoing.

## Contributing

See [CONTRIBUTING.md](../CONTRIBUTING.md) for guidelines on contributing to Orbit's architecture and implementation.

## License

Orbit is licensed under the Apache License 2.0. See [LICENSE](../LICENSE) for details.