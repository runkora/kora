# CRD-Driven Inference Providers - Product Requirements Document

**Status:** Proposed  
**Created:** 2026-10-01  
**Last Updated:** 2026-10-01  
**Owner:** Justin Wilkin  
**Stakeholders:** Platform administrators, evaluators, tenant administrators

---

## Executive Summary

Platform administrators register model providers with one declarative resource, the same way they register tenants, connections and MCP servers. Any provider Orbit supports can be added, changed or removed from the deployment's configuration repository, and the change takes effect without a restart. An evaluator can bring an OpenRouter key and have models flowing in minutes.

---

## Problem Statement

### Current State

A provider is configured through settings baked into the model gateway when it starts. AWS Bedrock is the only provider with a documented path. Its accounts and regions come from install values or a local settings file. Its credentials come from the gateway's cloud identity or from a shared secret that every Orbit service receives. Vertex AI and Azure have undocumented settings. There is no way to use the Anthropic API, OpenAI or an OpenAI-compatible service such as OpenRouter.

### User Pain Points

- Adding, changing or removing a provider means editing install settings and restarting the model gateway.
- Refreshing short-lived cloud credentials restarts the model gateway.
- An evaluator with an OpenRouter or OpenAI key cannot use it at all.
- Provider credentials reach every Orbit service, not only the one that calls the provider.
- Nobody can see which providers are configured or whether they work, short of reading logs.

### Business Impact

- Evaluations stall on AWS setup when the evaluator already has a working key elsewhere.
- Every provider change is a deployment event, which slows platform teams and invites drift.
- Wide credential distribution weakens Orbit's core promise that credentials stay with the component that needs them.

---

## Goals and Objectives

### Primary Goals

1. A platform administrator registers a provider with one resource and, where needed, one secret. No code change and no restart.
2. Every provider type Orbit supports is registrable: AWS Bedrock, Google Vertex AI, Azure AI, the Anthropic API, and any OpenAI-compatible service including OpenAI and OpenRouter.
3. Credentials stay with the model gateway. Agents and tenants never see them.
4. Administrators see each provider, its status and its models in the dashboard and the API.
5. Existing Bedrock installs keep working through the upgrade with no changes to their models.

### Success Metrics

- An evaluator goes from an OpenRouter key to a working chat completion in under ten minutes.
- A provider added, changed or removed is live within one minute, with no restart.
- A rotated credential is live within the configured refresh period, with no restart.
- Zero provider credentials in any agent environment, tenant namespace or API response.
- Existing Bedrock installs upgrade with no model, route or grant changes.

### Non-Goals

- Tenants bringing their own providers.
- Declaring models, routing or prices as resources. These stay in the catalog.
- A dashboard form to create or edit providers.

---

## User Stories

### Primary User Stories

**As a** platform administrator,  
**I want to** register a provider by committing a resource to our configuration repository,  
**So that** providers are reviewed, versioned and applied like the rest of our platform.

**Acceptance Criteria:**
- [ ] One resource registers a provider of any supported type.
- [ ] The resource names a secret for its credential. It never holds the credential itself.
- [ ] Cloud providers can use the gateway's cloud identity instead of a secret.
- [ ] The provider's models appear in the catalog within a minute, with no restart.
- [ ] Deleting the resource removes the provider within a minute.

---

**As an** evaluator running Orbit on my laptop or in my own cluster,  
**I want to** register OpenRouter with my API key,  
**So that** I can try Orbit with models I already pay for.

**Acceptance Criteria:**
- [ ] I create a secret with my key and one resource naming OpenRouter's address and that secret.
- [ ] No code change, no install setting and no restart.
- [ ] I choose which OpenRouter models to offer.
- [ ] My agents use those models through Orbit, governed by grants like any other model.

---

**As a** platform administrator,  
**I want to** see every provider, where it came from and whether it works,  
**So that** I can fix a broken provider before users notice.

**Acceptance Criteria:**
- [ ] The dashboard lists each provider with its type, endpoint or region, credential mode, source, status, model count and last error.
- [ ] The same list is available through the API and the Orbit MCP server.
- [ ] A resource that cannot be used reports why on the resource itself.
- [ ] A resource with missing required settings is rejected when applied.

---

**As a** platform administrator on an existing Bedrock install,  
**I want to** upgrade without touching my models, routes or grants,  
**So that** the move to resources is not an outage.

**Acceptance Criteria:**
- [ ] After upgrade, the same Bedrock providers serve the same models under the same names.
- [ ] The old settings keep working for one release and warn that they are deprecated.
- [ ] When a resource and an old setting name the same provider, the resource wins.

### Secondary User Stories

**As a** platform administrator with several AWS accounts,  
**I want** the gateway's one cloud identity to reach each account,  
**So that** I do not manage static keys per account.

**As an** agent developer,  
**I want** nothing to change,  
**So that** my agents keep calling models through the gateway as before.

---

## Proposed Solution

### High-Level Approach

Providers become declarative resources owned by the platform. Each resource states the provider type, where it lives, how the gateway authenticates, and optionally which models to offer. The platform picks up changes as they are applied. The model catalog, routing, prices and grants work as they do today.

### User Experience

#### Evaluator flow: OpenRouter

