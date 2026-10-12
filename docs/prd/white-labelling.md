# White-labelling - Product Requirements Document

**Status:** Approved  
**Created:** 2026-10-06  
**Last Updated:** 2026-10-12  
**Owner:** Qj Stout-Spykers  
**Stakeholders:** Frontend Engineering, Platform Engineering, Identity/Backend Engineering

> This PRD defines the theme as an artefact, the contract it conforms to, and how an instance loads, validates and renders it. It is written independently of whatever produces a theme. Anything that authors a theme does so against the contract defined here and is specified elsewhere, so that the instance side can be built without reference to any particular producer. The contract is defined by the Hades design system version, never by the kora version, and this PRD depends on the versioning story in the [Hades Design System PRD](design-system.md#version-the-design-system-so-a-theme-contract-can-be-defined-against-it): no contract can exist until Hades carries a version to define it against. This PRD is canonical for the theme contract, the Theme Surface, the compatibility rules, and the glossary.

---

## Executive Summary

kora is deployed as a separate instance per client organisation. Today every instance looks and reads like kora: the kora logo, the kora colour palette, the kora name in page titles and in-product copy. This PRD defines white-labelling as deployment configuration: a theme, supplied with the rest of an instance's configuration, that restyles and renames the instance to match the client's own identity. A theme is fixed for the life of a deployment. Changing it is a configuration change and a redeploy, never a runtime action. The theme is an artefact that conforms to a published contract; this PRD defines that contract and what an instance does with a theme that meets it.

---

## Problem Statement

### Current State

The product identity is fixed in code. The colour palette, corner radii, fonts, logo, favicon, page title and every piece of copy that names the product are compiled into the frontend. There is no concept of a brand that varies by instance, no place to define one, and no way to carry one between environments.

### User Pain Points

- A client organisation cannot present kora to its own staff under its own brand. Every screen, from the login page onward, advertises a third-party product.
- Restyling an instance today means forking the frontend, editing design tokens and SVG assets by hand, and rebuilding the container. That work has to be repeated on every upgrade.
- Product-name copy is scattered across the frontend and agent runtime messages. Even a hand-edited fork misses strings.

### Business Impact

- White-labelling is a precondition for selling kora as an embedded platform inside client organisations.
- Every manual rebrand is unrepeatable engineering effort that blocks upgrades and erodes the single-codebase, single-image model.

---

## Goals and Objectives

### Primary Goals

1. A client instance can be branded so that no kora identity is visible anywhere in the product: every page the dashboard serves, authenticated or not, page titles, favicon, in-product copy and agent runtime messages.
2. The theme is deployment configuration. It is in place from the instance's first deployment and reaches every environment the instance is deployed to the same way the rest of its configuration does.
3. For every value a theme sets and the instance recognises, the instance renders that value unchanged. There is no translation step between a theme and a deployment.
4. Branding a new client requires no code change in this repository.
5. A client on an older kora release keeps a working theme contract. The contract is versioned with the design system, not with kora, so a kora release never invalidates a theme on its own.

**User-visible** means any text or asset the dashboard renders, including run transcripts and tool output it displays. Text sent only to the model, such as the agent system prompt and tool descriptions, is also in scope, because the model echoes it into transcripts the dashboard renders. Machine-readable fields such as error codes, identifiers and API field names are not user-visible.

### Success Metrics

- The hardcoded product-name check defined in decision 5 of the white-labelling theme ADR runs in `make check` and passes. The ADR defines the source trees the check covers, the classes of string literal it inspects, and the exact names it searches for, given that identifiers and hostnames in the codebase still say `orbit`.
- The first HTML response for any page already carries the effective theme, so no client-side restyle happens after load. Verified on the local instance and the `dev` deployment.
- The `dev` deployment carries a reference theme for verification. For every value that theme sets and the instance recognises, the effective value on `dev` is the theme's value unchanged.
- The acceptance rules are covered by tests: a theme recording a design system version in a different major fails validation; a theme in the instance's major carrying a token the instance does not recognise is accepted and the token is logged; a recognised token with a malformed value fails validation; a value the theme does not set falls through to the kora default without failing.
- An invalid theme never reaches a running instance. The deploy fails before the instance serves a page.

### Non-Goals

- Per-user or per-tenant brands within a single deployment. One deployment has one brand.
- Runtime editing of the theme. There is no in-product editor and no API that changes the theme of a running instance.
- Per-client container images. One image serves every client; the theme is configuration.
- Branding of CLI output and of any surface outside the dashboard and the agent runtime messages it shows.
- Sign-in and consent pages hosted by the client's identity provider. Those carry the provider's own branding.
- Email branding. No email templates exist today.
- Shipping Storybook, or any design-tooling artefact, to clients as a client-facing product. A preview build published for whoever produces a theme is not a client deliverable.

---

## User Stories

### US-001: Deploy a theme

**As a** platform engineer standing up a client instance,  
**I want to** supply the client's theme as part of the instance's deployment configuration,  
**So that** the instance is branded from its first deployment and the theme reaches every environment it is deployed to without manual steps.

**Acceptance Criteria:**
- [ ] A theme can be defined before the instance is first deployed.
- [ ] Each deployment has exactly one theme. Promoting a theme from one environment to the next means applying the same theme to the next environment's configuration.
- [ ] The theme changes only on redeploy. Nothing at runtime changes it.
- [ ] A theme may be partial. Any value absent from the theme falls through to the kora default, and a missing value never fails the deploy. A deployment with no theme is a kora-branded instance, which is the intended state for internal deployments and for clients who do not want to rebrand.
- [ ] A theme is validated when the instance loads it. Validation fails on a recognised token with a malformed value, or on a design system version in a major other than the instance's. A failed validation fails the deploy before the instance serves any page, and the failure names the offending values.
- [ ] Supplying a theme never forces its brand assets (fonts, logos, favicon) to be committed to a git repository.
- [ ] A kora upgrade that keeps the same design system major leaves the theme in force. An upgrade that crosses a design system major is a breaking change to the theme contract, and a theme for the new major is applied with the upgrade.

### US-002: Product identity

**As a** client organisation,  
**I want** the instance to carry our product name and identity assets wherever identity is shown,  
**So that** the instance is recognisably ours from the first page it serves.

**Acceptance Criteria:**
- [ ] The product name is a single verbatim string. It replaces every user-visible occurrence of the name, including page titles, dashboard copy, confirmation dialogs and agent runtime messages shown in the product. Copy is written so the name needs no possessive or plural form.
- [ ] The instance renders the favicon and the logo the theme supplies wherever identity is shown.
- [ ] Logos and the favicon are addressed per mode, as colour tokens are. A theme may set either mode's assets independently, and an unset mode falls through to the kora default for that mode.
- [ ] An identity value cannot be set to nothing. A theme supplies a value or the kora default renders. Goal 1 is therefore met only by a theme that sets every identity value in both modes, and the published validation rules report which identity values a theme leaves to fall through.
- [ ] Identity is rendered through agnostic Hades slot components that read from the effective theme. The kora logo drawing that lives in Hades today moves out, into the kora default theme at app level, and the static favicon file and hardcoded title are replaced by the same slots. The backend reads the product name from the theme the same way the frontend does.
- [ ] Every unauthenticated page the dashboard serves, including the login page and the MCP consent flow, renders the theme.

### US-003: Shape and typography

**As a** client organisation,  
**I want** the instance to render our corner radius scale and our fonts,  
**So that** the instance matches our brand guidelines, not only our colours.

**Acceptance Criteria:**
- [ ] The corner radius scale in the theme applies across the product. Each step of the scale is a literal value in the theme.
- [ ] Fonts carried by a theme apply to an instance without a rebuild of the kora image.

### US-004: Apply a theme to a deployment

**As a** platform engineer,  
**I want** any theme that conforms to the contract to be accepted as-is by the instance's deployment configuration,  
**So that** a theme travels between environments without being recreated, whatever produced it.

**Acceptance Criteria:**
- [ ] The theme records one exact design system version: the version whose contract it conforms to. The targeted major is derived from it. This is the only mandatory field in a theme. The exact version is informational beyond its major; the instance logs it.
- [ ] The theme is in a form the deployment configuration can consume directly. The format, and whether brand assets travel inside it or beside it, are decided in the ADR. Whatever the form, applying a theme never forces brand assets into a git repository.
- [ ] A theme is accepted by any kora release whose pinned design system version is in the same major. A different major is a validation failure and fails the deploy. Within a major, an instance ignores and logs any token it does not recognise, because it cannot tell a later release's token from a mistake. A token it does recognise with a malformed value still fails the deploy.
- [ ] The validation rules are published per exact design system version as part of the contract, so a theme can be checked before it is applied. A theme that passes the rules of version V passes validation on any instance pinning a version in the same major at or below V. A later minor may recognise a token the earlier rules ignored, and then holds it to the malformed-value rule.
- [ ] For every value the theme sets and the instance recognises, the effective value is the theme's value unchanged.

---

## Theme Surface

The set of things a client can change in v1. Everything on this list is configurable; a theme may set any subset, and anything it does not set falls through to the kora default. The binary files among them (logos, favicon, fonts) are the theme's brand assets. This table is canonical.

| Area | What is configurable |
|---|---|
| Colours | Every colour token in both dark and light mode, each set individually. Includes status colours (healthy, degraded, error) and provider and strategy brand colours. |
| Depth | Every shadow token in both dark and light mode, each set individually. |
| Identity | Product name. Favicon and logo, each per mode. |
| Shape | Corner radius scale, each step a literal value. |
| Typography | UI font and monospace font, supplied as font files. |

---

## Proposed Solution

### High-Level Approach

A deployment renders one theme over the kora default.

- **Theme.** Supplied as part of the instance's deployment configuration. It is what the instance renders, value by value, for everything it sets. It changes only on redeploy. Because it is deployment configuration, it travels through environments the same way the rest of the configuration does. A theme may be partial.
- **kora default.** Any value absent from the theme falls through to the kora default. kora default values are outside the theme contract: any kora release may change a default, and that changes the fallthrough silently. An instance with no theme is a kora-branded instance.

The effective theme is the kora default, then the theme. There is no runtime layer: nothing in the product reads, writes or edits the theme after startup, and the product has no concept of who set a value. The product renders the effective theme everywhere it shows identity, including every unauthenticated page it serves.

The design system already structures its tokens so that a brand can be substituted beneath the components without touching them. White-labelling builds on that: the theme is the source for those tokens plus identity assets, and the product reads from it at startup rather than from compiled-in defaults.

**The theme contract is the Hades design system major, not the kora version.** Hades is semantically versioned; a major defines the theme surface and the theme format, and minor and patch releases within a major only add to the surface. Each kora release pins one Hades version, which is the version an instance validates against. What is and is not a contract change is decided by the contract boundary rules, a numbered list owned by the Hades Design System PRD and a requirement of this PRD: Hades defines the shape of every value a theme can set, as its theme surface manifest; who reads a value, the frontend or the backend, is not the contract's concern; Hades contains no kora identity; a kora change that does not change Hades, such as a change to a page or a template or a new place where a value appears, has no effect on the contract, while one that needs a new value is a Hades change; and kora default values are outside the contract. Crossing a Hades major is a breaking change, and a theme for the new major is applied with the upgrade. The versioning rules and how each design system change is classified are specified in the [Hades Design System PRD](design-system.md#version-the-design-system-so-a-theme-contract-can-be-defined-against-it).

The following are contract rules. They hold for every theme, whatever produced it:

- A theme records one exact design system version, the version whose contract it conforms to. The targeted major is derived from it. It is the only mandatory field.
- The product name is a single verbatim string, bounded in length and character set.
- A theme contains literal values only. An instance renders what the theme says and never computes one value from another. If one value was derived from another when the theme was authored, that is invisible to the instance.
- A value cannot be set to nothing. A theme sets a value or the kora default renders.
- The validation rules are part of the contract and are published per exact design system version, so a theme can be validated before it is applied with the same rules the instance applies on load.
- Within a major, an unrecognised token is ignored and logged; a recognised token with a malformed value fails the deploy; a missing value never fails the deploy.

The following are architectural decisions and are deferred to the white-labelling theme ADR. They are numbered so that other documents can cite each decision directly:

1. The theme contract: schema, the recorded version, validation rules and how they are published per version, the unknown-token rule, the per-mode addressing of colours and identity assets, and the asset packaging modes, subject to the constraint that brand assets are never forced into a git repository. The accepted asset formats and sizes and the product-name bounds are fields of the Hades theme surface manifest, decided in subsection 3 of the design system versioning ADR; this decision cites them.
2. How the theme enters the deployment configuration and how brand assets reach the instance. A custom resource reconciled by the operator is a candidate, following the precedent of [CRD-driven inference providers](crd-inference-providers.md).
3. Where validation runs and how an invalid theme fails the deploy before the instance serves a page.
4. Startup loading, rendering the effective theme in the first HTML response, serving fonts and logos, and the agnostic Hades slot components that render the product name, logo and favicon from the effective theme, with the kora logo moved out of Hades into the default theme.
5. Product-name wiring and the hardcoded-string check: the source trees it covers, the classes of string literal it inspects, and the names it searches for.
6. Template exposure, for the P1 requirement: whether selected real page templates can be exposed for rendering outside the app, which ones, and how they are delivered alongside each Hades major. This decision may conclude that it is not viable.

How the pinned design system version is exposed to the instance is subsection 2 of the design system versioning ADR; the contract subsection cites it rather than deciding it again.

### Theme lifecycle

| Theme present | Effective theme | After a redeploy with the same theme | After a redeploy with a new theme | After a kora upgrade within the same design system major |
|---|---|---|---|---|
| No | kora default | kora default | New theme | kora default of the new release |
| Yes | Theme over kora default | Unchanged | New theme | Theme over the new release's defaults for any value the theme does not set |

A redeploy that removes the theme is a redeploy with an empty theme: the instance renders the kora default. Nothing survives from the old theme, because nothing is stored outside the deployment configuration.

The last column is not a contract change, because kora defaults are outside the contract. A kora upgrade that crosses a design system major is not in the table. It needs a theme for the new major, applied with the upgrade. An instance never accepts a theme targeting a different major than its own.

### User Experience

**Deploy.** A platform engineer places the theme in the instance's deployment configuration and deploys. If the theme is invalid the deploy fails and names the offending values. If it is valid the instance is branded from the first page it serves.

**Change.** The engineer replaces the theme in the deployment configuration and redeploys. The change reaches lower environments first, the same way as any other configuration change.

**Upgrade across a major.** The engineer applies a theme targeting the new major together with the upgrade. How that theme is produced from the old one is outside this PRD.

### Key Components

- **Theme** - the deployment-supplied configuration: colour tokens for both modes, product name, radius scale, and the brand assets (logos, favicon, fonts). Partial allowed. Records the exact design system version whose contract it conforms to.
- **Theme contract** - the schema, recorded version, validation rules and packaging modes a theme must conform to, versioned by the Hades major independently of the kora version, and published per version so a theme can be validated before it is applied.
- **Theme loading and validation** - the instance reads the theme at startup, validates it, and fails the deploy on an invalid theme.
- **Theme rendering** - the running instance renders the effective theme on every page it serves, including unauthenticated ones, with no flash of default styling.
- **Identity slots** - agnostic Hades components that render the product name, the logo and the favicon from the effective theme. The slots are the identity surfaces the product renders today and no more; a surface that needs a mark alone adds that slot when it exists. They replace the single combined logo drawing, static favicon file and hardcoded title kora uses today; the kora logo becomes an asset of the kora default theme at app level.
- **Product-name wiring** - a single configured name that every user-visible occurrence of the product name reads from, in the dashboard and in agent runtime messages shown in it.

---

## Scope

### In Scope

- The full theme surface above, in dark and light mode, with identity assets addressed per mode.
- A theme supplied as part of instance deployment configuration, partial allowed, validated at load, failing the deploy when invalid.
- The theme contract, published per design system version in a form that lets a theme be validated before it is applied.
- Agnostic Hades identity slots replacing the identity surfaces kora renders today: the sidebar brand header and the login card, which share one combined logo drawing; the static favicon file; and the page title. The kora logo moves out of Hades into the kora default theme at app level. The asset contract is derived from these surfaces, not defined in a vacuum.
- Replacement of every hardcoded product-name occurrence in the frontend, in model-facing text, and in agent runtime messages shown in the product with the configured name.

### Out of Scope

- Anything listed under Non-Goals.
- Branding of machine-readable output: error codes, identifiers, API field names.
- Any tool that authors, previews or converts a theme. This PRD defines the artefact and its contract; producers of the artefact are specified separately, against this contract.
- Versioning of the design system. It is a user story in the Hades Design System PRD, and a precondition of this one.

### Future Considerations

- Per-tenant themes layered on the deployment theme, if a single instance ever serves organisations with different brands.

---

## Requirements

### Functional Requirements

**Must Have (P0):**
- [ ] One effective theme per deployment, composed of the kora default and a theme, covering the full theme surface in both modes.
- [ ] Theme supplied as deployment configuration, one per deployment, changed only on redeploy, may be partial, never forces brand assets into a git repository.
- [ ] Theme validated at load. A recognised token with a malformed value, or a design system version in another major, fails the deploy before the instance serves a page and names the offending values. A missing value never fails the deploy.
- [ ] Every user-visible product-name occurrence reads from the configured name, including model-facing text.
- [ ] Identity assets addressed per mode, rendered by shared components that read from the effective theme.
- [ ] Every unauthenticated page the dashboard serves renders the theme.
- [ ] The first HTML response carries the effective theme; no client-side restyle after load.
- [ ] The theme records one exact design system version, its only mandatory field, from which the targeted major is derived; a theme is accepted by any kora release pinning a version in the same major; the validation rules are published per version as part of the contract; for every value the theme sets and the instance recognises, the effective value is the theme's value unchanged.
- [ ] The contract boundary rules are documented explicitly, as the numbered list in the Hades Design System PRD, and the ADR contract subsection cites them. No decision about what a theme can set, or about what counts as a contract change, is made outside them.

**Should Have (P1):**
- [ ] kora exposes selected real page templates for rendering outside the app, per Hades major, so that a consumer of white-labelling can preview a theme on the product's actual pages rather than on compositions of components. kora does not know which consumers exist, only that they will. Which templates, how they are delivered alongside each Hades major, and whether this is viable at all given the templates live in the app rather than in Hades, are decided in decision 6 of the white-labelling theme ADR.

**Nice to Have (P2):**
- None.

### Non-Functional Requirements

**Security:**
- Everything in a theme ends up rendered in the product, so all theme input must be validated and bounded before it is rendered. The instance validates at load, and the same rules are published so a theme can be validated before it is applied. Colours are restricted to recognised colour formats, numbers to ranges, assets to expected file types and sizes. The product name is bounded in length and character set and may contain no control characters or markup; subsection 3 of the design system versioning ADR sets the bounds. Arbitrary strings are never rendered as styling.
- The effective theme is public, since unauthenticated pages render it. Nothing confidential may be placed in a theme.

**Performance:**
- First paint on a branded instance stays within the current p95 for first paint plus a budget set in the ADR.

**Reliability:**
- An instance renders the whole validated theme over the kora default, or the deploy fails. It never serves a page from a theme that failed validation or did not finish loading. Values the theme does not set are not a failure; they fall through. A deployment with no theme renders the kora default, which is an acceptable rendered state.

**Observability:**
- Theme changes are attributable through the deployment configuration's own version history. The product keeps no change history of its own.
- An instance logs at startup which theme it loaded, the design system version recorded in the theme, and any tokens it did not recognise and ignored.

**Compatibility:**
- The theme contract is the Hades design system major, not the kora version. A theme recording a version in major N is accepted by every kora release pinning a version in major N, including releases pinning an earlier version than the one the theme records. A version in any other major is a validation failure. A kora change that does not change Hades does not affect the contract, and kora default values are outside it.
- Within a major, an instance ignores and logs a token it does not recognise. It cannot tell a token from a later release of its major from a mistake, so it never fails the deploy on one. A recognised token with a malformed value fails the deploy. A missing value never does.
- The validation rules are published per exact version. A theme that passes the rules of version V passes on any instance pinning a version in the same major at or below V; a later minor may recognise a token those rules ignored.
- The rule that decides whether a design system release is a minor or a major is in the [Hades Design System PRD](design-system.md#version-the-design-system-so-a-theme-contract-can-be-defined-against-it). It is decided on the Hades theme surface manifest and scoped to the values a theme sets, so a change to a kora default alone is a patch, never a major.

---

## Dependencies

### Internal Dependencies

- **Hades design system versioning** - a hard dependency. No theme contract exists until Hades carries a version to define it against, so the versioning story in the [Hades Design System PRD](design-system.md#version-the-design-system-so-a-theme-contract-can-be-defined-against-it) is a precondition for this PRD's ADR contract subsection, not only for implementation. Each kora release pins one Hades version, which is the version an instance validates against.
- **Hades token structure** - the token structure already anticipates brand substitution. Four Hades changes are needed by this PRD and are made for the first Hades major: the corner radius steps, derived from a single unit today, become literal tokens; agnostic identity slot components are added; font slot tokens are added for the font families to resolve through; and the kora logo and mark move out of Hades into the kora default theme. Each Hades release then carries the theme surface manifest the validation rules are generated from.
- **Identity surfaces in kora today** - the asset contract is derived from these, and they are replaced by the Hades identity slots: one combined logo drawing in Hades (mark and wordmark as a single SVG) used by the sidebar brand header and the login card, which becomes an asset of the kora default theme at app level; the static favicon file under the app directory; and the page title in the root layout.
- **Instance configuration** - the theme enters through whatever carries instance configuration today; the ADR decides the vehicle.
- **Deploy pipeline** - an invalid theme must fail the deploy, which depends on where validation runs and how the pipeline surfaces it.

### External Dependencies

- None. Nothing outside this repository is required to deploy a theme. Tools that produce a theme depend on this PRD's contract, not the other way round.

---

## Risks and Mitigations

| Risk | Impact | Probability | Mitigation |
|------|--------|-------------|------------|
| Hardcoded product-name strings are missed | Medium | High | An automated check over user-facing string literals that fails on a hardcoded occurrence, with its trees, literal classes and names defined in the ADR. |
| Unvalidated theme input becomes a styling injection vector | High | Low | Validation of every field and asset in the instance at load, with the rules published so the same check can run before a theme is applied. |
| An invalid theme blocks a deploy | Low | Medium | The validation rules are published, so a theme can be checked before it is applied; lower environments catch a mistake first. Missing values never fail. |
| The asset contract is defined without reference to what kora renders | Medium | Medium | Derive it from the identity surfaces in code today and replace those with Hades identity slots as part of the work. |
| A kora default change is mistaken for a contract change | Low | Medium | Defaults are stated as outside the contract here and in the Hades versioning story, whose classification rule is scoped to the values a theme sets. |
| Model-facing text still names kora after the dashboard is rebranded | Medium | Medium | Model-facing text is in scope and covered by the same check. |

---

## Alternatives Considered

### Alternative 1: Deployment seed plus runtime overrides

**Description:** A seed supplied at deploy time, with an in-product editor that writes persisted overrides on top of it, value by value. This was the previous proposal in this PRD.

**Why not chosen:**
- It needs persisted brand state in the product, a new write scope, an in-product editor, an export and import path for overrides, a notice when a redeploy changes a seed value hidden by an override, and reconciliation rules between the layers.
- All of that serves a change that happens rarely per client. Deploy-time configuration reaches the same end state, with the configuration's version history as the audit trail and no runtime surface to secure.

### Alternative 2: Build-time branding

**Description:** The brand is baked into the frontend at build, one container image per client.

**Why not chosen:**
- One image per client breaks the single-image distribution model, and every change needs a rebuild. Fails goal 4.
- Deploy-time configuration keeps one image and moves the theme into the same configuration path everything else already uses.

### Alternative 3: Per-tenant branding

**Description:** Each tenant in a deployment carries its own brand.

**Why not chosen:**
- The deployment model is one instance per client organisation, so there is no second brand to serve. It also leaves unauthenticated pages unbranded until the tenant is known. Deferred to Future Considerations.

---

## Open Questions

None.

### Resolved Questions

1. **What happens when a seed and runtime edits both exist?** Resolved by removing runtime edits. There is one theme per deployment, set only at deploy time.
2. **Does a theme carry its assets?** Deferred to the ADR, under one constraint from this PRD: applying a theme never forces brand assets into a git repository. The contract may need to offer the values alone and the values with assets; the ADR decides.
3. **Is theme authoring part of this PRD?** No. This PRD defines the theme as an artefact and the contract it conforms to, so that the instance side is built without reference to any particular producer. Anything that authors, previews or converts a theme is specified separately, against this contract.
4. **What does a theme target?** The Hades design system major, not the kora version. A theme records one exact Hades version and its major is derived from it. An instance accepts any theme in its own major and refuses every other. A kora change that does not change Hades never affects the contract.
5. **Can an identity value be set to nothing?** No. A theme supplies a value or the kora default renders. A theme that wants no kora identity visible sets every identity value in both modes.
6. **Is the asset contract defined here?** No. It is derived from the identity surfaces kora renders today and recorded in the Hades theme surface manifest, in subsection 3 of the design system versioning ADR. Those surfaces are replaced by agnostic Hades slot components, and the kora logo becomes an asset of the kora default theme at app level, so Hades contains no kora identity.
7. **Which backend strings name the product?** Enumerated in decision 5 of the [White-labelling Theme ADR](../adr/2026-10-12-white-labelling-theme.md): guardrail messages, the capabilities brief, MCP tool descriptions and the registered MCP server name, egress denial messages, and the sandbox base image description. Identifiers, log lines and operator-facing output keep the kora name.
8. **How is the theme wired into the deployment?** Decided in decisions 2 and 3 of the same ADR: the document as a chart value supplied from the repository file, the bundle as a digest-pinned OCI image mounted as an image volume, validation in an init container in the frontend pod, and a theme change as a rolling update triggered by the values change.

---

## Timeline and Milestones

### Key Milestones

- [ ] PRD Approved
- [ ] Hades Design System PRD versioning story approved
- [ ] Design system semantic versioning ADR created, including the classifier's trigger point. A precondition for the contract subsection below.
- [ ] White-labelling theme ADR created, with the contract subsection agreed first so that anything producing a theme can be specified against it
- [ ] Implementation complete and verified on the local instance and the `dev` deployment

---

## Appendix

### Related Documents

- [Hades Frontend Design System ADR](../adr/2026-03-25-terra-frontend-design-system.md) - token structure this builds on
- [Hades Design System PRD](design-system.md#version-the-design-system-so-a-theme-contract-can-be-defined-against-it) - the versioning story that defines the theme contract

### Glossary

This glossary is canonical for white-labelling terms. Documents that specify a producer of themes use these terms with these meanings.

- **Theme:** the whole configuration artefact a deployment consumes and the instance loads: colour tokens, product name, radius scale, fonts, logos and favicon. Changes only on redeploy. May be partial. Records the exact design system version whose contract it conforms to, its only mandatory field.
- **Theme contract:** the schema, recorded version, validation rules and packaging modes a theme must conform to. Versioned by the Hades design system major, independently of the kora version. Published per version, generated from the Hades theme surface manifest, so a theme can be validated before it is applied.
- **Brand assets:** the binary files inside a theme: logos, favicon and fonts. The subset that must never be forced into a git repository.
- **Brand:** used only in prose for the client's own identity. Never a layer, a file or a field.
- **Design system version:** defined in the [Hades Design System PRD](design-system.md#glossary). The major is the theme contract.
- **Target:** the design system major a theme is made for. Derived from the version the theme records.
- **Effective theme:** the kora default, then the theme, value by value. What the product renders.
- **kora default:** the value the product renders for anything the theme does not set. Outside the contract; may change in any kora release.
- **User-visible:** any text or asset the dashboard renders, including transcripts and tool output, plus model-facing text that can be echoed into them.
