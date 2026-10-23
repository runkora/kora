# Discord Gateway Chat Source

<!-- Filename: 2026-10-23-discord-gateway-chat-source.md -->

**Status:** Proposed  
**Date:** 2026-10-23  
**Deciders:** Justin Wilkin  
**Technical Story:** [Chat Command Triggers PRD](../prd/chat-command-triggers.md), runkora/kora#728

---

## Context

### Background

The [Chat Command Triggers ADR](2026-10-20-chat-command-triggers.md) added the chat kind. A chat adapter implements `chatAdapter` (`backend/internal/triggers/adapter.go`) and maps its payload into the one document in `schemas/chat.json`. Slack is the only chat adapter. Its events arrive as signed HTTP requests that `Service.Ingest` (`service_ingest.go`) verifies, deduplicates, records and fans out.

Discord does not deliver messages that way. Its HTTP endpoint carries interactions only, such as slash commands ([Discord interactions](https://docs.discord.com/developers/interactions/overview)). Messages, mentions and direct messages arrive as `MESSAGE_CREATE` events over the [Gateway](https://docs.discord.com/developers/events/gateway). The Gateway is a WebSocket that the bot opens to Discord and keeps open with its bot token.

Four parts of the pipeline do not fit:

- `Service.Ingest` receives HTTP requests and calls `Verify` on each one. A Gateway message is already authenticated by the session that carries it.
- A chat source holds no platform token (PD5 in the PRD). A Gateway session needs the bot token.
- `kora-triggers` runs request handlers and one cron loop (`scheduler.go`). It holds no long-lived outbound connection.
- `Service.Ingest` has no hook for a connection state. A source records only rejection counters (`RecordSourceRejection` in `postgres.go`).

The Gateway behaves as follows:

- The client connects to `wss://gateway.discord.gg/?v=10&encoding=json` and receives a Hello event with a heartbeat interval.
- The client sends a heartbeat each interval and sends Identify with its token and intents. Discord answers with Ready. Ready carries `session_id` and `resume_gateway_url`.
- After a dropped connection the client opens `resume_gateway_url` and sends Resume with the last sequence number. Discord replays the missed events.
- A client may send 1,000 Identify calls in 24 hours. Resume calls do not count. At the limit Discord ends all sessions of the app and resets the bot token ([Identifying](https://docs.discord.com/developers/events/gateway)).
- Close codes 4004 (authentication failed) and 4014 (disallowed intents) tell the client not to reconnect ([Response codes](https://docs.discord.com/developers/topics/opcodes-and-status-codes)).

### Problem Statement

How should kora hear Discord mentions and direct messages and feed them into the chat pipeline, with the least change to ingest, storage and routing?

### Goals

- A Discord source reuses the chat schema, triggers, filters, sender restriction, deduplication, delivery history and routing.
- A Discord chat trigger is written exactly like a Slack one.
- Creating, enabling, disabling, deleting or rotating a Discord source takes effect without a restart.
- A Discord adapter in one file.

### Non-Goals

- Slash commands and other interactions.
- The privileged Message Content intent and any channel message that does not mention the bot.
- Discord roles in the sender restriction.
- Sharding beyond one shard per source.
- An acknowledgement in Discord when a run starts.
- Replying to Discord. Agents reply through a Discord MCP server.

### Which of the Three Questions This Touches

**How does it connect?** kora connects to Discord as the bot with the bot token. Outbound replies go through the Discord MCP server and the MCP gateway.

**Who is this agent?** Unchanged. A Discord-triggered run acts as the source's mapped SERVICE principal. Discord's user id stays in the stored payload for audit.

**What can it access?** Unchanged. The source principal's grants bound every run. Anyone who can message the bot or mention it in a channel it can see can fire a trigger that does not narrow by conversation or sender.

---

## Decision Drivers

- Discord triggers must reuse the chat pipeline without a change to the chat schema.
- A message sent by a bot or a webhook must never start a run. The agent's own replies are such messages.
- The bot token must stay encrypted at rest and must never leave the trigger service.
- A rejected token must not cause a reconnect loop. Identify calls are limited per day.
- The runtime must keep the cron scheduler's property: any number of replicas, no lock, one delivery.
- The adapter must not need the privileged Message Content intent.

---

## Considered Options

### Events

**Option 1a (chosen):** Reuse `mention` and `direct_message`.

**Option 1b (rejected):** Add Discord events such as `message` and `command`. A Discord trigger would differ from a Slack trigger.

### Session Placement

**Option 2a (chosen):** Every `kora-triggers` replica opens one session per enabled Discord source. Every replica receives each message. The unique index on `trigger_deliveries` (`source_id`, `provider_delivery_id`) keeps one delivery. There is no leader election.

**Option 2b (rejected):** One replica holds the session under a lease. A lease handover window can miss messages, and the Gateway replays only into a session that resumes. The scheduler rejects leader election for the same reason (`scheduler.go`).

**Option 2c (rejected):** A separate Gateway service. It adds a deployable, a port, a network policy and an authorization policy for one connection per source.

### Bot Token Storage

**Option 3a (chosen):** The admin pastes the bot token as the source secret through `issuedSecret`. It is stored encrypted in `secret_ciphertext` and changed with `rotate_secret`.

**Option 3b (rejected):** Read the token from the Discord MCP server's registration. Credentials connections between a chat source and an MCP server are a non-goal of the chat ADR.

### Gateway Library

**Option 4b (chosen):** `github.com/disgoorg/disgo` v0.19.6. Its `gateway` package owns heartbeat, zombie detection, resume and reconnect for one session. Its `bot` package keeps the channel and thread cache.

Maintenance, read from the repository on 2026-10-23:

- The last release is v0.19.6 (2026-06-07). The last push is 2026-10-20. The repository is not archived and has 15 open issues.
- No open issue reports a failed reconnect. The issue "Not reconnecting to gateway" (#309) is closed.
- Open issue #494 reports a data race between `Close` and the heartbeat goroutine. Open pull request #559 guards the gateway and voice state against data races.
- Open pull request #538 removes the `AutoReconnect` option.

Fit to the session manager's rules (Decision 3):

- `gateway.WithIntents` sets the intents. `WithEnableRawEvents` delivers each dispatch payload as received.
- `gateway.WithIdentifyRateLimiter` takes an interface with `Wait`, `Unlock` and `Close`. The manager supplies one that spaces Identify calls.
- `gateway.WithCloseHandler` is called when the library stops a session. It receives the error and whether the close code permits reconnect. Close codes 4004 and 4010 to 4014 are marked as not reconnectable.

Fit to a fake Gateway in tests:

- `gateway.WithURL` sets the Gateway URL. `WithEnableResumeURL(false)` keeps a resumed session on that URL. `WithCompression(gateway.CompressionNone)` turns off the default zstd stream.

Thread data:

- The `bot` package handlers store the threads from `GUILD_CREATE` and `THREAD_LIST_SYNC` and apply `THREAD_CREATE`, `THREAD_UPDATE` and `THREAD_DELETE` to the same cache.
- `discord.GuildThread` carries `OwnerID` and `ParentID`. `ParentID` is never nil for a thread.

- **Cost:** The library ships a REST client, voice support, entity caches and its own types. kora uses the gateway and the thread cache. The cache fills through `bot.Client`. The session builds a `bot.Client` and not a bare `gateway.Gateway`. The library's reconnect backoff inside a session is fixed. It starts at 1 second, doubles and caps at 60 seconds, with no jitter. A release that removes `AutoReconnect` (pull request #538) changes the option kora sets. The module adds `gorilla/websocket` and the `disgoorg` modules to `backend/go.mod`. The library's release cadence becomes a dependency.

**Option 4a (rejected):** A hand-written client on `github.com/coder/websocket`. The module is in `backend/go.mod` as an indirect dependency at v1.8.15.

- **Cost:** kora owns the protocol: Hello, heartbeat, zombie detection, Identify, Resume, Reconnect, Invalid Session, identify pacing and close-code handling. A change to the Gateway version, a new close code or a new opcode needs a code change. The client needs its own tests for the zombie connection and resume paths. `coder/websocket` becomes a direct dependency.

**Option 4c (rejected):** `github.com/diamondburned/arikawa/v3` or `github.com/bwmarrin/discordgo`. Read from the repositories on 2026-10-23:

- arikawa: last release v3.6.0 (2025-09-04), last push 2026-05-18, 35 open issues.
- discordgo: last release v0.29.0 (2025-05-24), last push 2026-02-14, 233 open issues.
- disgo has the most recent release and push of the three.

- **Cost:** Not chosen. Neither library was fitted to the session manager's rules or to a fake Gateway URL.

### Thread Detection

A `MESSAGE_CREATE` event carries `channel_id` and an optional `channel_type`. For a message in a thread, `channel_id` is the thread's id. The event carries neither the thread's parent channel nor the thread's owner. The chat document needs both (`conversation.id` and `thread.started_by.id`).

**Option 5a (chosen):** Read the library's channel and thread cache, fed by the `GUILDS` intent. The cache holds `ParentID` and `OwnerID` for each thread (option 4b). A message whose `channel_id` is not in the cache as a thread is treated as a message outside a thread. There is no REST fallback.

- **Cost:** The session holds state for every active thread in the bot's guilds. A message in an archived thread can arrive before `THREAD_UPDATE`. That message maps as a message outside a thread.

**Option 5b (rejected):** A REST lookup of `GET /channels/{id}` for every guild message, with no cache. The session cannot rely on the optional `channel_type` to skip channels.

- **Cost:** Each thread message adds one request to the Discord API and its latency. The requests share the bot's API rate limit. A burst of thread messages can be rate limited, and the message waits or fails.

### Ingest Entry Point

**Option 6a (chosen):** A Service method for the Gateway session that skips HTTP verification and replay windows.

**Option 6b (rejected):** Sign each Gateway message inside the process and call `Service.Ingest`. It adds a secret use and a decrypt per message.

---

## Decision

**Chosen Options:** 1a, 2a, 3a, 4b, 5a and 6a.

### 1. Discord Adapter

`backend/internal/triggers/adapter_discord.go` holds `discordAdapter` and registers under the provider id `discord`. It implements `SourceAdapter`, `chatAdapter`, `issuedSecret` and `senderIdentifier`. It does not implement `handshaker`.

- **Schema:** `Schema` returns nil. The registry gives the adapter the shared chat catalog.
- **Verify:** `Verify` always returns `ErrSignatureInvalid`. The adapter exposes no HTTP ingest.
- **Webhook URL:** A request to a Discord source's webhook URL goes through `Service.Ingest`. A source with a secret runs `Verify`, and it fails. A source without a secret takes `ingestWithoutSecret` and fails the same way. Both answer the same 404 as a bad signature. The source counts a rejection.
- **Category:** `categoryOf` reports `chat`.

### 2. Bot Token as the Source Secret

The bot token is the source's secret. The `issuedSecret` rules in Decision 3 of the chat ADR apply unchanged:

- `CreateSource` accepts a source without a secret. The source waits for its token.
- `UpdateSource` sets the first token without `rotate_secret`. A change after that needs `rotate_secret` and the new value.
- The token is trimmed, must be non-empty and at most 256 bytes. It is stored encrypted in `secret_ciphertext`.
- `SourceResponse.Secret` stays empty. `signing_secret_set` tells the console whether a token is saved. The request field keeps the name `signing_secret`.
- The kora MCP tools do not accept the token.

The Discord MCP server keeps its own copy of the token. A rotation changes both.

### 3. Session Manager

`GatewayManager` runs in `kora-triggers` beside the `Scheduler` (`cmd/kora-triggers/main.go`). Each replica runs one manager. The manager holds one session per enabled Discord source that has a secret.

**Reconcile.** The manager reads the Discord sources from `webhook_sources` on an interval of 30 seconds. It compares each row with its running session. It acts on the difference:

- **Start:** A source is enabled, holds a secret and has no session.
- **Stop:** A source is deleted, is disabled or has no secret.
- **Restart:** `secret_rotated_at` differs from the value the session started with.

A change made through the API on one replica reaches every other replica at its next reconcile. The replica that served the change also reconciles at once. Sessions need no coordination between replicas.

**Library.** Each session is one `bot.Client` from disgo (option 4b). The library owns the Gateway protocol: Hello, heartbeat, zombie detection, Identify, Resume, reconnect within a session and the thread cache. kora's code sets the library up and decides when a session starts and stops.

**Session setup.** The manager builds each `bot.Client` with:

- The bot token from the source's secret.
- The intents in Decision 4.
- Raw events on. The session reads the `MESSAGE_CREATE` payload as received.
- The cache flag for channels. The channel cache holds the threads (Decision 7).
- A close handler that reports to the manager.
- An identify rate limiter owned by the manager.
- The Gateway URL from configuration. It defaults to `wss://gateway.discord.gg`. Tests point it at the fake Gateway.

**Identify pacing.** Discord allows one Identify per 5 seconds for each `max_concurrency` slot. The manager's identify rate limiter admits one Identify at a time and spaces them by 5 seconds. The manager starts its sessions through that limiter.

**Start.** The manager opens the session. If the open fails with a reconnectable error, the manager starts a new session after a backoff.

**Stop.** The manager closes the `bot.Client` when the source is deleted, disabled or loses its secret, and on shutdown.

**Backoff between sessions.** The manager waits before it starts a new session after a failed open or after the library stops a session with a reconnectable error. The wait is exponential with jitter. It starts at 1 second and caps at 5 minutes. A session that reaches Ready resets the backoff. The library's own reconnect inside a session is not tuned.

**Ready.** A session that receives Ready records the bot's user id for the wrapper (Decision 6) and sets the connection state (below).

**Rejection.** The library's close handler reports the close code. Close codes 4004 and 4014 end the session and record a rejection reason on the source:

- 4004: `discord rejected the bot token`.
- 4014: `discord rejected the requested intents`.

Close codes 4010, 4011, 4012 and 4013 are also marked not reconnectable by Discord. They end the session and record the library's description of the code as the reason. The session stays stopped until the secret changes. The manager does not start a session for a source whose recorded rejection is newer than `secret_rotated_at`. A rotation restarts the session.

**Connection state.** A migration under `backend/cmd/kora-triggers/migrations/` adds three columns to `webhook_sources`: `gateway_status`, `gateway_reason` and `gateway_status_at`. `gateway_status` is `connected` or `rejected`, and null before a first connection. A session sets `connected` on Ready and `rejected` with the reason on a rejection close code. The existing `last_rejection_reason` is not used. Requests to a Discord source's webhook URL write it ("invalid signature"), and it would then show a healthy source as rejected. The source response carries a read-only `connection` object with `state` and `reason`. `state` is `waiting` when the source has no secret, otherwise `connected` or `rejected` from the columns. A disabled source reports no `connection`.

### 4. Intents

The Identify payload sets `intents` to `GUILDS` (`1 << 0`), `GUILD_MESSAGES` (`1 << 9`) and `DIRECT_MESSAGES` (`1 << 12`). The sum is 4609. No other intent is sent.

- `GUILDS` delivers `GUILD_CREATE`, `THREAD_*` and channel events. The library's cache stores them. Thread detection reads the cache (Decision 7).
- `GUILD_MESSAGES` delivers `MESSAGE_CREATE` for guild channels.
- `DIRECT_MESSAGES` delivers `MESSAGE_CREATE` for direct messages.

None of the three is privileged. The Message Content intent (`1 << 15`) is privileged. Without it Discord sends empty `content`, `embeds`, `attachments`, `components` and `poll` fields with these exceptions: content of messages the app sends, content of direct messages with the app, and content of messages that mention the app ([Message Content Intent](https://docs.discord.com/developers/events/gateway)). Mentions and direct messages are the only messages a trigger uses. The session needs no Message Content intent.

A message in a guild channel that does not mention the bot arrives with empty content. `EventType` returns `""` for it.

### 5. Gateway Ingest Entry Point

`Service` gains one method for a Gateway session, `IngestGateway(ctx, sourceID, body)`. It follows `Ingest` with these differences:

- It skips `cipher.decrypt` and `adapter.Verify`. The authenticated session is the proof.
- It skips the handshake and `ClaimReplayKey`. The replay window guards a captured signed request. A Gateway message has none.
- It does not read request headers.

It keeps these steps:

- **Source checks:** The source must exist, be enabled and have a registered adapter.
- **Delivery id:** `adapter.DeliveryID` returns the message id. An empty id is rejected with `ErrMissingDeliveryID`.
- **Delivery row:** `InsertDelivery` writes the row. The partial unique index on `(source_id, provider_delivery_id)` (`postgres.go`) rejects a second insert. `IngestGateway` does not call `recordDuplicate` for it.
- **Counters:** `recordAccepted` counts the accepted message.
- **Sender and fan-out:** `dispatch` runs `identifySender` and `fanOut`. Sender restrictions, trigger filters, coalescing and routing apply unchanged.

The session calls `IngestGateway` only when `EventType` returns a non-empty value. A guild message that does not mention the bot, a bot message and a webhook message write no delivery row.

`IngestGateway` returns an `IngestResult`. The session logs an error and ignores the outcome.

Every replica holds a session for the source and receives the same message. Each calls `IngestGateway`. One insert wins and runs the triggers.

On a unique conflict `IngestGateway` returns without writing a delivery row. It does not call `dispatchDuplicate`. A second session's copy of a message is not a provider retry. A trigger with deduplication off runs once for the message.

### 6. Event Types

The session wraps each dispatch before it calls `IngestGateway`. The stored body is:

```json
{
  "d": { "...": "the MESSAGE_CREATE data, as received" },
  "kora": {
    "bot_user_id": "the bot's user id from Ready",
    "thread": { "id": "", "parent_id": "", "parent_type": 0, "owner_id": "" }
  }
}
```

`kora.thread` is present only when the library's cache holds `d.channel_id` as a thread (channel type 10, 11 or 12). It holds the thread's id, its parent channel id, the parent's channel type and the thread's owner id. The wrapper keeps `EventType` and `ChatEvent` pure functions of the body. `Redeliver` maps the stored body again and gets the same result.

`DeliveryID` returns `d.id`, the message id.

`EventType` applies these rules in order:

1. It returns `""` when `d.author.bot` is true. The agent's own replies and every other bot's messages never start a run.
2. It returns `""` when `d.webhook_id` is set.
3. It returns `""` when `d.type` is neither 0 (default) nor 19 (reply).
4. It returns `direct_message` when `d.guild_id` is absent.
5. It returns `mention` when `kora.bot_user_id` equals the `id` of an entry in `d.mentions`.
6. It returns `""` otherwise.

`d.mentions` lists the users specifically mentioned. It does not list `@everyone`, `@here` or a role mention. A message that mentions only another user returns `""`.

### 7. Chat Mapping

`ChatEvent` maps the stored body into `schemas/chat.json`. A message outside a thread is its own thread. A thread started from a message has the id of that message ([Start Thread from Message](https://docs.discord.com/developers/resources/channel)). A top-level message and the replies in its thread therefore share one `thread.id`.

| Field | Source |
| --- | --- |
| `text` | `d.content` as sent. Discord's mention markup (`<@id>`) is kept. |
| `user.id` | `d.author.id`. |
| `conversation.id` | `kora.thread.parent_id` in a thread, otherwise `d.channel_id`. |
| `conversation.type` | In a thread, from `kora.thread.parent_type`. Otherwise from `d.channel_type`, and `dm` when `d.guild_id` is absent. 0 and 5 map to `channel`, 1 to `dm` and 3 to `group`. Any other value maps to empty. |
| `thread.id` | `kora.thread.id` in a thread, otherwise `d.id`. |
| `thread.started_by.id` | `kora.thread.owner_id` in a thread, otherwise `d.author.id`. |
| `message.id` | `d.id`. It equals `thread.id` outside a thread. |
| `workspace.id` | `d.guild_id`. It is empty in a direct message. |
| `platform` | `discord`. |

`ok` is true only when `user.id`, `conversation.id` and `thread.id` are non-empty.

Discord has no private-channel type. A guild channel with restricted permissions maps to `channel`. `private` is never produced.

Thread detection (option 5a): the session looks up `d.channel_id` in the library's channel cache before it wraps the message. A `discord.GuildThread` entry fills `kora.thread` from its `ID`, `ParentID`, the parent's channel type and `OwnerID`. Any other result leaves `kora.thread` out. The session makes no REST request.

Routing `join_key`:

- `mention` triggers use `{{ conversation.id }}:{{ thread.id }}`. A reply in a thread joins the run that the thread's first mention started.
- `direct_message` triggers use `{{ conversation.id }}`. A direct message conversation has one `channel_id`.

### 8. Sender Identification

`Sender` reads `d.author.id`. `NormalizeID` trims space and returns the id, a decimal snowflake string. `Roles` returns an empty list. The sender restriction editor shows people only. `ChatEvent` sets `user.id` to the same value.

### 9. Chart Egress

`kora-triggers` reaches `gateway.discord.gg` and `discord.com` over TLS on port 443. The Ready event's `resume_gateway_url` names a separate Gateway URL under `discord.gg`. The chart allows `*.discord.gg` for it.

- **NetworkPolicy:** `allow-external-https` in `charts/kora/templates/platform/netpols.yaml` lists the pods that may reach port 443 outside the cluster. `kora-triggers` joins that list. A NetworkPolicy matches addresses only. The pod can then reach any host on 443.
- **Mesh:** When `istio.outboundTrafficPolicy` is `REGISTRY_ONLY`, `serviceentries.yaml` gains a `ServiceEntry` for `gateway.discord.gg`, `*.discord.gg` and `discord.com`.
- **Other environments:** An environment with `ALLOW_ANY` and no restricting policy needs no change.

---

## Consequences

### Positive

- A Discord trigger is written like a Slack trigger. Prompts, filters, sender restrictions, routing and delivery history carry over.
- A bot connection needs no inbound URL. A Discord source works in a cluster that has no public ingress.
- Replicas need no coordination. Any replica can stop or start without a handover.
- Intents carry no privileged access. The admin needs no Discord review.

### Negative

- Each `kora-triggers` replica holds its own session. Every rollout uses one Identify call per replica for each Discord source. Discord allows 1,000 Identify calls a day for a bot. At the limit Discord ends all sessions and resets the bot token. The admin must paste the new token into kora and into the Discord MCP server.
- A message sent while every replica is down is lost. The Gateway replays missed events only into a session that resumes. A session that cannot resume identifies anew and receives no replay.
- The bot token lives in two places, the chat source and the Discord MCP server. A rotation changes both.
- A follow-up in a channel reaches kora only when it mentions the bot, the same as Slack.
- Two Discord sources with the same bot token open two sessions. Each source deduplicates on its own, and a message starts a run on each source's triggers.
- The reconcile interval delays a change. A new source, a rotation or a disable reaches the other replicas within 30 seconds.
- The session holds a cache of the bot's active threads (option 5a). A message in a thread that is not in the cache maps as a message in a channel, with the thread's id as `conversation.id`.
- A role mention of the bot does not start a run. `d.mentions` lists users only.
- The Gateway version is set by the library. A new Gateway version needs a library upgrade.
- The library is a dependency with open pull request #538 that removes the `AutoReconnect` option. The version in `backend/go.mod` is pinned.
- `kora-triggers` can reach any host on port 443 and holds every tenant's source secrets. A compromised pod can send them to any host. The `ServiceEntry` hosts apply only under `REGISTRY_ONLY`.
- A Discord source records no delivery for ignored messages. The delivery history cannot show that a bot message or an unmentioned channel message was seen. This differs from Slack.

### Neutral

- Discord sources appear in the existing Sources, Triggers and Deliveries surfaces.
- The source response gains a read-only `connection` object.
- `webhook_sources` gains three columns.

---

## Implementation

Four pull requests, each based on the one before it:

1. **`docs:` (this ADR):** the PRD amendment and this ADR, against `main`.
2. **`feat(triggers):` the Gateway runtime:** `GatewayManager`, its reconcile against the sources table, the disgo `bot.Client` setup, the identify rate limiter, start and stop, backoff between sessions, the rejection reasons, the connection state columns, the thread lookup in the library's cache and `Service.IngestGateway`. The pull request adds disgo to `backend/go.mod`. Tests run against a fake Gateway that disgo reaches through `gateway.WithURL`. Based on the docs branch.
3. **`feat(triggers):` the Discord adapter:** `adapter_discord.go`, the event type rules, the `ChatEvent` mapping, the sender identification and the chart egress. Based on the runtime branch.
4. **`feat(frontend):` the console:** Discord in the chat command kind, the source step with the token field and the invite link, and the connection state on the triggers page and in the trigger drawer. Based on the adapter branch.

---

## Validation

Unit tests run against a fake Gateway. They cover:

- Event types for a mention, a direct message, a bot author, a webhook message and a message that mentions another user.
- The chat document for a channel message and a thread message.
- `IngestGateway` with one message seen by two sessions: one delivery row and one run for a trigger with deduplication off.
- Session start and stop on create, enable, disable, delete and rotation.
- The rejection reasons for a bad token (4004) and for disallowed intents (4014).
- The close codes 4010 to 4013 record the library's description as the reason.
- Backoff between sessions after a failed open and its reset on Ready.
- Identify calls spaced by the identify rate limiter.
- A thread message and a message in a channel read from the library's cache.
- A request to a Discord source's webhook URL answered with the 404 of a bad signature.

Console tests cover the Discord source step and its connection states.

---

## Related Decisions

- [Chat Command Triggers](2026-10-20-chat-command-triggers.md)
- [Agent Triggers and Webhook Ingestion](2026-06-17-agent-triggers-and-webhook-ingestion.md)
- [Extended Trigger Types](2026-06-20-extended-trigger-types.md): the scheduler's multi-replica pattern.
- [Trigger Routing](2026-10-19-trigger-routing.md)
- [MCP Server Static Credentials](2026-10-22-mcp-server-static-credentials.md)

---

## References

- [Gateway](https://docs.discord.com/developers/events/gateway)
- [Gateway events](https://docs.discord.com/developers/events/gateway-events)
- [Opcodes and status codes](https://docs.discord.com/developers/topics/opcodes-and-status-codes)
- [Channel resource](https://docs.discord.com/developers/resources/channel)
- [Message resource](https://docs.discord.com/developers/resources/message)
