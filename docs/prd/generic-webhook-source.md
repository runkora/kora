# Generic Webhook Source - Product Requirements Document

**Status:** Implemented  
**Created:** 2026-10-27  
**Last Updated:** 2026-10-27  
**Owner:** Justin Wilkin  
**Stakeholders:** Platform Team, trigger authors, open source contributors

---

## Executive Summary

A generic webhook source will let an admin connect any system that sends webhooks, with no kora release for that system. The admin picks how kora verifies the sender and where the event type sits in the request, in the console or in a `TriggerSource` resource. Triggers on a generic source read the payload by field paths the author types.

---

## Problem Statement

### Current State

Webhook triggers accept deliveries from GitHub and Alertmanager, and chat triggers from Slack. Each provider is built into kora and ships in a release. A system outside that list cannot start a run, even when it sends a signed webhook in a common format. kora is open source, and the set of systems its users run is larger than any list kora can maintain.

### User Pain Points

- **No way to connect an unsupported system:** A team that wants a run on a Linear issue, a Sentry error or an internal service event has to wait for a provider to be added, or contribute one in Go.
- **Relays outside kora:** Teams run a forwarder in front of kora that re-signs deliveries as a supported provider. The forwarder hides the original sender and is one more service to operate.

---

## Goals and Objectives

### Primary Goals

1.  An admin connects any system that sends webhooks over HTTPS without a kora release.
2.  A generic source is created the same way from the console and from a `TriggerSource` resource.
3.  Every delivery to a generic source is verified before kora acts on it.
4.  A trigger author filters and templates on any field in the payload by typing its path.
5.  Generic deliveries appear in the delivery history with an event type when the sender supplies one.

### Success Metrics

None at this time.

### Non-Goals

- Schemas supplied by the admin, and field pickers for generic sources.
- Schema validation of triggers on a generic source.
- Sender restriction on generic sources.
- Chat platforms as generic sources.
- Verification methods outside the three listed in this document, and custom code or expressions in verification.
- Replacing the built-in providers. GitHub, Alertmanager and Slack keep their own sources.

---

## User Stories

### US-001: Connect a system that sends webhooks

**As a** tenant admin,  
**I want** to create a generic webhook source and choose how kora verifies it,  
**So that** a system kora has no built-in provider for can start agent runs.

**Acceptance Criteria:**

- [ ] The new-trigger screen offers "Generic webhook" next to GitHub and Alertmanager.
- [ ] The admin picks one verification method: a bearer token, an HMAC-SHA256 signature, or Standard Webhooks.
- [ ] For an HMAC-SHA256 signature, the admin sets the header that carries the signature, an optional prefix such as `sha256=`, and whether the signature is hex or base64.
- [ ] For Standard Webhooks, kora reads the id, timestamp and signature headers that the specification defines and refuses a delivery whose timestamp is outside the replay window.
- [ ] For HMAC-SHA256 and Standard Webhooks, the admin chooses up front whether kora generates the secret or the admin pastes the secret the sender issued.
- [ ] kora always generates a bearer token.
- [ ] A generated secret is shown once, at creation and on rotation. A pasted secret is never shown back.
- [ ] The source shows its webhook URL.
- [ ] A delivery that fails verification is refused and counted on the source.

### US-002: Read the event type from the delivery

**As a** tenant admin,  
**I want** to tell kora where a sender puts the event type,  
**So that** triggers and the delivery history can tell one kind of event from another.

**Acceptance Criteria:**

- [ ] The admin can set the event type location to a request header, such as `X-Event-Key`, or a field path in the payload, such as `type`.
- [ ] The event type location is optional. A source without one gives every delivery the event type `webhook`.
- [ ] The delivery history shows and filters by the event type of generic deliveries.

### US-003: Write a trigger on a generic source

**As a** trigger author,  
**I want** to type the event type and field paths myself,  
**So that** I can write a trigger for a payload kora has no schema for.

**Acceptance Criteria:**

- [ ] The event type is a free text field. Event types the source has already received are offered as suggestions.
- [ ] Filters and prompt placeholders take field paths typed by hand, such as `data.issue.title`.
- [ ] kora checks the syntax of each field path and does not check it against a schema.
- [ ] A field the delivery does not contain resolves to an empty value.
- [ ] Delivery values reach the agent inside the untrusted data section.
- [ ] The trigger form offers no sender restriction on a generic source.

### US-004: Declare a generic source as a resource

**As a** platform engineer,  
**I want** to declare a generic source in a `TriggerSource` resource,  
**So that** it is reviewed and deployed through GitOps with the rest of the tenant.

**Acceptance Criteria:**

- [ ] A `TriggerSource` with provider `generic` carries the same verification and event type settings as the console.
- [ ] The signing secret always comes from a Kubernetes Secret reference. kora generates no secret for a declared source.
- [ ] A source declared as a resource behaves the same as one created in the console.

### US-005: Repeated deliveries do not start repeated runs

**As a** trigger author,  
**I want** a delivery the sender retries to be recognized as a repeat,  
**So that** one event starts one run.

**Acceptance Criteria:**

- [ ] kora recognises a repeat of a generic delivery by its content.
- [ ] A repeat is recorded as a duplicate and follows the trigger's existing dedupe setting.

---

## Scope

### In Scope

- The `generic` webhook provider in the console, the API and the `TriggerSource` resource.
- Three verification methods: bearer token, HMAC-SHA256 and Standard Webhooks.
- An optional event type location in a header or a payload field.
- Repeat detection by delivery content.
- Free text event types and field paths on generic triggers.

---

## Product Decisions

**PD1: Fixed verification list:** An admin picks one of three methods and sets its options. kora runs no code or expression supplied by the admin. **Accepted cost:** a sender with a scheme outside the list needs a built-in provider.

**PD2: Content repeat detection:** kora treats a delivery with the same content as a repeat. **Accepted cost:** two genuinely separate events with identical bodies count as one delivery.

**PD3: Optional event type location:** A source can leave the event type location unset. **Accepted cost:** a source without one shows `webhook` as the event type of every delivery, and triggers on it filter by payload fields only.

**PD4: No schema:** Authors type event types and field paths. **Accepted cost:** a mistyped path is not caught when the trigger is saved and renders empty at run time.

**PD5: Two secret sources:** A generic source works with senders that accept a secret and with senders that issue their own. **Accepted cost:** setup has one more choice than a GitHub source.

### Decisions delegated to the ADR

The ADR will decide:

- How generic settings are stored on a source and in the `TriggerSource` resource.
- The exact Standard Webhooks behavior and replay window.
- How the content hash is built.
- How the event type is read from a header or a payload path.
- How trigger validation skips the schema for a generic source.

---

## Related Documents

- [Agent Webhook Triggers PRD](agent-webhook-triggers.md)
- [Trigger Payload Templating PRD](trigger-payload-templating.md)
- [Chat Command Triggers PRD](chat-command-triggers.md)
- [Declarative resources ADR](../adr/2026-10-24-declarative-resources.md)
- [Alertmanager webhook adapter ADR](../adr/2026-07-21-alertmanager-webhook-adapter.md)
