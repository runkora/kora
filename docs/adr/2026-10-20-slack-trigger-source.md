# Slack Trigger Source

<!-- Filename: 2026-10-20-slack-trigger-source.md -->

**Status:** Proposed  
**Date:** 2026-10-20  
**Deciders:** Justin Wilkin  
**Technical Story:** [Slack Trigger Source PRD](../prd/slack-trigger-source.md)

---

## Context

### Background

The [Agent Triggers ADR](2026-06-17-agent-triggers-and-webhook-ingestion.md) defines the inbound pipeline and the `SourceAdapter` interface (`backend/internal/triggers/adapter.go`). The [Alertmanager ADR](2026-07-21-alertmanager-webhook-adapter.md) added the second adapter. `senderIdentifier` (`adapter.go`) is an optional capability that the pipeline checks by type assertion in `identifySender` (`service_ingest.go:348`). Only the GitHub adapter implements it.

Three parts of the current pipeline do not fit Slack's HTTP Events API:

- `CreateSource` always generates the signing secret (`service_sources.go:84`), and `UpdateSource` regenerates it on rotation (`service_sources.go:178`). Slack issues the signing secret for an app, and the secret cannot be set.
- Slack confirms a request URL with a `url_verification` request that expects the `challenge` value back in an HTTP 200 ([url_verification](https://docs.slack.dev/reference/events/url_verification)). The request carries no `event_id`. `Service.Ingest` rejects it with `ErrMissingDeliveryID` (`service_ingest.go:54`), and the handler answers 404 (`server.go:185-190`).
- A Slack thread is `event.thread_ts` for a reply and `event.ts` for a top-level message ([Retrieving messages](https://docs.slack.dev/messaging/retrieving-messages)). Templates have no fallback syntax (`template.go`). A routing `join_key` therefore cannot express the thread from payload paths alone.

Trigger Routing (`docs/adr/2026-10-19-trigger-routing.md`, Option 6a) provides agent choice and the `join_key` that sends a follow-up to an open run. This ADR relies on it and does not change it.

### Problem Statement

How should a Slack adapter verify requests, answer the URL check, identify events and expose the thread, with platform changes that are generic optional capabilities and that leave GitHub and Alertmanager unchanged?

### Goals

- A Slack adapter for the `app_mention` and `message.im` events in one file, `adapter_slack.go`.
- Three optional adapter capabilities: a secret the provider issues, a handshake reply and derived fields.
- No change to the deliveries schema, the scopes model or the GitHub and Alertmanager behavior.

### Non-Goals

- A platform acknowledgement in Slack, runs that act as the Slack user, Socket Mode, Kubernetes resources for sources, OAuth distribution to several workspaces, Teams, Discord and reaction events.
- A chat-platform abstraction.
- A Slack credentials connection.
- Replying to Slack. Agents reply through a Slack MCP server registered for the same Slack app. That server holds the bot token. The trigger source holds no Slack token.

### Which of the Three Questions This Touches

**How does it connect?** Inbound requests are verified with Slack's v0 request signature and a timestamp window. Outbound replies go through the Slack MCP server and the MCP gateway, and the trigger source never sees the bot token.

**Who is this agent?** A Slack-triggered run acts as the source's mapped SERVICE principal like every webhook-triggered run. The Slack user id stays in the stored payload for audit. Acting as the Slack user is a later phase under [Delegated Agent Identity](2026-07-23-delegated-agent-identity.md).

**What can it access?** Unchanged. The source principal's grants bound every run. Anyone in a channel the bot is in, including Slack Connect external users, can fire a trigger that does not narrow by channel or sender.

---

## Decision Drivers

- GitHub and Alertmanager must behave exactly as today.
- Each platform change must be usable by any future adapter and cost nothing to adapters that do not use it.
- Messages that apps post must never start a run, including the agent's own replies.
- Unauthenticated callers must not learn whether a source id exists.

---

## Considered Options

### Capability Shape

**Option 1a (chosen):** Add three small optional interfaces in `adapter.go`, each checked by type assertion like `senderIdentifier`.

**Option 1b (rejected):** Add the three methods to `SourceAdapter`. GitHub and Alertmanager would gain no-op methods for capabilities they do not have.

**Option 1c (rejected):** Branch on `ProviderSlack` in `Service.Ingest`, `CreateSource` and `fanOut`. The pipeline would carry provider conditionals, and the next provider with the same needs would add more.

### Handshake Placement

**Option 2a (chosen):** Answer the handshake inside `Service.Ingest` after `Verify` succeeds.

**Option 2b (rejected):** Answer it in `handleIngest` before `Ingest`. An unsigned request would get a 200 for a real source id and a 404 for an unknown one, and that difference reveals which source ids exist.

### Dropping App Messages

**Option 3a (chosen):** The Slack adapter's `EventType` returns `""` for a message from an app. No trigger can have an empty event type (`service.go:393`), and the delivery records as matching no trigger.

**Option 3b (rejected):** Add a fourth capability that drops the request before the delivery row. It adds code outside the three capabilities, and a dropped request leaves no audit row.

**Option 3c (rejected):** Leave app messages to each trigger's match condition. A trigger that forgets the condition loops on the agent's own replies.

### Thread Field

**Option 4a (chosen):** The adapter derives `thread` and the pipeline merges it into the field map.

**Option 4b (rejected):** Add fallback syntax to templates. It changes the template grammar for every provider to serve one field.

### Manifest Location

**Option 5a (chosen):** A frontend module builds the manifest from the brand's product name.

**Option 5b (rejected):** Embed the manifest in the backend and serve it from a new endpoint. A new route would serve static text that only the console reads.

---

## Decision

**Chosen Options:** 1a, 2a, 3a, 4a and 5a. The Slack adapter follows them.

### 1. Optional Adapter Capabilities

`adapter.go` gains three unexported interfaces:

```go
// issuedSecret is implemented by adapters whose provider issues the signing
// secret. Their sources take the secret from the admin at create and rotate.
type issuedSecret interface {
	providerIssuesSecret()
}

// handshaker is implemented by adapters whose provider sends a verification
// request that expects a reply and is not a delivery.
type handshaker interface {
	// Handshake returns the reply for a handshake request. ok is false for a delivery.
	Handshake(body []byte) (reply map[string]string, ok bool)
}

// fieldDeriver is implemented by adapters that compute fields no single
// payload path holds.
type fieldDeriver interface {
	// DerivedFields returns field key to value. Keys never start with "meta.".
	DerivedFields(eventType string, doc any) map[string]string
}
```

The call sites are:

- **Issued secret at create:** `CreateSourceRequest` and `UpdateSourceRequest` (`models.go`) gain a write-only `signing_secret`. At `service_sources.go:84`, an adapter that implements `issuedSecret` requires a non-empty `signing_secret` and encrypts it. Any other adapter rejects `signing_secret` with `ErrValidation` and generates the secret as today. `SourceResponse.Secret` stays empty for an issued secret.
- **Issued secret at rotate:** At `service_sources.go:178`, `rotate_secret` on an issued-secret source requires `signing_secret` and stores it through the existing rotation path. `secret_rotated_at` and the redelivery cutoff in `redeliverWebhook` (`service_fire.go:332`) apply unchanged.
- **Handshake:** In `Service.Ingest`, between `Verify` (`service_ingest.go:44`) and `DeliveryID` (`service_ingest.go:53`), a `handshaker` adapter whose `Handshake` returns ok ends the request. The handshake writes no delivery row, takes no replay key and leaves the source counters unchanged. `Ingest` returns an `IngestResult` with the `IngestOutcome` and the reply. A new `IngestHandshake` outcome makes `handleIngest` (`server.go:197-201`) answer 200 with the reply through `api.JSON`.
- **Derived fields:** `fanOut` computes the derived fields once beside `identifySender` (`service_ingest.go:268`). No fields are derived for a nil `doc`. `triggerFields` (`payload.go:66`) takes them and copies them after the referenced payload paths. A derived key overrides a payload path with the same key. Match conditions, the prompt, the coalesce key and the routing `join_key` read the same map. `Redeliver` goes through `fanOut` and gets the same fields.

GitHub and Alertmanager implement none of the three, and every call site keeps its current path for them.

### 2. Verification

`Verify` follows Slack's request signing scheme:

1.  Read `X-Slack-Request-Timestamp` as Unix seconds. Reject a missing or unparsable value, or one more than five minutes from the server clock.
2.  Compute HMAC-SHA256 with the signing secret over `v0:{timestamp}:{body}`.
3.  Compare `v0=` plus the lowercase hex digest with `X-Slack-Signature` using `hmac.Equal`.

Every failure returns `ErrSignatureInvalid`. The source counts it as an invalid signature, and the caller gets the same 404 as today. The adapter takes its clock as a field for tests.

### 3. Events and Delivery ID

- **Handshake:** A body whose `type` is `url_verification` returns `{"challenge": <challenge>}`. Slack accepts a JSON reply with status 200.
- **Delivery id:** `DeliveryID` returns the body's `event_id`. Slack's Events API documents it as globally unique. A body without one, such as `app_rate_limited`, fails the existing `ErrMissingDeliveryID` guard.
- **Event type:** `EventType` returns `""` unless the body's `type` is `event_callback`. It returns `""` when `event.bot_id` is set or `event.subtype` is `bot_message`. Otherwise `event.type` `app_mention` maps to `app_mention`. `event.type` `message` with `event.channel_type` `im` and no subtype or the `file_share` subtype maps to `message.im`, the Slack event subscription name. Every other event returns `""`.
- **Retries:** Slack retries a delivery up to three times and marks each retry with `X-Slack-Retry-Num`. A retry carries the same `event_id`. The partial unique index on `trigger_deliveries` records it as a duplicate, and the handler answers 200 (`server.go:197`). The adapter does not read the retry headers.

### 4. Sender

The adapter implements `senderIdentifier`. `Sender` returns `event.user` and reports no sender when the field is empty. `NormalizeLogin` uppercases the id, and `Associations` returns an empty list. A trigger's sender restriction lists Slack user ids.

### 5. Derived Fields

The adapter implements `fieldDeriver` and returns a single `thread` field that holds `event.thread_ts` for a reply and `event.ts` for a top-level message. A `ts` is unique within one conversation. A routing `join_key` that identifies a thread combines both keys: `{{ event.channel }}:{{ thread }}`. `team_id` and `event.channel` are plain payload paths and are not derived.

### 6. Schema

`backend/internal/triggers/schemas/slack.json` is hand-written in the catalog format that `BuildCatalog` (`catalog.go:61`) reads. It declares the events `app_mention` and `message.im` over one envelope definition with `team_id`, `api_app_id`, `event_id`, `event_time` and `thread`. The `thread` description states that kora derives it. The `event` object declares `type`, `user`, `text`, `ts`, `thread_ts`, `channel` and `event_ts`. `message.im` adds `channel_type` with the enum `im`. The adapter embeds it like `schemas/alertmanager.json`, and `NewRegistry` registers `&slackAdapter{}`. `models.go` gains `ProviderSlack = "slack"`.

### 7. App Manifest

`frontend/src/app/(dashboard)/triggers/_lib/slack-app-manifest.ts` builds a JSON app manifest from `useBrand().productName`:

- `display_information.name` and `features.bot_user.display_name` are the product name.
- `features.app_home.messages_tab_enabled` is true and `messages_tab_read_only_enabled` is false. People can then send the bot direct messages.
- `oauth_config.scopes.bot` lists `app_mentions:read` and `im:history`.
- `settings.event_subscriptions.bot_events` lists `app_mention` and `message.im`.
- `settings.event_subscriptions.request_url` is a placeholder built from the ingest host and the webhook path with `SOURCE_ID` in place of the source id.
- `settings.socket_mode_enabled`, `org_deploy_enabled` and `token_rotation_enabled` are false.

The scopes for replying belong to the Slack MCP server and are not in this manifest. When Slack is the chosen provider, the new-source step in `kinds/webhook.tsx` shows the manifest with a copy button and a signing secret field. The done step shows the request URL to set in the app. Slack requires a manual check of a request URL set in a manifest, and that check is the handshake.

### 8. Acknowledgement Time

Slack requires a 2xx within three seconds. `Service.Ingest` records the delivery and enqueues each matched trigger inline (`service_ingest.go:123`). The run starts when the worker claims the fire (`service_ingest.go:332-334`), and routing decisions run in `DispatchPending`. The 202 therefore waits on database writes only and never on a run or a model call.

---

## Consequences

### Positive

- Any future adapter can take a provider-issued secret, answer a handshake or add derived fields without a pipeline change.
- People start agent runs from Slack mentions and direct messages, and follow-ups in a thread can join the run through routing.

### Negative

- Every message an app posts in a direct message with the bot, including the agent's replies, records a delivery that matches no trigger.
- A channel follow-up reaches kora only when it mentions the bot. `message.channels` is not subscribed.
- `fanOut` bounds the inline enqueue with `dispatchTimeout` (60 seconds, `service.go:21`). A slow database past three seconds makes Slack retry, and the retry records as a duplicate.
- A Slack trigger with dedupe disabled starts a run for every Slack retry (`dispatchDuplicate` in `service_ingest.go`).
- An admin configures two things for one Slack app: the trigger source and the Slack MCP server.

### Neutral

- `Service.Ingest` returns `IngestResult`. The handler and the tests change with it.
- Slack sources appear in the existing Sources, Triggers and Deliveries surfaces.

---

## Implementation

1.  `backend/internal/triggers/adapter.go`: the three capability interfaces and the Slack registration.
2.  `backend/internal/triggers/adapter_slack.go` and `schemas/slack.json`: the adapter and its schema.
3.  `backend/internal/triggers/models.go`: `ProviderSlack`, `signing_secret` on the create and update requests, and `IngestResult`.
4.  `backend/internal/triggers/service_sources.go`: the issued-secret paths in `CreateSource` and `UpdateSource`.
5.  `backend/internal/triggers/service_ingest.go`, `payload.go` and `server.go`: the handshake, the derived fields and the handshake response.
6.  `frontend/src/app/(dashboard)/triggers/`: the manifest module, Slack in the provider list, the secret field, the request URL on the done step and a secret input on rotation for Slack sources.

---

## Validation

Unit tests cover:

- Verification with a valid signature, a wrong signature, a missing header and timestamps inside and outside five minutes.
- The handshake reply, a handshake with a bad signature and the absence of a delivery row.
- Event types for a mention, a direct message, a `file_share` direct message, an edited message, a message with `bot_id` and a `bot_message`.
- `thread` for a top-level message and a thread reply, and a derived key that overrides a payload path.
- Create and rotate with and without `signing_secret` for Slack, GitHub and Alertmanager.

---

## Related Decisions

- [Agent Triggers and Webhook Ingestion](2026-06-17-agent-triggers-and-webhook-ingestion.md)
- [Alertmanager Webhook Adapter](2026-07-21-alertmanager-webhook-adapter.md)
- [Trigger Payload Templating](2026-09-28-trigger-payload-templating.md)
- [Trigger Fire Coalescing](2026-10-13-trigger-fire-coalescing.md)
- [Delegated Agent Identity](2026-07-23-delegated-agent-identity.md)
- Trigger Routing (`docs/adr/2026-10-19-trigger-routing.md`, on the trigger routing stack)

---

## References

- [Verifying requests from Slack](https://docs.slack.dev/authentication/verifying-requests-from-slack)
- [Events API](https://docs.slack.dev/apis/events-api/)
- [url_verification event](https://docs.slack.dev/reference/events/url_verification)
- [app_mention event](https://docs.slack.dev/reference/events/app_mention)
- [message.im event](https://docs.slack.dev/reference/events/message.im)
- [App manifest reference](https://docs.slack.dev/reference/app-manifest)
- [Retrieving messages](https://docs.slack.dev/messaging/retrieving-messages)
