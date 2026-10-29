# Generic Webhook Source

**Status:** Accepted  
**Date:** 2026-10-27  
**Deciders:** Justin Wilkin  
**Technical Story:** [Generic Webhook Source PRD](../prd/generic-webhook-source.md)

---

## Context

Each webhook provider is a Go type that implements `SourceAdapter` (`backend/internal/triggers/adapter.go`) and is registered in `NewRegistry`. GitHub, Alertmanager and Slack ship today. The four required methods are `Verify`, `DeliveryID`, `EventType` and `Schema`. None of them receives the source. An adapter behaves the same for every source of its provider.

The GitHub and Alertmanager adapters use only those four methods. Each reads a value from a header, a body field or a hash, and checks one signature scheme. A generic adapter covers the same ground when the header names, the scheme and the event type location come from the source.

The following parts already exist and need no change:

- `WebhookSource.Config` (`backend/internal/triggers/models.go`) is a JSON column that the API accepts on create and that no code reads today.
- The `TriggerSource` CRD (`backend/internal/platform/declarative/v1alpha1/types.go`) carries `spec.config` as raw JSON and `spec.signingSecretRef`. The declared path (`backend/internal/triggers/declared.go`) stores both.
- Only chat triggers are checked against a schema (`validateChatTrigger` in `backend/internal/triggers/service_lifecycle.go`). Webhook triggers already accept any event type and any field path.
- The event types endpoint returns the types a source has received as `observed` next to the schema's `declared` types.

### Security and Identity

The change touches how a source connects. Every delivery to a generic source is verified before any delivery row is written, and a request that fails verification moves only the source's rejection counter. The verification methods are a fixed list in Go. A source configures header names and encodings, and supplies no code or expression. Agent identity and grants are unchanged: a generic trigger runs as its agent under the trigger's existing grants. Generic sources do not identify a sender. Sender restriction is refused on them.

### Goals

- Add a `generic` provider whose verification and event type location come from the source's config.
- Create a generic source the same way through the API, the console and a `TriggerSource`.
- Keep the rejection of a delivery without a delivery id for every provider.
- Leave the built-in adapters unchanged.

### Non-Goals

- Schemas supplied by the admin and field pickers for generic sources.
- Verification methods beyond bearer, HMAC-SHA256 and Standard Webhooks.
- Chat platforms and sender identity on generic sources.
- Editing a generic source's config through the API after create.

---

## Decision Drivers

- **Built-in adapters stay untouched:** The change should not alter the methods GitHub, Alertmanager and Slack implement.
- **One path for API and CRD:** Config validation and secret handling should run in the same function for both create paths.
- **Closed verification:** A source chooses among methods implemented in Go and configures only their parameters.

---

## Decision 1: How the Adapter Reads Source Config

### Option 1A (chosen): Build an adapter per source from its config

An optional interface lets an adapter return a configured copy of itself for one source:

```go
// sourceConfigured is implemented by adapters whose behavior comes from the
// source's config.
type sourceConfigured interface {
	// ValidateConfig refuses a config the adapter cannot run with.
	ValidateConfig(config map[string]any) error
	// ForSource returns the adapter for one source.
	ForSource(config map[string]any) (SourceAdapter, error)
}
```

`Registry.ForSource(src WebhookSource) (SourceAdapter, error)` returns the registered adapter, or the result of `ForSource` when the adapter implements `sourceConfigured`. Ingest (`service_ingest.go`) and redelivery (`service_fire.go`) call `Registry.ForSource` in place of `Registry.Get` and pass the result to dispatch. The built-in adapters do not implement the interface and are returned as they are.

### Option 1B (rejected): Pass the config to every adapter method

`Verify`, `DeliveryID` and `EventType` would each take the source config. Every built-in adapter and its tests would change to accept a parameter they ignore.

### Option 1C (rejected): One provider per verification method

Providers such as `generic-bearer` and `generic-hmac` would fix the method in the provider id. The header names, encoding and event type location would still come from the source. The per-source config is needed in any case.

---

## Decision 2: Generic Source Config

The config is stored in `WebhookSource.Config` and has this shape:

```json
{
  "verification": {
    "method": "hmac_sha256",
    "header": "X-Signature",
    "prefix": "sha256=",
    "encoding": "hex"
  },
  "event_type": { "header": "X-Event-Key" }
}
```

