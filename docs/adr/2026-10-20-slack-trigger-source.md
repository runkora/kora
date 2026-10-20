# Chat Command Triggers

<!-- Filename: 2026-10-20-slack-trigger-source.md -->

**Status:** Proposed  
**Date:** 2026-10-20  
**Deciders:** Justin Wilkin  
**Technical Story:** [Chat Command Triggers PRD](../prd/slack-trigger-source.md)

---

## Context

### Background

The [Agent Triggers ADR](2026-06-17-agent-triggers-and-webhook-ingestion.md) defines the inbound pipeline and the `SourceAdapter` interface (`backend/internal/triggers/adapter.go`). The [Alertmanager ADR](2026-07-21-alertmanager-webhook-adapter.md) added the second adapter. `senderIdentifier` (`adapter.go`) is an optional capability that the pipeline checks by type assertion in `identifySender` (`service_ingest.go:348`). Only the GitHub adapter implements it, and its restriction vocabulary is GitHub's: `logins` and `associations` on `SenderRestriction` (`models.go:673`).

A trigger reads fields by dotted path from the decoded provider payload (`triggerFields` in `payload.go:66`). Each provider's schema file under `schemas/` declares those paths for the console. A chat trigger written against Slack's paths would not work on another chat platform.

Three parts of the pipeline do not fit Slack's HTTP Events API:

