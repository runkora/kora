# Declarative Resources - Product Requirements Document

**Status:** Implemented  
**Created:** 2026-10-24  
**Last Updated:** 2026-10-26  
**Owner:** Justin Wilkin  
**Stakeholders:** Platform teams, tenant administrators, agent developers

---

## Executive Summary

Platform teams and tenant administrators define agents, environments, triggers, trigger sources, roles and models as declarative resources in a configuration repository. A whole tenant's agent setup can be reviewed in pull requests, promoted from a dev tenant to a prod tenant, and rebuilt from the repository. The dashboard and API keep working for organizations that do not use a repository, and an organization can switch them off per tenant.

---

## Problem Statement

### Current State

Providers, MCP servers, connections, sandbox images and tenants are declarative resources. Agents, environments, triggers, trigger sources, roles and models exist only in kora's database. They are created and changed through the dashboard, the API or the kora MCP server.

### User Pain Points

- An agent setup cannot be reviewed in a pull request before it goes live.
- A setup built in a dev tenant has to be recreated by hand in prod.
- A prod tenant cannot be rebuilt from a known definition after a loss or a mistake.
- Changes made through the dashboard leave no reviewable history beside the rest of the platform configuration.
- A platform team that manages everything else through its repository has no way to stop ad hoc changes to agents in prod.
- Deleting an agent deletes its whole run history.

### Business Impact

- Agent setups drift between environments and between the dashboard and the platform team's repository.
- Regulated customers cannot show that every agent change passed review.

---

## Goals and Objectives

### Primary Goals

1. An organization defines its whole agent setup in its configuration repository: agents, environments, triggers, trigger sources, roles and models.
2. The same definitions apply to a dev tenant and a prod tenant.
3. The dashboard, API and kora MCP server keep creating and editing resources for organizations that do not use a repository.
4. A platform team can switch off dashboard and API changes for a tenant.
5. Run history survives when an agent is deleted.

### Success Metrics

None at this time.

### Non-Goals

- Exporting a dashboard-built resource as a declarative resource.
- Taking over a dashboard-built resource with a declarative resource of the same name.
- Grants for individual users as declarative resources.
- Declarative model prices and per-model provider toggles.
- Tenants defining their own models.

---

## User Stories

### Primary User Stories

**As a** tenant administrator,  
**I want to** define my agents, their environments, their triggers and the trigger sources in our configuration repository,  
**So that** agent changes go through the same review as the rest of our platform.

**Acceptance Criteria:**
- [ ] Each agent, environment, trigger and trigger source is one declarative resource in the tenant's namespace.
- [ ] A resource refers to other resources by name.
- [ ] Applying a resource creates or updates the matching agent, environment, trigger or trigger source within seconds.
- [ ] Deleting a resource deletes the matching item.
- [ ] A trigger source refers to a secret for its signing secret. The resource never holds the secret.
- [ ] Resources can be applied in any order. A resource whose references do not exist yet reports it in its status and becomes active once they exist.
- [ ] A declarative resource can refer only to other declarative resources and to platform models.

---

**As a** platform team member,  
**I want to** define the models every tenant can use in our configuration repository,  
**So that** the model catalog is reviewed and identical across environments.

**Acceptance Criteria:**
- [ ] Each model and its routes to providers is one declarative resource in the platform's namespace.
- [ ] Agents in any tenant can refer to a declared model by name.

---

**As a** tenant administrator,  
**I want to** define roles and their budgets in our configuration repository,  
**So that** who can do what in the tenant is reviewed like any other change.

**Acceptance Criteria:**
- [ ] Each role is one declarative resource in the tenant's namespace with its scopes and an optional budget.
- [ ] The tenant's overall budget is set on the tenant resource.
- [ ] Users receive a declared role through their identity provider as they do today.
- [ ] A role in a tenant's namespace cannot hold platform-wide scopes. The platform team declares those roles in the platform's namespace for a named tenant.

---

**As a** platform team member,  
**I want to** lock a tenant to the repository,  
**So that** our prod tenant changes only through our repository while our dev tenant stays open for experiments.

