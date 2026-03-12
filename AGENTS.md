# Orbit Development Guide for AI Agents

## Project Overview

Orbit is the **control plane for AI agents** — not a framework, not a model router, not an LLM wrapper. It provides the coordination, identity, and control infrastructure required to safely scale autonomous agents inside real systems.

**Core Metaphor**: Orbit is architected as a **nervous system** for autonomous agents. This metaphor guides all architectural decisions:
- **Identity & Auth** = Sensory input (who is acting?)
- **Policy Enforcement** = Reflex control (what's allowed?)
- **Credential Broker** = Motor function (how action is executed)
- **Event Spine** = Neural signaling (awareness & feedback)
- **Autonomy Levels** = Brain stem vs cortex (decision hierarchy)

**Development Status**: Early development. Core platform components are actively being built.

## Product Development Workflow

Orbit follows a structured process for new features and significant changes:

### 1. Product Requirements Document (PRD)
When working on a new feature or significant change:
- Create a PRD in `docs/prd/` following the template in `docs/prd/template.md`
- PRDs define the problem, user needs, and high-level solution approach
- Collaborate with the human to refine requirements and scope

### 2. Architecture Decision Record (ADR)
After PRD approval, create an ADR in `docs/adr/`:
- ADRs document the technical implementation approach
- Work with the human to explore options and trade-offs
- Use the ADR template: Context → Options → Decision → Consequences
- ADRs capture the "why" behind architectural choices

### 3. Implementation
Only after PRD and ADR approval:
- Implement the feature following the documented approach
- Reference the ADR and PRD in commit messages and PR descriptions
- Update documentation as needed

**The nervous system metaphor is central to Orbit's design.** All architectural decisions should align with this principle: coordination over computation, signaling over storage, governance over control.

## Development Setup

**Language**: Go (control plane), potentially Rust for performance-critical paths

**Status**: Codebase is in early development. Setup instructions will be added as components are built.

## Code Organization

Orbit follows a **feature-based architecture** with the repository pattern for data access. See the following documents for complete details:

- **[docs/code-organization.md](docs/code-organization.md)** - Complete directory structure and package organization
- **[docs/adr/0003-repository-pattern-and-code-organization.md](docs/adr/0003-repository-pattern-and-code-organization.md)** - Architectural decisions and rationale

### Key Principles

**Feature Ownership**: Each feature package (`internal/identity/`, `internal/agents/`, etc.) owns its complete stack:
- Repository interface (storage contract)
- PostgreSQL implementation
- Business logic (service layer)
- HTTP handlers
- Feature-specific types

**Storage Infrastructure**: The `internal/storage/` package provides database infrastructure (connections, transactions, migrations) but never imports feature packages. Dependency flow is always: Features → Storage.

**"DDD-lite" Approach**: We use valuable Domain-Driven Design concepts (bounded contexts, repository pattern, ubiquitous language) while staying idiomatic to Go. We skip strict DDD ceremony (deep layering, aggregate roots, value objects) in favor of flat packages and explicit code.

**Standard Feature Structure**:
```
internal/{feature}/
├── repository.go    # Storage interface
├── postgres.go      # PostgreSQL implementation
├── service.go       # Business logic
├── handlers.go      # HTTP transport
└── models.go        # Feature-specific types
```

This structure enables feature teams to work independently while maintaining clear separation of concerns.

## Design Principles

When contributing code or architecture:

1. **Security First**: Every component designed with security as primary concern
2. **Identity-Driven**: All actions tied to verified identities
3. **Policy-Governed**: Policies enforced *before* execution, not after
4. **Observable**: Complete visibility into agent behavior
5. **Extensible**: Pluggable integrations for models, tools, identity providers
6. **Production-Grade**: Built for enterprise reliability and scale
7. **Open Standards**: OPA for policy, OIDC/SAML for identity, MCP for tools

## Security Guidelines

Orbit is infrastructure for autonomous agents. Security is not optional.

- **No secrets in code**: Use environment variables or secret management
- **Validate all inputs**: Especially from external agents or tools
- **Principle of least privilege**: Grant minimum necessary permissions
- **Audit everything**: All actions must emit events to the event spine
- **Identity verification**: Never trust, always verify
- **Policy-first**: Don't implement authorization logic in business code

See `SECURITY.md` for vulnerability reporting.

## Contributing Workflow

### For New Features or Significant Changes:

1. **Create PRD**: Start with a Product Requirements Document in `docs/prd/` using the template
   - Define the problem, goals, and user stories
   - Collaborate with the human to refine requirements
   - Get PRD approval before proceeding

2. **Create ADR**: After PRD approval, create an Architecture Decision Record in `docs/adr/`
   - Document the technical approach and trade-offs
   - Work with the human to explore implementation options
   - Use the ADR template to capture decisions and rationale

3. **Implement**: Only after PRD and ADR approval
   - Write tests for all new code
   - Follow Go standards: `gofmt`, `golint`, idiomatic Go
   - Reference PRD and ADR in commit messages and PR descriptions
   - Request security review for security-critical changes

### For Bug Fixes or Minor Changes:

1. **Document**: Create an issue describing the problem
2. **Implement**: Fix with tests and documentation updates
3. **Review**: Submit PR with clear description

## Testing Strategy

- **Unit tests**: Test individual components in isolation
- **Integration tests**: Test component interactions
- **E2E tests**: Test full agent workflows through Orbit
- **Security tests**: Validate policy enforcement and credential isolation
- **Performance tests**: Ensure scalability of control plane

## Implementation Principles

When implementing Orbit components:

- **Policy Evaluation**: Always evaluate policies before executing actions, not after
- **Event Emission**: Emit structured events for all significant actions and decisions
- **Credential Brokering**: Never expose raw credentials to agents - use scoped, temporary access
- **Identity Context**: Carry identity context through the entire execution chain
- **Repository Pattern**: Define repository interfaces in feature packages, implement with PostgreSQL in the same package
- **Feature Independence**: Features should not import each other - share only through well-defined contracts
- **Idiomatic Go**: Favor simplicity, flat packages, and explicit code over deep abstraction hierarchies

See [ADR-0003](docs/adr/0003-repository-pattern-and-code-organization.md) for detailed architectural patterns and the rationale behind our code organization approach.

## Frontend Rules

- **`server-only` guard**: Every module under `frontend/src/server/` must have `import "server-only"` after its JSDoc block, before any other imports.
- **BFF boundary**: Only `src/app/api/` route handlers may import from `@/server/`. Pages and components must fetch via `/api/*`.

## Templates and Documentation

### PRD Template
When creating a new feature PRD, use `docs/prd/template.md`. The template includes:
- Problem statement and user pain points
- Goals, success metrics, and non-goals
- User stories with acceptance criteria
- Proposed solution and key components
- Requirements (functional and non-functional)
- Dependencies, risks, and timeline

### ADR Template
When creating an ADR, use `docs/adr/template.md`. The template includes:
- Context and problem statement
- Decision drivers and considered options
- Decision rationale and consequences
- Implementation notes and validation criteria
- Related decisions and references

Both templates are designed to facilitate collaboration between agents and humans in the product development process.

## Current Focus Areas

As Orbit is in early development, current priorities are:

1. **Identity layer**: OIDC/SAML integration, agent identity
2. **Policy engine**: OPA integration, RBAC/PBAC enforcement
3. **Event spine**: Structured event emission and routing
4. **Credential broker**: Secure secret management integration

Check open issues and project boards for specific tasks.

## Getting Help

- **Architecture questions**: Open a discussion or issue
- **Security concerns**: See `SECURITY.md` for private reporting
- **General questions**: Use GitHub Discussions

## License

Orbit is licensed under Apache License 2.0. All contributions must be compatible with this license.

---

**Remember**: Orbit is not just infrastructure — it's the nervous system that coordinates autonomous agents. Every component should reflect this: coordination over computation, signaling over storage, governance over control.