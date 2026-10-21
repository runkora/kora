# Chat Command Triggers

<!-- Filename: 2026-10-20-chat-command-triggers.md -->

**Status:** Accepted  
**Date:** 2026-10-20  
**Deciders:** Justin Wilkin  
**Technical Story:** [Chat Command Triggers PRD](../prd/chat-command-triggers.md)

---

## Context

### Background

The [Agent Triggers ADR](2026-06-17-agent-triggers-and-webhook-ingestion.md) defines the inbound pipeline and the `SourceAdapter` interface (`backend/internal/triggers/adapter.go`). The [Alertmanager ADR](2026-07-21-alertmanager-webhook-adapter.md) added the second adapter. `senderIdentifier` (`adapter.go`) is an optional capability that the pipeline checks by type assertion in `identifySender` (`service_ingest.go`). Only the GitHub adapter implements it, and its restriction vocabulary is GitHub's: `logins` and `associations` on `SenderRestriction` (`models.go`).

A trigger reads fields by dotted path from the decoded provider payload (`triggerFields` in `payload.go`). Each provider's schema file under `schemas/` declares those paths for the console. A chat trigger written against Slack's paths would not work on another chat platform.

Three parts of the pipeline do not fit Slack's HTTP Events API:

- `CreateSource` always generates the signing secret, and `UpdateSource` regenerates it on rotation (`service_sources.go`). Slack issues the signing secret for an app, and the secret cannot be set.
- Slack needs a working request URL before the admin can finish the app's event subscriptions, and kora issues the URL only when the source exists. A wizard that asks for the signing secret first sends the admin to Slack, back to kora and to Slack again.
- Slack confirms a request URL with a `url_verification` request that expects the `challenge` value back in an HTTP 200 ([url_verification](https://docs.slack.dev/reference/events/url_verification)). The request carries no `event_id`. `Service.Ingest` rejects it with `ErrMissingDeliveryID`, and `handleIngest` answers 404.
- A Slack thread is `event.thread_ts` for a reply and `event.ts` for a top-level message ([Retrieving messages](https://docs.slack.dev/messaging/retrieving-messages)). Templates have no fallback syntax (`template.go`).

[Trigger Routing](2026-10-19-trigger-routing.md) provides agent choice and a `join_key` that sends a follow-up to an open run. Routing applies to webhook triggers only.

### Problem Statement

How should kora add a "Chat command" trigger kind whose triggers work on every chat platform that delivers the same events, with Slack as the first platform, using the least change to the ingest pipeline, storage and routing?

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
- Chat triggers must get routing without a change to the routing design.
- Messages that Slack marks as from an app (`bot_id` or the `bot_message` subtype) must never start a run.
- A chat payload that kora cannot read must never start or join a run.
- Unauthenticated callers must not learn whether a source id exists.
- A sender restriction must never load or save as empty and let every sender through.

---

## Considered Options

### Chat Kind Shape

**Option 1a (chosen):** "Chat command" is a console kind over webhook triggers on chat sources. The stored trigger kind stays `webhook`, and the adapter decides whether a source is a chat source.

**Option 1b (rejected):** Add `KindChat` to the backend. Ingest, redelivery, `runPrincipal`, prompt rendering, the kind config codec and trigger creation all branch on `KindWebhook`, and routing accepts only webhook triggers.

### Chat Fields

**Option 2a (chosen):** A chat adapter maps the decoded payload into one kora chat document. Fields, prompts, filters and join keys resolve against that document only.

**Option 2b (rejected):** Keep the provider payload and add the chat fields beside it. Provider paths would stay reachable, and triggers would drift onto them.

### Sender Restriction Storage

**Option 3a (rejected):** Rename the stored keys to `ids` and `roles` and read the legacy keys on load. A rollback to a build that reads only `logins` and `associations` would load new rows as empty restrictions.

**Option 3b (chosen):** Keep `logins` and `associations` in storage behind the new API names. The repository maps between the two at encode and decode.

### Handshake Placement

**Option 4a (rejected):** Answer the handshake inside `Service.Ingest` after `Verify` succeeds for every source. A source cannot pass `Verify` before it holds a signing secret, and the secret can be copied only after Slack accepts the request URL.

**Option 4b (rejected):** Answer it in `handleIngest` before `Ingest` for every source. An unsigned request would get a 200 for a real source id and a 404 for an unknown one.

**Option 4c (chosen):** Answer the handshake after `Verify` for a source with a signing secret. For a source without one, answer only the handshake, unsigned, and reject every other request. This accepts the trade-off recorded under Decision 5.

### Dropping App Messages

**Option 5a (chosen):** The Slack adapter's `EventType` returns `""` for a message from an app. `validateWebhookTriggerCreate` (`service.go`) refuses an empty event type, and the delivery records as matching no trigger.

**Option 5b (rejected):** Add a capability that drops the request before the delivery row. It adds code, and a dropped request leaves no audit row.

**Option 5c (rejected):** Leave app messages to each trigger's filters. A trigger that forgets the filter loops on the agent's own replies.

---

## Decision

**Chosen Options:** 1a, 2a, 3b, 4c and 5a. The sub-decisions follow.

### 1. Chat Command Kind

The backend stores and runs chat command triggers as `webhook` triggers. A source is a chat source when its adapter implements `chatAdapter`. Every `KindWebhook` path applies to chat triggers, routing included.

- **Category:** Source responses (create, get, list and update) and `EventTypesResponse` carry a read-only `category` of `chat` or `webhook`, set by type assertion on `chatAdapter`. The console reads it in place of a provider list.
- **Validation:** For a trigger on a chat source, create and update refuse an event type outside `chat.json`, and any field that the prompt, a match condition, the coalesce key or the routing `join_key` references and that is neither a `chat.json` field nor a meta field. A routing update runs the same check.
- **Observed events:** On a chat source, `observed` in the source event types response lists only `chat.json` event types.
- **Console:** The trigger-kind registry (`kinds/index.ts`) gains a `chat` entry over the webhook kind's module with a category filter. The `chat` id exists only in the console, and its `apiKind` is `webhook`. The webhook-only gates in the wizard, routing included, test `apiKind`. The chat entry lists chat providers and shows Discord and Microsoft Teams as later, in the style of the planned kinds. The webhook entry lists webhook providers and webhook-category sources.
- **Triggers page:** A trigger on a chat source shows as "Chat command". The kind filter gains a Chat command option with its own count, and the Webhook count excludes chat triggers.
- **kora MCP:** The trigger and source tool descriptions in `backend/internal/koramcp/tools_triggers.go` cover chat sources, the chat events and `category`.

### 2. Chat Schema

`backend/internal/triggers/schemas/chat.json` is hand-written in the catalog format that `BuildCatalog` (`catalog.go`) reads. It declares the events `mention` and `direct_message` over one definition with these fields:

- `text`: the message text as the platform sends it. It can be empty for a message that carries only a file.
- `user.id`: the platform's id for the sender.
- `conversation.id`: the channel or direct message conversation.
- `conversation.type`: `channel`, `private`, `dm` or `group`.
- `thread.id`: the thread within the conversation. A message outside a thread is its own thread.
- `thread.started_by.id`: the id of the user who posted the thread's first message. It is the app's own id when the app started the thread.
- `message.id`: the platform's id for the message that fired the event. It equals `thread.id` outside a thread and differs for a reply.
- `workspace.id`: the platform's workspace.
- `platform`: the provider id, such as `slack`.

`NewRegistry` builds the catalog of every adapter that implements `chatAdapter` from this one file. `adapter.go` declares the chat capability:

```go
// chatAdapter is implemented by chat platform adapters. A trigger on a chat
// source reads the document ChatEvent returns, in the shape of schemas/chat.json.
type chatAdapter interface {
	// ChatEvent maps a decoded payload to the chat document. ok is true only
	// when user.id, conversation.id and thread.id are all non-empty.
	ChatEvent(eventType string, doc any) (event map[string]any, ok bool)
}
```

`fanOut` calls `ChatEvent` once after `identifySender` and resolves every trigger's fields against the returned document. When the decoded body is nil or `ChatEvent` returns ok false, no trigger on the chat source matches, and the delivery records as matching no trigger. Match conditions, the prompt and its `<delivery-data>` block, the coalesce key and the routing `join_key` read the same map. The meta fields `meta.event_type`, `meta.delivery_id` and `meta.source` apply to chat triggers. `Redeliver` goes through `fanOut` and maps the stored payload again. The delivery row keeps the raw payload for audit.

### 3. Optional Adapter Capabilities

`issuedSecret` and `handshaker` are optional interfaces in `adapter.go`:

- **Issued secret:** `CreateSourceRequest` and `UpdateSourceRequest` carry a write-only `signing_secret`. It is valid only for an adapter that implements `issuedSecret`. Every other request that carries it fails with `ErrValidation`, including updates to GitHub and Alertmanager sources. For an issued-secret provider, create may omit it, and the source is stored without a secret. `UpdateSource` sets the first secret on a source that has none without `rotate_secret`. Once a secret is set, a change needs `rotate_secret` and the new value. `SourceResponse` carries a read-only `signing_secret_set` that tells the console a source is waiting for its secret. The value is trimmed and must be non-empty and at most 256 bytes. `SourceResponse.Secret` stays empty for an issued secret, and the rotation cutoff in `redeliverWebhook` (`service_fire.go`) applies unchanged.
- **Handshake:** In `Service.Ingest`, between `Verify` and `DeliveryID`, a `handshaker` adapter whose `Handshake` returns ok ends the request. A source without a signing secret skips `Verify` and takes the unsigned path in Decision 5. It writes no delivery row, takes no replay key and leaves the source counters unchanged. `Ingest` returns an `IngestResult` that sits beside `IngestOutcome` in `service.go`, and `IngestHandshake` makes `handleIngest` answer 200 with the reply through `api.JSON`.

The kora MCP tools `triggers.sources.create` and `triggers.sources.update` do not accept `signing_secret`. A secret must not pass through a model's context. An agent can create a chat source through `triggers.sources.create`. The source is stored without a signing secret and waits for one, and an admin sets the secret in the console or through the REST API. The same applies to rotation.

### 4. Sender Restriction

`Sender` is `{ID string, Roles []string}`, and `senderIdentifier` is the following interface:

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

- **Restriction:** `SenderRestriction` is `{ids, roles}`. A sender passes when its id is listed or any of its roles is listed. `normalizeRestriction` (`service_lifecycle.go`) validates roles against `Roles()` and normalizes ids through `NormalizeID`.
- **GitHub:** The id is the lowercased login, and the roles hold the author association that `Sender` reads today. `Roles()` returns `OWNER`, `MEMBER` and `COLLABORATOR` with labels and descriptions. A GitHub trigger fires for the same senders as today.
- **Slack:** The id is `event.user` uppercased, the form Slack issues ids in, and `Roles()` is empty. `ChatEvent` sets `user.id` to the same value.
- **Catalog:** `EventTypesResponse` carries `senders`, an object holding `roles`, in place of `associations`. It is absent when the adapter does not implement `senderIdentifier`. The sender restriction editor shows when `senders` is present, shows the role control when `roles` is not empty and reads every label from the response.
- **Storage:** The restriction stays in `kind_config` JSON under `logins` and `associations` through a private storage struct. `marshalKindConfig` maps `ids` and `roles` into it, and `decodeKindConfig` maps it back (`postgres.go`). No migration runs.
- **API:** `sender_restriction` takes `ids` and `roles` on create and update. `api.DecodeJSON` ignores unknown keys. `SenderRestriction.UnmarshalJSON` therefore fails on any key other than `ids` and `roles`, and the request answers 400. A misspelt key or a legacy `logins` or `associations` key cannot clear a stored restriction. The kora MCP tool arguments use `ids` and `roles`.

### 5. Slack Verification and Events

`Verify` follows Slack's request signing scheme ([Verifying requests from Slack](https://docs.slack.dev/authentication/verifying-requests-from-slack)):

1.  Read `X-Slack-Request-Timestamp` as Unix seconds. Reject a missing or unparsable value, or one more than five minutes from the server clock.
2.  Compute HMAC-SHA256 with the signing secret over `v0:{timestamp}:{body}`.
3.  Compare `v0=` plus the lowercase hex digest with `X-Slack-Signature` using `hmac.Equal`.

Every failure returns `ErrSignatureInvalid` and the same 404 as today. The adapter takes its clock as a field and uses `time.Now` when the field is nil.

- **Handshake:** A body whose `type` is `url_verification` returns `{"challenge": <challenge>}` with status 200.
- **Source without a signing secret:** `Verify` cannot run. Such a source answers only the `handshaker` handshake, and only with the echo the handshake returns. It writes no delivery row, takes no replay key and moves no counter. It rejects every other request with `ErrSignatureInvalid` and the same 404 as a failed `Verify`, so the response does not show that the source has no secret yet. Only the recorded reason, "signing secret not set", differs. A source with a secret keeps the order `Verify`, then the handshake.
- **Trade-off:** The handshake on a source without a secret is unsigned. The reply echoes the caller's challenge and carries nothing else, and the source id is an unguessable UUID. A caller who knows the id and sends a `url_verification` body learns that the source exists and gets its own challenge back. The source runs no trigger and stores nothing until an admin sets the secret.
- **Delivery id:** `DeliveryID` returns the body's `event_id`, and the Events API documents it as globally unique. `app_rate_limited` carries no `event_id` and fails the existing `ErrMissingDeliveryID` guard, and that rejection is accepted.
- **Event type:** `EventType` returns `""` unless the body's `type` is `event_callback`. It returns `""` when `event.bot_id` is set or `event.subtype` is `bot_message`. `event.type` `app_mention` maps to `mention`. `event.type` `message` with `event.channel_type` `im` and no subtype or the `file_share` subtype maps to `direct_message`. Every other event returns `""`, including `thread_broadcast` and edits in a direct message.
- **Retries:** Slack retries a delivery up to three times and marks each retry with `X-Slack-Retry-Num`. A retry carries the same `event_id`, and the partial unique index on `trigger_deliveries` records it as a duplicate. The adapter does not read the retry headers.

### 6. Slack Chat Mapping

`ChatEvent` maps the Slack payload as follows:

- `text` is `event.text` with Slack's mention markup kept as sent.
- `user.id` is `event.user` uppercased.
- `conversation.id` is `event.channel`.
- `thread.id` is `event.thread_ts` for a reply and `event.ts` for a top-level message.
- `conversation.type` is `event.channel_type` mapped: `channel` to `channel`, `group` to `private`, `im` to `dm`, `mpim` to `group`. It is empty for any other value.
- `thread.started_by.id` is `event.parent_user_id` uppercased for a reply and `event.user` for a top-level message.
- `message.id` is `event.ts`.
- `workspace.id` is `team_id`.
- `platform` is `slack`.

A Slack `ts` is unique within one conversation only. A mention trigger's routing `join_key` is `{{ conversation.id }}:{{ thread.id }}`. A direct message trigger's `join_key` is `{{ conversation.id }}`, one key per direct message conversation. The Slack adapter has no schema file of its own.

### 7. Slack App Manifest

`frontend/src/app/(dashboard)/triggers/_lib/slack-app-manifest.ts` builds a JSON [app manifest](https://docs.slack.dev/reference/app-manifest) from `useBrand().productName`:

- `display_information.name` is the product name, cut to 35 characters.
- `features.bot_user.display_name` is a slug of the product name in the characters Slack allows: `a-z`, `0-9`, `-`, `_` and `.`.
- `features.app_home.messages_tab_enabled` is true and `messages_tab_read_only_enabled` is false. People can then send the bot direct messages.
- `oauth_config.scopes.bot` lists `app_mentions:read` and `im:history` for the chat source. It lists `chat:write`, `channels:history`, `groups:history` and `users:read` for the agent to read the thread and reply.
- `settings.event_subscriptions.request_url` is the source's request URL, and `bot_events` lists `app_mention` and `message.im`. Socket Mode is off, so Slack rejects a manifest without a `request_url` with `invalid_manifest`.
- `settings.socket_mode_enabled`, `org_deploy_enabled` and `token_rotation_enabled` are false.

The Slack MCP server uses this app's bot token (`xoxb-`) to post, and the admin adds any further scopes that server needs. The wizard issues the request URL first and takes the signing secret last, in one pass:

1.  The admin picks Slack and names a new source. The wizard creates the source at once, with no signing secret.
2.  The source step shows the manifest with the real `request_url` and the request URL with a copy button. An admin with an existing Slack app sets the URL under Event Subscriptions instead. No placeholder URL is shown for a source that does not exist.
3.  Slack sends `url_verification` and kora answers it.
4.  The admin pastes the signing secret from Slack's Basic Information page into the same screen. The wizard does not continue until the secret is saved.

The retry path in `create-from-wizard.ts` reuses the source it created. The triggers page and the trigger drawer show a chat source without a secret as waiting for its signing secret, with a way to set it.

### 8. Acknowledgement Time

Slack requires a 2xx within three seconds. `Service.Ingest` records the delivery and enqueues each matched trigger inline. The run starts when the worker claims the fire, and routing decisions run in `DispatchPending`. The 202 therefore waits on database writes only and never on a run or a model call.

### Future Platforms

A Teams adapter would implement `chatAdapter` for Bot Framework activities. Teams sends them with a JWT in `Authorization` that the bot validates against keys from Bot Framework's OpenID metadata, with the bot's app id as the audience ([Bot Connector authentication](https://learn.microsoft.com/en-us/azure/bot-service/rest-api/bot-framework-rest-connector-authentication)). That validation needs a cached key fetch that `Verify` does not perform. Discord's HTTP endpoint carries interactions only, signed with Ed25519 in `X-Signature-Ed25519` and `X-Signature-Timestamp` ([Discord interactions](https://docs.discord.com/developers/interactions/overview)). Only a `command` event maps from it. A Discord adapter needs a 401 for a rejected signature and an initial reply to each interaction within three seconds ([Receiving and responding](https://docs.discord.com/developers/interactions/receiving-and-responding)). The pipeline has neither.

---

## Consequences

### Positive

- A chat trigger's prompt, filters, sender restriction and join key carry over to every later chat platform that delivers the same event.
- The console's sender restriction carries no provider text, and a new provider declares its own roles.
- Any future adapter can take a provider-issued secret or answer a handshake without a pipeline change.

### Negative

- API callers see chat command triggers as `webhook` triggers on a source whose `category` is `chat`.
- Every message an app posts in a direct message with the bot, including the agent's replies, records a delivery that matches no trigger.
- A Slack MCP server that posts with a user token sends messages without `bot_id`. Those messages can start runs and loop.
- A channel follow-up reaches kora only when it mentions the bot. `message.channels` is not subscribed.
- `fanOut` bounds the inline enqueue with `dispatchTimeout` (60 seconds, `service.go`). A slow database past three seconds makes Slack retry, and the retry records as a duplicate.
- A Slack trigger with dedupe disabled starts a run for every Slack retry (`dispatchDuplicate` in `service_ingest.go`).
- The handshake on a source without a signing secret is unsigned, so a caller who knows the source id can get a challenge echoed.
- A source abandoned before its signing secret is set stays inert and waiting, because it has no secret to verify events against. Deleting the source is the cleanup.
- Slack's signing secret regeneration has no grace window. Deliveries between the regeneration in Slack and the rotation in kora get 404, and Slack drops them after its retries.
- An admin configures two things for one Slack app: the chat source and the Slack MCP server.
- Storage and the API use different names for the sender restriction keys.

### Neutral

- `sender_restriction` and the event types response change shape in the API.
- Chat sources appear in the existing Sources, Triggers and Deliveries surfaces.

---

## Implementation

1.  **`feat/slack-trigger-capabilities` (#641):** `chatAdapter`, `schemas/chat.json` and its registry assignment, the fail-closed match in `fanOut`, `category` on source and event types responses, chat field validation on trigger create and update, the generic `Sender`, `SenderRole` and `SenderRestriction`, the GitHub roles, the storage mapping in `postgres.go`, the refusal of keys other than `ids` and `roles`, the chat filter on `observed`, `senders` on `EventTypesResponse`, and the kora MCP arguments and descriptions. Fake-adapter tests cover the capability call sites.
2.  **`feat/slack-trigger-adapter` (#643):** the Slack `ChatEvent` mapping, the `mention` and `direct_message` event types and the Slack `senderIdentifier`.
3.  **`feat/slack-trigger-frontend` (#644):** the chat registry entry with `apiKind`, category-driven provider and source lists, the Chat command filter on the triggers page, the sender editor reading `senders`, the manifest without `request_url` and the request URL on the done step, chat placeholders, and Storybook mocks and stories for the chat source setup and done step. The BFF passthroughs (`frontend/src/app/api/trigger-sources/route.ts`, `frontend/src/app/api/trigger-sources/[id]/route.ts`, `frontend/src/shared/api.ts` and `@runkora/server/triggers-client`) carry `signing_secret`, `category`, `senders` and the `sender_restriction` keys.

Issue #656 changes the setup order, so the request URL is issued before the signing secret. It ships as three pull requests:

4.  **Docs (#657):** the setup flow in the ADR and the PRD.
5.  **Backend:** create a chat source without a signing secret, accept the first secret without `rotate_secret`, refuse a second set without it, echo the unsigned handshake on a source without a secret and reject every other request.
6.  **Console:** the manifest with `request_url`, the source-first wizard order with the signing secret saved last, and the waiting-for-secret state.

---

## Validation

Unit tests cover:

- Slack verification with a valid signature, a wrong signature, a missing header and timestamps inside and outside five minutes.
- The handshake reply, a handshake with a bad signature and the absence of a delivery row.
- Slack event types for a mention, a direct message, a `file_share` direct message, an edited message, a `thread_broadcast`, a message with `bot_id` and a `bot_message`.
- The Slack chat document for a top-level message and a thread reply, and a trigger that cannot reference a raw Slack path.
- An unmapped chat payload and a nil body start no run and join nothing.
- Sender restrictions by id and role for GitHub and Slack, the storage mapping, and a request with `logins`, `associations` or a misspelt key refused.
- The `observed` filter on a chat source.
- Create and rotate with and without `signing_secret` for Slack, GitHub and Alertmanager.
- Create without a secret, setting the first secret without `rotate_secret`, and a second set refused without `rotate_secret`.
- A source without a secret: the unsigned handshake echo, no delivery row, replay key or counter, every other request rejected with the same 404 status and body as a bad signature and the reason "signing secret not set" recorded, and the unchanged order for a source with a secret.

---

## Related Decisions

- [Agent Triggers and Webhook Ingestion](2026-06-17-agent-triggers-and-webhook-ingestion.md)
- [Alertmanager Webhook Adapter](2026-07-21-alertmanager-webhook-adapter.md)
- [Trigger Payload Templating](2026-09-28-trigger-payload-templating.md): the sender restriction vocabulary superseded in part.
- [Trigger Fire Coalescing](2026-10-13-trigger-fire-coalescing.md)
- [Delegated Agent Identity](2026-07-23-delegated-agent-identity.md)
- [Trigger Routing](2026-10-19-trigger-routing.md)

---

## References

- [Events API](https://docs.slack.dev/apis/events-api/)
- [url_verification event](https://docs.slack.dev/reference/events/url_verification)
- [app_mention event](https://docs.slack.dev/reference/events/app_mention)
- [message.im event](https://docs.slack.dev/reference/events/message.im)