**Acceptance Criteria:**
- [ ] The switch is set per tenant on the tenant resource.
- [ ] With the switch off, the dashboard, API and kora MCP server cannot create, edit or delete any agent, environment, trigger, trigger source or role in that tenant.
- [ ] With the switch off, resources created earlier through the dashboard stay in place and become read-only.
- [ ] A separate platform-wide switch turns off dashboard and API changes to models.

---

**As a** dashboard user,  
**I want to** see which resources are managed in the repository,  
**So that** I know where to make a change.

**Acceptance Criteria:**
- [ ] A declared resource shows as managed in git in the dashboard.
- [ ] The dashboard, API and kora MCP server cannot edit or delete a declared resource.
- [ ] A declared resource whose name matches a dashboard-built resource is refused and reports the conflict in its status.

---

**As an** agent operator,  
**I want** an agent's runs to stay after the agent is deleted,  
**So that** I can still audit what it did.

**Acceptance Criteria:**
- [ ] Deleting an agent from the dashboard, the API or the repository keeps its runs, sessions and events.
- [ ] A new agent with the name of a deleted one starts with no runs.
- [ ] Deleting a trigger source keeps its triggers. They stop firing and report the missing source.

---

## Proposed Solution

### High-Level Approach

Six new kinds of declarative resource join the existing ones. Models live in the platform's namespace, and the other five kinds live in each tenant's namespace. kora reads each resource as it is applied and creates the same agent, trigger or role the dashboard would create. A declared item can change only through the repository.

### User Experience

#### Platform team flow

1. Commit model resources to the platform's namespace.
2. Add a tenant resource per environment and switch off dashboard and API changes on the prod tenant.

#### Tenant administrator flow

1. Commit environments, agents, trigger sources, triggers and roles to the dev tenant's namespace.
2. Read each resource's status to see whether it is ready.
3. Promote the same files to the prod tenant's namespace.

#### Dashboard user flow

In a tenant with the switch on, only declared items are read-only. In a tenant with the switch off, every agent, environment, trigger, trigger source and role is read-only.

---

## Scope

### In Scope

- Every capability in the user stories above.
- A copy-paste example per kind in the documentation.

### Out of Scope

- Everything listed under Non-Goals.

### Future Considerations

- Export of a dashboard-built item as a resource, to move it into a repository.

---

## Requirements

### Functional Requirements

**Must Have (P0):**
- [ ] Declared items behave at run time the same as dashboard-built items.

**Should Have (P1):**

None at this time.

**Nice to Have (P2):**
- [ ] Documentation example per kind.

### Non-Functional Requirements

**Security:**
- Secrets are never written into a resource. A resource refers to a secret by name.
- The repository and the team that runs it decide which declared agents exist and what they can access.

**Reliability:**
- A broken resource does not stop the others from applying.

---

## Dependencies

### Internal Dependencies

- Tenant resource: gains the switch and the tenant budget.
- Existing resources: environments refer to sandbox images and connections, and agents refer to connections and MCP servers.

### External Dependencies

- A deployment tool such as Argo CD or Flux.
- A secret pipeline for trigger source signing secrets.

---

## Risks and Mitigations

None at this time.

---

## Alternatives Considered

### Alternative 1: Declarative resources replace the dashboard

**Description:** Resources become the only way to define agents, and the dashboard becomes read-only.

**Why not chosen:**
- Organizations that do not run a repository need the dashboard and API.

### Alternative 2: The dashboard edits declared items

**Description:** The dashboard edits a declared item, and the repository's next sync overwrites the edit.

**Why not chosen:**
- An edit disappears without warning on the next sync.

---

## Open Questions

None at this time.

---

## Timeline and Milestones

### Phases

None at this time.

### Key Milestones

- [x] PRD Approved
- [x] ADR Accepted
- [x] Implementation Started
- [ ] Launch

---

## Appendix

### Related Documents

- [CRD-Driven Inference Providers](crd-inference-providers.md)
- [Sandbox Environments](sandbox-environments.md)
- [Agent Webhook Triggers](agent-webhook-triggers.md)
- [Install and Distribution](install-and-distribution.md)

### Glossary

- **Declarative resource:** an object applied to the cluster, reviewed and versioned in the organization's configuration repository.
- **Declared item:** an item kora created from a declarative resource.
- **Dashboard-built item:** an item created without a declarative resource.
- **Tenant namespace:** the cluster namespace that holds one tenant's resources.