The fields are as follows:

- `verification.method` is required and is one of `bearer`, `hmac_sha256` or `standard_webhooks`.
- `verification.header`, `verification.prefix` and `verification.encoding` apply only to `hmac_sha256`. `header` is required and must be a valid HTTP header name. `prefix` is optional and at most 32 bytes. `encoding` is `hex` or `base64` and defaults to `hex`.
- `event_type` is optional and holds exactly one of `header` or `path`. A `path` uses the same dotted segment grammar as template placeholders.
- An `event_type.header` cannot be `Authorization`, the configured signature header or one of the Standard Webhooks headers `webhook-id`, `webhook-timestamp` and `webhook-signature`.
- `header`, `prefix` and `encoding` are refused on `bearer` and `standard_webhooks`.
- A key that is not listed here, or that differs from a listed key only in case, is refused.

`ValidateConfig` runs on API create and on every declared `Apply`, through the shared `validateSourceCreate` path. A `TriggerSource` with the same config in `spec.config` produces the same source:

```yaml
apiVersion: runkora.io/v1alpha1
kind: TriggerSource
metadata:
  name: linear
  namespace: kora-tenant-example
spec:
  provider: generic
  config:
    verification:
      { method: hmac_sha256, header: Linear-Signature, encoding: hex }
    event_type: { path: type }
  signingSecretRef: { name: linear-webhook, key: secret }
```

`validateSourceCreate` takes the config as a third argument, and the `TriggerSource` CRD needs no schema change. `UpdateSourceRequest` keeps accepting only `enabled` and secret rotation, and a changed config through the console needs a new source. A declared source updates its config in place through `updateDeclaredSource`.

---

## Decision 3: Verification Methods

Each method compares in constant time and returns `ErrSignatureInvalid` on any failure:

