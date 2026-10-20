# Chat Command Triggers - Product Requirements Document

**Status:** Approved  
**Created:** 2026-10-20  
**Last Updated:** 2026-10-20  
**Owner:** Justin Wilkin  
**Stakeholders:** Platform Team, trigger authors across engineering teams

---

## Executive Summary

"Chat command" will be a trigger kind. A person who mentions the kora bot in a chat channel or sends it a direct message will start an agent run, and the agent will answer in the chat through the platform's MCP server. Slack is the first platform. A trigger is written once against kora's own chat events and fields and works the same on every later chat platform that delivers the same event.

---

## Problem Statement

### Current State

Triggers start runs from GitHub and Alertmanager webhooks, from a schedule and from a manual fire. Teams talk to each other in Slack, and nothing in Slack can start a run. An agent can already post to Slack through a registered Slack MCP server. The new-trigger screen lists "Chat command" as a planned kind. A webhook trigger reads its provider's own payload, and the sender restriction offers only GitHub's vocabulary.

### User Pain Points

- **No conversational entry point:** A person who wants an agent's help leaves Slack, opens the dashboard and starts a run there.
- **Lost thread context:** A question raised in a Slack thread has to be copied into the run's first message by hand.
- **Custom bots:** A team that wants an agent in Slack builds and hosts its own bot that calls the kora API.

---

## Goals and Objectives

### Primary Goals

1.  A mention of the bot in a channel or a direct message to the bot starts a run of the agent its trigger names.
2.  An admin connects a Slack workspace without writing code.
3.  A trigger author writes a chat trigger once, and the same prompt, filters and follow-up settings work on every chat platform that delivers the same event.
4.  A trigger author limits any webhook or chat trigger to chosen people, and to chosen roles where the platform reports roles.
5.  A follow-up in the same thread or direct message conversation can reach the run that is already working on it.
6.  Every chat delivery appears in the delivery history with the person who caused it.

### Success Metrics

None at this time.

### Non-Goals

- A platform acknowledgement in the chat, such as a reaction or a link to the run.
- Runs that act as the person who mentioned the bot.
- Socket Mode.
- Kubernetes resources that declare a chat source.
- One Slack app distributed to several workspaces through OAuth.
- Discord and Microsoft Teams in this release.
- Reaction events.

---

## User Stories

### US-001: Connect a Slack workspace

**As a** tenant admin,  
**I want** to register a chat source first and create the Slack app from its manifest,  
**So that** people in my workspace can reach agents from Slack.

**Acceptance Criteria:**

- [ ] The new-trigger screen offers "Chat command" next to the existing kinds.
- [ ] The chat command kind offers Slack. Discord and Microsoft Teams are shown as coming later and cannot be picked.
- [ ] The admin names the source, and kora creates it at once without a signing secret.
- [ ] The source step shows the Slack app manifest with the real request URL, a copy button and the steps to create the app from it. An admin with an existing app sees the request URL on its own.
- [ ] The wizard shows no placeholder URL for a source that does not exist yet.
- [ ] Slack's request URL check succeeds against the source before it has a secret, and the check creates no delivery.
- [ ] The admin pastes the app's signing secret from Slack into the same screen, and the wizard continues only after the secret is saved. kora does not generate a secret for a Slack source.
- [ ] A source without a secret shows as waiting for its signing secret on the triggers page and in the trigger drawer, with a way to set it.
- [ ] Replacing the secret asks the admin for the new secret from Slack.
- [ ] Placeholders and examples in the chat flow use chat terms.
- [ ] GitHub and Alertmanager sources keep their current setup and their generated secrets.

### US-002: Start an agent from a mention or a direct message

**As a** trigger author,  
**I want** a chat command trigger that fires on mentions of the bot or on direct messages to it,  
**So that** the right people can start an agent where they already work.

**Acceptance Criteria:**

- [ ] A chat command trigger offers two events: a mention and a direct message.
- [ ] The prompt and the filters use kora's chat fields: the message text, the person, the conversation, the thread, the workspace and the platform.
- [ ] The author can limit the trigger to conversations with a filter on the conversation.
- [ ] Messages the bot or any other app posts as a bot never start a run.
- [ ] Slack's retries of one event start at most one run for a trigger with deduplication on.

### US-003: Limit who can fire a trigger

**As a** trigger author,  
**I want** to list the people, and the roles where the platform has them, that may fire a trigger,  
**So that** a webhook or chat trigger answers only the senders I trust.

**Acceptance Criteria:**

- [ ] The sender restriction works the same on webhook and chat command triggers.
- [ ] The author lists people by the platform's id for them.
- [ ] The author picks roles from the list the platform declares, each with its label and description.
- [ ] GitHub offers its repository relationships as roles, and Slack offers no roles.
- [ ] A GitHub trigger fires for the same senders as before.

