# Tenant Switching - Product Requirements Document

**Status:** Draft  
**Created:** 2026-11-04  
**Last Updated:** 2026-11-04  
**Owner:** Justin Wilkin  
**Stakeholders:** Platform Team, tenant admins, people who work across several teams

---

## Executive Summary

Tenant switching will let a person who belongs to several teams' tenants sign in and move between those tenants without signing out. The dashboard, the CLI sign-in and the MCP consent page will each let the person choose the tenant they act in. The identity provider stays the authority on membership: kora lets a person into a tenant only when their current sign-in proves they belong to it.

---

## Problem Statement

### Current State

kora runs one tenant per team, and several teams in one company can share an email domain. Each tenant admits people by their identity provider groups. A person whose groups match more than one tenant cannot sign in. The sign-in fails with an error that lists the tenants and offers no way to pick one. The CLI sign-in fails the same way.

### User Pain Points

- **Locked out:** A person added to a second team's groups loses access to kora entirely.
- **No way to move:** A person who works in two teams cannot reach the second team's agents, runs and settings from the session they already have.
- **Unclear MCP target:** An MCP client that a person connects acts in one tenant, and the consent page does not say which.

---

## Goals and Objectives

### Primary Goals

1.  A person who matches several tenants can sign in.
2.  A signed-in person can move to any other tenant they belong to from the dashboard.
3.  A person can choose the tenant for a CLI sign-in.
4.  A person can see and change the tenant an MCP client will act in before granting consent.
5.  Every switch is checked against the person's current membership in the identity provider.

### Success Metrics

None at this time.

### Non-Goals

- Membership managed in kora. Tenant membership stays in the identity provider groups.
- Several tenants open at once in one browser.
- Switching for agents, service accounts or personal access tokens.

---

## User Stories

### US-001: Sign in when several tenants match

**As a** person who belongs to several teams' tenants,  
**I want** to sign in and land in the tenant I used last,  
**So that** I can work without picking a tenant every time.

**Acceptance Criteria:**

- [ ] Sign-in opens the tenant used last in this browser when the person still belongs to it.
- [ ] On the first sign-in, or when the person no longer belongs to the last-used tenant, sign-in shows the matching tenants by name and the person picks one.
- [ ] Picking a tenant does not send the person back to the identity provider.
- [ ] A person who matches one tenant sees no picker.

### US-002: Switch tenant in the dashboard

**As a** person working across teams,  
**I want** to switch to another of my tenants from the dashboard,  
**So that** I can reach that team's agents and runs without signing out.

**Acceptance Criteria:**

- [ ] The dashboard shows the current tenant by name and lists the person's other tenants.
- [ ] Choosing a tenant moves the person to it and opens the same page in that tenant.
- [ ] The switcher does not appear for a person with one tenant.
- [ ] The tenant the person switches to becomes the last-used tenant for the next sign-in.

### US-003: Switch fails safely

**As a** person whose group membership has changed,  
**I want** a failed switch to leave me where I was,  
**So that** a lost membership does not sign me out of the tenant I am using.

**Acceptance Criteria:**

- [ ] When the person no longer belongs to the chosen tenant, the switch fails with a message that says so.
- [ ] After a failed switch, the person stays signed in to the current tenant.
- [ ] After a failed switch, the tenant list reflects the person's current membership.
- [ ] When the identity provider session has ended, the switch sends the person through a normal sign-in to the chosen tenant.

### US-004: Choose the tenant for a CLI sign-in

**As a** developer who belongs to several tenants,  
**I want** to choose the tenant when I sign in from the terminal,  
**So that** my CLI token acts in the right team.

**Acceptance Criteria:**

- [ ] The CLI sign-in accepts a tenant.
- [ ] When the person matches several tenants and gave none of them, the CLI lists the matching tenants and asks for a new sign-in with one of them.
- [ ] A CLI sign-in never grants a tenant the person does not belong to.

### US-005: See and change the tenant at MCP consent

**As a** person connecting an MCP client,  
**I want** the consent page to show which tenant the client will act in and let me change it,  
**So that** the client acts in the team I intend.

**Acceptance Criteria:**

- [ ] The consent page shows the current tenant by name.
- [ ] A person with several tenants can switch tenant from the consent page and return to the same consent request.
- [ ] Consent grants the client access to the tenant shown on the page.

---

## Proposed Solution

### User Experience

#### User Flow

1.  The person opens the tenant switcher in the dashboard and chooses a tenant.
2.  kora checks the person's current membership with the identity provider. The person normally sees no identity provider page.
3.  On success, the dashboard reloads in the chosen tenant.
4.  On failure, the person stays in the current tenant and sees why the switch failed.

---

## Scope

### In Scope

- The sign-in picker for people who match several tenants.
- The dashboard tenant switcher.
- The tenant choice in the CLI device sign-in.
- The tenant name and switch control on the MCP consent page.
- Tenant display names in the picker, the switcher and the consent page.

### Out of Scope

- Tenant membership changes inside kora.
- Several signed-in tenants in one browser at the same time.

---

## Product Decisions

**PD1: The identity provider decides membership:** The tenant list a person sees is for display. Only the person's current sign-in proves membership of a tenant. **Accepted cost:** every switch checks with the identity provider and fails when the provider cannot confirm membership.

**PD2: Remembered tenant:** The browser remembers the person's tenant between sign-ins. **Accepted cost:** a person who wants a different tenant signs in first and then switches.

**PD3: One tenant per browser:** A browser holds one signed-in tenant at a time. **Accepted cost:** a person cannot keep two tenants open in two tabs of one browser.

**PD4: No sign-out on a failed switch:** A switch that fails keeps the current session. **Accepted cost:** a person who no longer belongs to the current tenant stays in it until the session ends.

### Decisions delegated to the ADR

The ADR will decide:

- How sign-in returns the matching tenants and where the dashboard keeps them.
- How a switch proves membership with the identity provider.
- How the last-used tenant is stored.
- How the CLI and the MCP consent page carry the tenant choice.
- What a switch records for audit.

---

## Related Documents

- [Tenant Claim Binding and Shared Email Domains ADR](../adr/2026-10-26-tenant-claim-binding-and-shared-email-domains.md)
- [Tenant Switching ADR](../adr/2026-11-04-tenant-switching.md)
- [Browser Authentication Flow PRD](browser-auth-flow.md)
- [MCP / Tool Gateway PRD](mcp-tool-gateway.md)