- **`bearer`:** Compares the `Authorization: Bearer <token>` value with the secret, as the Alertmanager adapter does.
- **`hmac_sha256`:** Computes HMAC-SHA256 over the raw body keyed by the secret. It strips `prefix` from the configured header, decodes the rest with `encoding`, and compares the result.
- **`standard_webhooks`:** Implements the [Standard Webhooks](https://www.standardwebhooks.com/) signature. It signs `{webhook-id}.{webhook-timestamp}.{body}` with HMAC-SHA256 keyed by the base64-decoded secret after an optional `whsec_` prefix. It passes when any `v1,<base64>` entry in `webhook-signature` matches. It refuses a `webhook-timestamp` more than five minutes from the server clock in either direction, the same window as the Slack adapter.

---

## Decision 4: Secret Source

### Option 4A (chosen): The create request decides

The generic adapter accepts a supplied secret through `signing_secret` for `hmac_sha256` and `standard_webhooks`:

- **Supplied:** The secret is stored and never returned. A Standard Webhooks secret must decode as base64 after an optional `whsec_` prefix.
- **Omitted:** kora generates the secret and returns it once in the create response. A generated Standard Webhooks secret is `whsec_` followed by the standard base64 encoding of 32 random bytes.
- **Bearer:** kora always generates the secret and refuses a supplied one.

Rotation follows the same rule. `rotate_secret` with `signing_secret` stores the supplied secret, and `rotate_secret` alone generates one. A declared source always reads its secret from `signingSecretRef`.

`sourceSecret`, `newSecret` and `takesFirstSecret` in `backend/internal/triggers/service_sources.go` decide today by provider through the `issuedSecret` interface. Under `issuedSecret`, an omitted secret stores no secret. The generic adapter does not implement `issuedSecret`. A separate optional interface reports whether the configured adapter accepts a supplied secret and checks its format:

```go
// suppliedSecretChecker is implemented by configured adapters that accept a
// secret the admin supplies.
type suppliedSecretChecker interface {
	// CheckSuppliedSecret refuses a supplied secret the method cannot use.
	CheckSuppliedSecret(secret string) error
}
```

`sourceSecret`, `newSecret` and `takesFirstSecret` take the source, call `Registry.ForSource`, and use the checker. `declaredSource` in `backend/internal/triggers/declared.go` calls the same checker on the secret from `signingSecretRef`. A declared bearer source accepts its Secret value.

### Option 4B (rejected): Store the secret source in config

A `secret_source: generated | supplied` key would record the choice. The request already carries the choice, and the stored key would drive no behavior after create.

---

## Decision 5: Delivery ID and Event Type

- **Delivery ID:** `DeliveryID` returns the hex SHA-256 of the raw body. A delivery with an empty body has no id and is refused by the existing `ErrMissingDeliveryID` check. The Standard Webhooks `webhook-id` header is not used as the delivery id.
- **Event type:** `EventType` returns the configured header value, or the value at the configured path in the decoded JSON body through `resolvePath` (`backend/internal/triggers/payload.go`). With no `event_type` config, the event type is `webhook` for every delivery. When the configured header or path is missing, or the body is not JSON, the event type is empty and matches no trigger. Trigger create refuses an empty event type (`service.go`), and the fixed `webhook` value gives triggers on such a source an event type to match.

---

## Decision 6: Catalog and Trigger Validation

`Schema` returns nil for the generic adapter, and `Registry.register` stores an empty `Catalog` for an adapter with no schema. The event types endpoint returns an empty `declared` list with the source's `observed` types, and the fields endpoint returns only the `meta` fields.

Trigger create and update on a generic source keep the existing webhook checks: the event type is free text, and `validateFieldKeys` checks each referenced field path against the placeholder grammar. `normalizeRestriction` already refuses a sender restriction for an adapter without `senderIdentifier`.

`truncate` in `service.go` returns a body of 512 bytes or less unchanged as `payload_excerpt`. A Postgres `TEXT` column refuses a body that is not valid UTF-8 or that holds a NUL byte, and a generic source can receive either. `truncate` will clean every excerpt.

---

## Decision 7: Console

- The new-trigger screen offers "Generic webhook" in the webhook kind next to GitHub and Alertmanager.
- The source step asks for the verification method and, for `hmac_sha256`, the header, prefix and encoding.
- For `hmac_sha256` and `standard_webhooks`, the source step asks up front whether kora generates the secret or the admin pastes one.
- The source step offers an optional event type location: none, a header or a payload path.
- The trigger step and the trigger detail modal take a free text event type with the source's observed types as suggestions. The event type and filter field pickers (`CatalogPicker` in `trigger-wizard/kinds/webhook.tsx`, `filter-rows-editor.tsx` and `event-filters-section.tsx`) accept a typed value when the source has no declared events.
- The sender restriction is hidden for generic sources.
- The provider list in `_lib/providers.ts` gains `generic`.

The console files are under `frontend/src/app/(dashboard)/triggers/`.

---

## Rationale

None at this time.

---

## Consequences

### Positive

- A new system that signs with bearer, HMAC-SHA256 or Standard Webhooks needs no kora release.
- The built-in adapters and the ingest pipeline keep their current behavior.

### Negative

- Two separate events with identical bodies count as one delivery.
- A mistyped field path on a generic trigger saves and renders empty at run time.
- A generic source's config cannot be edited in the console after create.

### Neutral

- Ingest builds a generic adapter from config on each delivery.

---

## Implementation

1.  **Backend:** The backend change will:
    - Add `adapter_generic.go` with the config type, `ValidateConfig`, `ForSource` and the three verification methods.
    - Add `Registry.ForSource` and use it in ingest, redelivery and secret handling, including the declared path.
    - Store an empty catalog for an adapter with no schema and clean every delivery excerpt.
    - Update `docs/guides/create-a-trigger.md` and `docs/guides/declarative-resources.md` with the generic source.
2.  **Frontend:** Add the generic option, its source step and the trigger step changes from Decision 7.
3.  **Verification:** On the local cluster, create a generic source of each method through the API and through a `TriggerSource`, send signed and unsigned deliveries, and confirm runs start only for verified deliveries that match.

---

## Related Decisions

- [Agent Triggers and Webhook Ingestion](2026-06-17-agent-triggers-and-webhook-ingestion.md)
- [Alertmanager Webhook Adapter](2026-07-21-alertmanager-webhook-adapter.md)
- [Trigger Payload Templating](2026-09-28-trigger-payload-templating.md)
- [Chat Command Triggers](2026-10-20-chat-command-triggers.md)
- [Declarative Resources](2026-10-24-declarative-resources.md)