### US-004: Follow up in the same conversation

**As a** person working with an agent in a chat,  
**I want** my next message in the thread or the direct message to reach the run that is already handling it,  
**So that** the agent keeps its context.

**Acceptance Criteria:**

- [ ] A routing trigger on mentions can send a follow-up in the same thread to the open run for that thread.
- [ ] A routing trigger on direct messages can send any later message in the same conversation to the open run for that conversation.
- [ ] In a channel, a follow-up reaches kora only when it mentions the bot. In a direct message, every message reaches kora.

### US-005: See chat deliveries

**As a** trigger author,  
**I want** chat deliveries in the delivery history,  
**So that** I can see who started a run and why a message did not.

**Acceptance Criteria:**

- [ ] Each chat delivery shows its event, the trigger outcomes and the platform's payload.
- [ ] The payload shows the platform's id for the person who sent the message.
- [ ] A message excluded by a filter or the sender restriction shows as filtered with the cause.
- [ ] A message from a bot shows as matching no trigger.

---

## Proposed Solution

### Setup

1.  The admin picks "Chat command" on the new-trigger screen, picks Slack and names the source. kora creates the source at once, without a signing secret.
2.  kora shows the manifest with the real request URL. The admin creates the Slack app from it, or sets the request URL on an existing app. Slack confirms the URL, and kora answers the check.
3.  The admin copies the signing secret from the app's Basic Information page, pastes it into the same screen and continues.
4.  The admin installs the app in the workspace and invites the bot to the channels it should hear.

The manifest asks for what kora needs to hear mentions and direct messages, and for what the agent needs to read the thread and reply. Replying in Slack goes through a Slack MCP server registered with the same Slack app's bot token. The admin adds any further permissions that server needs. kora's chat source never holds a Slack token.

### Writing a Trigger

The author picks the mention event or the direct message event, narrows by conversation and person, and writes the prompt with kora's chat fields. The same trigger settings would work on another chat platform that delivers the same event. Agent choice and follow-ups come from trigger routing. A routing trigger's join key can identify the thread for mentions or the conversation for direct messages.

---

## Scope

### In Scope

- The "Chat command" trigger kind with Slack as its first platform.
- kora's chat events and fields.
- A sender restriction with people and platform-declared roles for webhook and chat triggers.
- A Slack app manifest in the chat source setup.

### Out of Scope

- Everything listed under Non-Goals.

---

## Product Decisions

**PD1:** kora posts nothing in the chat when a run starts. The first visible signal is the agent's own reply through the platform's MCP server. **Accepted cost:** a person sees nothing until the agent replies, and a run that fails before replying leaves no trace in the chat.

**PD2:** Runs act as the chat source's identity like every webhook-triggered run today. The delivery records the platform's id for the sender for audit. Acting as the person who mentioned the bot is a committed later phase ([Delegated Agent Identity](delegated-agent-identity.md)). **Accepted cost:** every person who can fire a trigger gets the source's access for that run, whatever their own access is.

**PD3:** Anyone in a channel the bot is in may fire it, including external users in a Slack Connect channel. Triggers narrow by conversation and by person. **Accepted cost:** any member or external user in a shared channel can start a run with the source's access when the trigger does not narrow it.

**PD4:** One Slack app serves one workspace. The admin creates it from the manifest. **Accepted cost:** each workspace needs its own app and its own source.

**PD4a:** Until the signing secret is set, the source answers only Slack's URL check and starts nothing.

**PD5:** Hearing the chat and replying in the chat stay separate. The chat source holds the signing secret, and the Slack MCP server holds the bot token. They are joined only by being the same Slack app, and no Slack credentials connection exists. **Accepted cost:** an admin configures two things for one Slack app. A Slack MCP server that posts as a user can start runs from its own replies.

**PD6:** Chat triggers read kora's chat fields only. The platform's own payload stays visible in the delivery history. **Accepted cost:** a detail that kora's chat fields do not carry cannot be used in a prompt or a filter.

**PD7:** The sender restriction lists people and roles for every provider. **Accepted cost:** an integration that sets a sender restriction through the API with the old field names is refused until it uses the new ones.

---

## Related Documents

- [Agent Webhook Triggers PRD](agent-webhook-triggers.md)
- [Trigger Payload Templating PRD](trigger-payload-templating.md)
- [Delegated Agent Identity PRD](delegated-agent-identity.md)
- [Chat Command Triggers ADR](../adr/2026-10-20-chat-command-triggers.md)
- [Trigger Routing PRD](trigger-routing.md)