1. Create a secret holding the OpenRouter key.
2. Apply a provider resource: type OpenAI-compatible, OpenRouter's address, the secret, and the models to offer.
3. Open the Catalog page. The OpenRouter provider shows as ready with its models.
4. Create a model in the catalog routed to an OpenRouter model, or use the Orbit MCP server to do it.
5. Grant it and call it through the gateway.

#### Platform administrator flow: Bedrock

1. Commit a provider resource per AWS account: region, and optionally a role to assume.
2. The gateway uses its cloud identity. No secret is needed.
3. The Catalog page shows each account as a provider with its models.

#### What agents experience

Nothing changes. Agents call models through the gateway with their Orbit identity. They never see a provider, a region or a credential.

### Key Components

- Provider resource: declares one provider.
- Provider list: dashboard section, API and MCP tool showing each provider and its health.
- Resource status: reports whether the platform accepted the resource and why not.

---

## Scope

### In Scope

- Provider resources for AWS Bedrock, Google Vertex AI, Azure AI, the Anthropic API and OpenAI-compatible services.
- Secret-based credentials for every type, cloud identity for Bedrock and Vertex AI, and role assumption for Bedrock.
- Live pickup of provider changes and credential rotation.
- A read-only provider list in the dashboard, API and MCP server.
- Migration of existing Bedrock installs and a one-release window for the old settings.
- A copy-paste example per provider type in the documentation.

### Out of Scope

- Tenant-owned providers.
- Models, routing and prices as resources.
- Creating providers from the dashboard.
- Azure cloud identity.

### Future Considerations

- Tenant-owned providers.
- Price sources per provider, such as OpenRouter's published prices.
- Azure cloud identity.

---

## Requirements

### Functional Requirements

**Must Have (P0):**
- [ ] One resource type registers every supported provider type.
- [ ] Credentials come from a named secret or, where supported, cloud identity.
- [ ] Changes, deletions and credential rotation apply without a restart.
- [ ] Invalid resources are rejected on apply or report the reason in their status.
- [ ] Existing Bedrock installs upgrade with no model, route or grant change.
- [ ] Agents are unaffected.

**Should Have (P1):**
- [ ] Dashboard, API and MCP provider list with status.
- [ ] Optional list of models to offer per provider.
- [ ] Role assumption for Bedrock.

**Nice to Have (P2):**
- [ ] Documentation example per provider type.

### Non-Functional Requirements

**Performance:**
- A provider change is live within one minute.
- Reloading providers does not interrupt requests in flight.

**Security:**
- Credentials are never written into the resource, the API, logs or the dashboard.
- Only the model gateway holds provider credentials.
- Only platform administrators can register providers.

**Reliability:**
- A broken provider does not stop the others from loading.
- An unreadable configuration keeps the last working set.

**Observability:**
- Each provider reports its status, model count and last error.
- The old settings log a deprecation warning.

---

## Dependencies

### Internal Dependencies

- Orbit operator: already reconciles tenants, connections and MCP servers.
- Model catalog: unchanged; routes reference providers by name.

### External Dependencies

- Provider accounts and keys, supplied by the customer.
- A secret pipeline in the customer's deployment for provider keys.

---

## Risks and Mitigations

| Risk | Impact | Probability | Mitigation |
|------|--------|-------------|------------|
| Renaming a provider resource breaks routes that name it | Medium | Low | Document that the resource name is the provider's identity |
| Upgrade drops an env-configured provider | High | Low | Old settings honoured for one release; same provider names |
| Claude Code cannot use an OpenAI-compatible model | Medium | Medium | Document which provider types serve Claude Code |
| A rotated credential is picked up late | Low | Medium | Refresh period documented; local refresh command forces pickup |

---

## Alternatives Considered

### Alternative 1: Keep install settings and add more providers

**Description:** Add settings for each new provider type.

**Why not chosen:**
- Every change still needs a restart.
- Credentials still reach every service.

### Alternative 2: Register providers in the dashboard

**Description:** A form that stores providers and keys in Orbit's database.

**Why not chosen:**
- Keys would travel through the API and live in the database.
- Providers would drift from the deployment's configuration repository.

---

## Open Questions

1. **Should tenants bring their own providers?**
   - **Status:** Open
   - **Owner:** Justin Wilkin

2. **Should OpenRouter prices sync automatically?**
   - **Status:** Open
   - **Owner:** Justin Wilkin

3. **How long is the compatibility window for the old settings?**
   - **Status:** Open (proposed: one release)
   - **Owner:** Justin Wilkin

---

## Timeline and Milestones

### Phases

**Phase 1: Bedrock through resources**
- Provider resource, live pickup, Bedrock migration, provider list.

**Phase 2: OpenAI-compatible providers**
- OpenAI, OpenRouter and self-hosted compatible services.

**Phase 3: Parity**
- Anthropic API, Vertex AI and Azure AI through resources.

### Key Milestones

- [ ] PRD Approved
- [ ] ADR Accepted
- [ ] Implementation Started
- [ ] Launch

---

## Appendix

### Related Documents

- [ADR: CRD-Driven Inference Providers](../adr/2026-10-01-crd-inference-providers.md)
- [Multi-Account AWS Bedrock Support](multi-account-bedrock.md)
- [Inference Gateway](inference-gateway.md)

### Glossary

- **Provider:** a service that runs models, such as AWS Bedrock or OpenRouter.
- **Resource:** a declarative object applied to the cluster, reviewed and versioned in the deployment's configuration repository.
- **Catalog:** Orbit's list of models, how they route to providers, and their prices.
