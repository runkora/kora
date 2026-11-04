# Tenant Claim Binding and Shared Email Domains

**Status:** Proposed
**Date:** 2026-10-26
**Deciders:** Platform maintainers
**Technical Story:** [#786](https://github.com/runkora/kora/issues/786)

---

## Context

Home Realm Discovery routes a person to a tenant by the domain of their email. One domain reaches one tenant. Per-team tenants in one company need a separate domain each.

[Tenant Registration and OIDC Provider Binding](2026-03-31-tenant-registration-and-oidc-provider-binding.md) designs `claimBinding` with a single `value`. Nothing implements it. Two tenants that claim one domain today resolve to either one, by map order. Open item 2 of [Home Realm Discovery](2026-04-01-home-realm-discovery-and-email-based-tenant-routing.md) asks for that to fail loudly.

### Problem Statement

Let several tenants share an email domain, and choose the tenant for a person from their ID token after sign-in.

### Non-Goals

- A tenant picker or switcher for a person who matches several tenants.

---

## Considered Options

### Option 1: Shared domain through claim bindings

Tenants may share a domain when they share `issuerURL` and `clientID` and each sets `claimBinding` with a `claim` and a list of `values`. Discovery returns the shared issuer. After sign-in, the identity service issues the session for the one tenant whose binding matches.

**Pros:** One sign-in client. No new routing input. Overlaps are decidable from the registry.

**Cons:** The IdP must emit a claim that separates the teams.

### Option 2: A tenant hint at sign-in

The person names their tenant on the sign-in page.

**Pros:** No IdP claim needed.

**Cons:** Adds UI and a new request input. The tenant is self-selected, so the token does not prove membership.

### Option 3: One domain per tenant, as today

**Pros:** No change.

**Cons:** Does not meet the need. Duplicate domains stay silent unless the registry check is kept.

---

## Decision

**Chosen Option:** Option 1, shared domain through claim bindings.

### Rules

- **Binding:** `spec.oidc.claimBinding` holds `claim` and `values`. A scalar claim matches when it equals one value. An array claim matches when it contains one value. A missing claim never matches.
- **Plural values:** `values` replaces the single `value` of the earlier design, so one tenant can bind several groups.
- **Unbound tenants:** A tenant without `claimBinding` keeps issuer-only membership.
- **Sign-in:** A bound tenant resolves among the tenants on the same issuer and client whose `emailDomains` include the domain of the verified ID token's `email` claim, compared case-insensitively. The email domain is a gate and the claim chooses among the tenants behind it, so a person whose groups match a tenant on another domain gets a 403. A token without an `email` claim considers only the requested tenant. No match is 403. One match issues the session for that tenant, even if the client named another. Several matches fail with an error that lists the tenants.
- **Discovery:** For a shared domain, discovery returns the tenant with the lowest ID. All sharing tenants have the same issuer and client.
- **Overlap:** Two tenants may claim one domain only under the shared-domain rule. The identity service refuses a registry with any other overlap. The operator marks the newer `KoraTenant` `Invalid`, leaves it out of the registry and keeps the older tenant's domain.

---

## Consequences

### Positive

- Per-team tenants share a company domain.
- Duplicate domains fail loudly instead of resolving by chance.

### Negative

- A person in the groups of several tenants cannot sign in until the picker exists.
- The IdP admin controls tenant membership through the claim values.

---

## Related Decisions

- [tenant-registration-and-oidc-provider-binding](2026-03-31-tenant-registration-and-oidc-provider-binding.md) - Designs `claimBinding`
- [home-realm-discovery-and-email-based-tenant-routing](2026-04-01-home-realm-discovery-and-email-based-tenant-routing.md) - Email domain routing

**Last Updated:** 2026-10-26