- `CreateSource` always generates the signing secret (`service_sources.go:84`), and `UpdateSource` regenerates it on rotation (`service_sources.go:178`). Slack issues the signing secret for an app, and the secret cannot be set.
- Slack confirms a request URL with a `url_verification` request that expects the `challenge` value back in an HTTP 200 ([url_verification](https://docs.slack.dev/reference/events/url_verification)). The request carries no `event_id`. `Service.Ingest` rejects it with `ErrMissingDeliveryID` (`service_ingest.go:53-62`), and the handler answers 404 (`server.go:185-190`).
- A Slack thread is `event.thread_ts` for a reply and `event.ts` for a top-level message ([Retrieving messages](https://docs.slack.dev/messaging/retrieving-messages)). Templates have no fallback syntax (`template.go`).

Trigger Routing (`docs/adr/2026-10-19-trigger-routing.md`) provides agent choice and a `join_key` that sends a follow-up to an open run. `CreateTrigger` sends only `KindWebhook` through `createWebhookTrigger`, the path that accepts routing. Scheduled and manual triggers reject it.

### Problem Statement

How should kora add a "Chat command" trigger kind whose triggers work unchanged on every chat platform, with Slack as the first platform, using the least change to the ingest pipeline, storage and routing?

### Goals

- A chat command trigger reads one kora chat schema with the events `mention` and `direct_message`.
- Chat sources reuse verification, deduplication, replay protection, delivery history and routing.
- One sender restriction of ids and provider-declared roles for webhook and chat triggers that keeps GitHub behavior unchanged.
- A Slack adapter in one file.

### Non-Goals

- A platform acknowledgement in the chat, runs that act as the chat user, Socket Mode, Kubernetes resources for sources, OAuth distribution to several workspaces and reaction events.
- Discord and Microsoft Teams adapters. Future Platforms lists what each would need.
- A Slack credentials connection.
- Replying to the chat. Agents reply through a Slack MCP server registered for the same Slack app. The chat source holds no Slack token.

### Which of the Three Questions This Touches

**How does it connect?** Inbound requests are verified with Slack's v0 request signature and a timestamp window. Outbound replies go through the Slack MCP server and the MCP gateway, and the chat source never sees the bot token.

**Who is this agent?** A chat-triggered run acts as the source's mapped SERVICE principal like every webhook-triggered run. The platform's user id stays in the stored payload for audit. Acting as the chat user is a later phase under [Delegated Agent Identity](2026-07-23-delegated-agent-identity.md).

**What can it access?** Unchanged. The source principal's grants bound every run. Anyone in a channel the bot is in, including Slack Connect external users, can fire a trigger that does not narrow by conversation or sender.

---

## Decision Drivers

- GitHub and Alertmanager triggers must fire for exactly the same deliveries as today.
- Routing must work for chat triggers without a change to the routing design.
- Messages that apps post must never start a run, including the agent's own replies.
- Unauthenticated callers must not learn whether a source id exists.
- A stored sender restriction must never load as empty and let every sender through.

---

## Considered Options

### Chat Kind Shape

**Option 1a (chosen):** "Chat command" is a console kind over webhook triggers on chat sources. The stored trigger kind stays `webhook`, and the provider decides whether a source is a chat source.

**Option 1b (rejected):** Add `KindChat` to the backend. Ingest, redelivery, `runPrincipal`, prompt rendering, the kind config codec and create all branch on `KindWebhook` (17 non-test references in `backend/internal/triggers` on main). Routing accepts only the webhook create path and would need a change of its own.

### Chat Fields

**Option 2a (chosen):** A chat adapter maps the decoded payload into one kora chat document. Fields, prompts, filters and join keys resolve against that document only.

**Option 2b (rejected):** Keep the provider payload and add the chat fields beside it through `fieldDeriver`. Provider paths would stay reachable, and triggers would drift onto them.

### Sender Restriction Storage

**Option 3a (chosen):** Rename the stored and API keys to `ids` and `roles`, and read the legacy `logins` and `associations` keys when a trigger loads.

**Option 3b (rejected):** Keep `logins` and `associations` in storage behind the new API names. The repository and the API would translate between two vocabularies permanently.

**Option 3c (rejected):** Rename the keys with no legacy read. A restriction stored under the old keys would load as empty and fire for every sender.

### Handshake Placement

**Option 4a (chosen):** Answer the handshake inside `Service.Ingest` after `Verify` succeeds.

**Option 4b (rejected):** Answer it in `handleIngest` before `Ingest`. An unsigned request would get a 200 for a real source id and a 404 for an unknown one.

### Dropping App Messages

**Option 5a (chosen):** The Slack adapter's `EventType` returns `""` for a message from an app. No trigger can have an empty event type (`service.go:393`), and the delivery records as matching no trigger.

**Option 5b (rejected):** Add a capability that drops the request before the delivery row. It adds code, and a dropped request leaves no audit row.

**Option 5c (rejected):** Leave app messages to each trigger's filters. A trigger that forgets the filter loops on the agent's own replies.

---

## Decision

**Chosen Options:** 1a, 2a, 3a, 4a and 5a. The sub-decisions follow.

### 1. Chat Command Kind

The backend stores and runs chat command triggers as `webhook` triggers. A provider is a chat provider when its adapter implements `chatAdapter`. Every existing `KindWebhook` path, routing included, applies to chat triggers. The console's trigger-kind registry (`kinds/index.ts`) gains a `chat` kind built from the planned "Chat command" tile. The chat kind lists Slack and shows Discord and Microsoft Teams as later, in the style of the existing planned kinds. The chat kind submits `kind: webhook` and passes every webhook-only gate in the wizard, routing included. The webhook kind stops listing chat providers and existing chat sources. The trigger list and drawer label a trigger "Chat command" when its source's provider is a chat provider, read from one frontend constant.

### 2. Chat Schema

`backend/internal/triggers/schemas/chat.json` is hand-written in the catalog format that `BuildCatalog` (`catalog.go:61`) reads. It declares the events `mention` and `direct_message` over one definition with these fields:

- `text`: the message text as the platform sends it.
- `user.id`: the platform's id for the sender.
- `conversation.id`: the channel or direct message conversation.
- `thread.id`: the thread within the conversation. A message outside a thread is its own thread.
- `workspace.id`: the platform's workspace.
- `platform`: the provider id, such as `slack`.

Every chat adapter returns this file from `Schema()`. `adapter.go` gains the chat capability:

```go
// chatAdapter is implemented by chat platform adapters. A trigger on a chat
// source reads the document ChatEvent returns, in the shape of schemas/chat.json.
type chatAdapter interface {
	// ChatEvent maps a decoded payload to the chat document. ok is false when
	// the payload carries no chat message.
	ChatEvent(eventType string, doc any) (event map[string]any, ok bool)
}
```

`fanOut` calls it once after `identifySender` and resolves every trigger's fields against the returned document. A payload that does not map resolves no fields. Match conditions, the prompt and its `<delivery-data>` block, the coalesce key and the routing `join_key` read the same map. `Redeliver` goes through `fanOut` and maps the stored payload again. The delivery row keeps the raw payload for audit. The meta fields `meta.event_type`, `meta.delivery_id` and `meta.source` still apply to chat triggers. `chatAdapter` replaces `fieldDeriver`.

### 3. Optional Adapter Capabilities

`issuedSecret` and `handshaker` are optional interfaces in `adapter.go`:

- **Issued secret:** `CreateSourceRequest` and `UpdateSourceRequest` carry a write-only `signing_secret`. It is valid only for an adapter that implements `issuedSecret`, on create and on an update that sets `rotate_secret`. Every other request that carries it fails with `ErrValidation`, including updates to GitHub and Alertmanager sources. For an issued-secret provider, create and rotate require it. The value is trimmed and must be non-empty and at most 256 bytes. `SourceResponse.Secret` stays empty for an issued secret, and the rotation cutoff in `redeliverWebhook` (`service_fire.go:332`) applies unchanged.
- **Handshake:** In `Service.Ingest`, between `Verify` (`service_ingest.go:44`) and `DeliveryID` (`service_ingest.go:53`), a `handshaker` adapter whose `Handshake` returns ok ends the request. It writes no delivery row, takes no replay key and leaves the source counters unchanged. `Ingest` returns an `IngestResult` that sits beside `IngestOutcome` in `service.go`, and `IngestHandshake` makes `handleIngest` answer 200 with the reply through `api.JSON`.

The kora MCP tools `triggers.sources.create` and `triggers.sources.update` (`backend/internal/koramcp/tools_triggers.go:74-98`) do not accept `signing_secret`. A secret must not pass through a model's context. An admin creates and rotates a Slack source in the console or through the REST API.

### 4. Sender Restriction

`Sender` becomes `{ID string, Roles []string}`, and `senderIdentifier` becomes the following interface:

```go
type senderIdentifier interface {
	// Sender reads the sender from the decoded body. ok is false when the event carries no sender.
	Sender(eventType string, body any) (s Sender, ok bool)
	// NormalizeID puts an id in the form Sender returns, for comparison.
	NormalizeID(id string) string
	// Roles lists the roles a restriction may name, with their labels.
	Roles() []SenderRole
}

// SenderRole is a role a provider reports, with the text the console shows.
type SenderRole struct {
	Value       string `json:"value"`
	Label       string `json:"label"`
	Description string `json:"description"`
}
```

- **Restriction:** `SenderRestriction` is `{ids, roles}`. A sender passes when its id is listed or any of its roles is listed. `normalizeRestriction` (`service_lifecycle.go:482`) validates roles against `Roles()` and normalizes ids through `NormalizeID`.
- **GitHub:** The id is the lowercased login and the roles hold the author association that `Sender` reads today. `Roles()` returns `OWNER`, `MEMBER` and `COLLABORATOR` with labels and descriptions. A GitHub trigger fires for the same senders as today.
- **Slack:** The id is `event.user` uppercased, and `Roles()` is empty.
- **Catalog:** `EventTypesResponse` replaces `associations` and `identifies_senders` with `senders`, an object holding `roles`. It is absent when the adapter does not implement `senderIdentifier`. The sender restriction editor shows when `senders` is present, shows the role control when `roles` is not empty and reads every label from the response.
- **Storage:** The restriction stays in `kind_config` JSON, and writes use `ids` and `roles`. The webhook config decode in `postgres.go:243` also reads `logins` into `ids` and `associations` into `roles`. No migration runs.
- **API:** `sender_restriction` takes `ids` and `roles` on create and update. A request that carries `logins` or `associations` fails with `ErrValidation`. The kora MCP tool arguments (`tools_triggers.go:307-358`) change with it.

### 5. Slack Verification and Events

`Verify` follows Slack's request signing scheme ([Verifying requests from Slack](https://docs.slack.dev/authentication/verifying-requests-from-slack)):

1.  Read `X-Slack-Request-Timestamp` as Unix seconds. Reject a missing or unparsable value, or one more than five minutes from the server clock.
2.  Compute HMAC-SHA256 with the signing secret over `v0:{timestamp}:{body}`.
3.  Compare `v0=` plus the lowercase hex digest with `X-Slack-Signature` using `hmac.Equal`.

Every failure returns `ErrSignatureInvalid` and the same 404 as today. The adapter takes its clock as a field and uses `time.Now` when the field is nil.

- **Handshake:** A body whose `type` is `url_verification` returns `{"challenge": <challenge>}` with status 200.
- **Delivery id:** `DeliveryID` returns the body's `event_id`, and the Events API documents it as globally unique. `app_rate_limited` carries no `event_id` and fails the existing `ErrMissingDeliveryID` guard, and that rejection is accepted.
- **Event type:** `EventType` returns `""` unless the body's `type` is `event_callback`. It returns `""` when `event.bot_id` is set or `event.subtype` is `bot_message`. `event.type` `app_mention` maps to `mention`. `event.type` `message` with `event.channel_type` `im` and no subtype or the `file_share` subtype maps to `direct_message`. Every other event returns `""`.
- **Retries:** Slack retries a delivery up to three times and marks each retry with `X-Slack-Retry-Num`. A retry carries the same `event_id`, and the partial unique index on `trigger_deliveries` records it as a duplicate. The adapter does not read the retry headers.

### 6. Slack Chat Mapping

`ChatEvent` maps the Slack payload as follows:

- `text` is `event.text`, including Slack's mention markup.
- `user.id` is `event.user`.
- `conversation.id` is `event.channel`.
- `thread.id` is `event.thread_ts` for a reply and `event.ts` for a top-level message.
- `workspace.id` is `team_id`.
- `platform` is `slack`.

A Slack `ts` is unique within one conversation only. A mention trigger's routing `join_key` is `{{ conversation.id }}:{{ thread.id }}`. A direct message trigger's `join_key` is `{{ conversation.id }}`, one key per direct message conversation. `schemas/slack.json` and the Slack `DerivedFields` are removed.

### 7. Slack App Manifest

`frontend/src/app/(dashboard)/triggers/_lib/slack-app-manifest.ts` builds a JSON [app manifest](https://docs.slack.dev/reference/app-manifest) from `useBrand().productName`:

- `display_information.name` is the product name, cut to 35 characters.
- `features.bot_user.display_name` is a slug of the product name in the characters Slack allows: `a-z`, `0-9`, `-`, `_` and `.`.
- `features.app_home.messages_tab_enabled` is true and `messages_tab_read_only_enabled` is false. People can then send the bot direct messages.
- `oauth_config.scopes.bot` lists `app_mentions:read` and `im:history` for the chat source. It lists `chat:write`, `channels:history`, `groups:history` and `users:read` for the agent to read the thread and reply.
- `settings.event_subscriptions.bot_events` lists `app_mention` and `message.im`, and the manifest has no `request_url`.
- `settings.socket_mode_enabled`, `org_deploy_enabled` and `token_rotation_enabled` are false.

The Slack MCP server uses this app's bot token (`xoxb-`) to post, and the admin adds any further scopes that server needs. The source step of the chat kind shows the manifest with a copy button and a signing secret field. The done step shows the real request URL to set under Event Subscriptions, and Slack's check of that URL is the handshake.

### 8. Acknowledgement Time

Slack requires a 2xx within three seconds. `Service.Ingest` records the delivery and enqueues each matched trigger inline (`service_ingest.go:123`). The run starts when the worker claims the fire (`service_ingest.go:332-334`), and routing decisions run in `DispatchPending`. The 202 therefore waits on database writes only and never on a run or a model call.

### Future Platforms

A Discord or Teams adapter implements `chatAdapter` with its own verification, handshake and event mapping. Discord delivers interactions to an HTTP endpoint signed with Ed25519 in `X-Signature-Ed25519` and `X-Signature-Timestamp`, sends a PING (`type: 1`) that expects a PONG, and expects a 401 for an invalid signature ([Discord interactions](https://docs.discord.com/developers/interactions/overview)). Its endpoint carries interactions such as slash commands, and a Discord adapter adds a `command` event to the chat schema. kora answers an invalid signature with 404. Teams delivers Bot Framework activities with a JWT in `Authorization` that the bot validates against keys from Bot Framework's OpenID metadata, with the bot's app id as the audience ([Bot Connector authentication](https://learn.microsoft.com/en-us/azure/bot-service/rest-api/bot-framework-rest-connector-authentication)). A Teams adapter needs a cached key fetch that `Verify` does not perform today.

---

## Consequences

### Positive

- A chat trigger's prompt, filters, sender restriction and join key carry over to every later chat platform.
- The console's sender restriction carries no provider text, and a new provider declares its own roles.
- Any future adapter can take a provider-issued secret or answer a handshake without a pipeline change.

### Negative

- API callers see chat command triggers as `webhook` triggers on a Slack source.
- Every message an app posts in a direct message with the bot, including the agent's replies, records a delivery that matches no trigger.
- A Slack MCP server that posts with a user token sends messages without `bot_id`. Those messages can start runs and loop.
- A channel follow-up reaches kora only when it mentions the bot. `message.channels` is not subscribed.
- `fanOut` bounds the inline enqueue with `dispatchTimeout` (60 seconds, `service.go:21`). A slow database past three seconds makes Slack retry, and the retry records as a duplicate.
- A Slack trigger with dedupe disabled starts a run for every Slack retry (`dispatchDuplicate` in `service_ingest.go`).
- Slack's signing secret regeneration has no grace window. Deliveries between the regeneration in Slack and the rotation in kora get 404, and Slack drops them after its retries.
- An admin configures two things for one Slack app: the chat source and the Slack MCP server.
- The legacy key read stays in the webhook config decode until no stored restriction uses the old keys.

### Neutral

- `sender_restriction` and the event types response change shape in the API.
- Chat sources appear in the existing Sources, Triggers and Deliveries surfaces.

---

## Implementation

The work maps onto the existing stack:

1.  **#641 (`feat/slack-trigger-capabilities`):** Replace `fieldDeriver` with `chatAdapter` and add `schemas/chat.json`. Add the generic `Sender`, `SenderRole` and `SenderRestriction`, the GitHub roles, the legacy key read in `postgres.go`, `senders` on `EventTypesResponse` and the kora MCP argument and description changes. Fake-adapter tests cover the capability call sites.
2.  **#643 (`feat/slack-trigger-adapter`):** Map Slack into the chat schema with `ChatEvent`, return `mention` and `direct_message`, implement the new `senderIdentifier`, and remove `schemas/slack.json` and `DerivedFields`.
3.  **#644 (`feat/slack-trigger-frontend`):** Add the chat kind with Slack and later platforms, move Slack out of the webhook kind, label chat triggers, read sender roles and labels from `senders`, drop `request_url` from the manifest and show the request URL on the done step, use chat placeholders, and add Storybook mocks and stories for the chat source setup and done step. The BFF passthroughs (`frontend/src/app/api/trigger-sources/route.ts`, `frontend/src/app/api/trigger-sources/[id]/route.ts`, `frontend/src/shared/api.ts` and `@runkora/server/triggers-client`) carry `signing_secret`, `senders` and the new `sender_restriction` keys.

---

## Validation

Unit tests cover:

- Slack verification with a valid signature, a wrong signature, a missing header and timestamps inside and outside five minutes.
- The handshake reply, a handshake with a bad signature and the absence of a delivery row.
- Slack event types for a mention, a direct message, a `file_share` direct message, an edited message, a message with `bot_id` and a `bot_message`.
- The Slack chat document for a top-level message and a thread reply, and a trigger that cannot read a raw Slack path.
- Sender restrictions by id and role for GitHub and Slack, and a stored restriction with legacy keys.
- Create and rotate with and without `signing_secret` for Slack, GitHub and Alertmanager.

---

## Related Decisions

- [Agent Triggers and Webhook Ingestion](2026-06-17-agent-triggers-and-webhook-ingestion.md)
- [Alertmanager Webhook Adapter](2026-07-21-alertmanager-webhook-adapter.md)
- [Trigger Payload Templating](2026-09-28-trigger-payload-templating.md)
- [Trigger Fire Coalescing](2026-10-13-trigger-fire-coalescing.md)
- [Delegated Agent Identity](2026-07-23-delegated-agent-identity.md)
- Trigger Routing (`docs/adr/2026-10-19-trigger-routing.md`)

---

## References

- [Events API](https://docs.slack.dev/apis/events-api/)
- [url_verification event](https://docs.slack.dev/reference/events/url_verification)
- [app_mention event](https://docs.slack.dev/reference/events/app_mention)
- [message.im event](https://docs.slack.dev/reference/events/message.im)
