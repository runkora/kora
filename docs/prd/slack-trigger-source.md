# Slack Trigger Source - Product Requirements Document

**Status:** In Review  
**Created:** 2026-10-20  
**Last Updated:** 2026-10-20  
**Owner:** Justin Wilkin  
**Stakeholders:** Platform Team, trigger authors across engineering teams

---

## Executive Summary

Slack will be a trigger source. A person who mentions the kora bot in a channel or sends it a direct message will start an agent run, and the agent will answer in Slack through the Slack MCP server. An admin sets up the Slack app from a manifest that kora ships, and trigger authors narrow who and where can fire a trigger with the filters every webhook trigger already has.

---

## Problem Statement

### Current State

Triggers start runs from GitHub and Alertmanager webhooks, from a schedule and from a manual fire. Teams talk to each other in Slack, and nothing in Slack can start a run. An agent can already post to Slack through a registered Slack MCP server. The trigger wizard lists "Chat command" as a planned kind.

### User Pain Points

- **No conversational entry point:** A person who wants an agent's help leaves Slack, opens the dashboard and starts a run there.
- **Lost thread context:** A question raised in a Slack thread has to be copied into the run's first message by hand.
- **Custom bots:** A team that wants an agent in Slack builds and hosts its own bot that calls the kora API.

---

## Goals and Objectives

### Primary Goals

1.  A mention of the bot in a channel or a direct message to the bot starts a run of the agent its trigger names.
2.  An admin creates the Slack app and the Slack source without writing code.
3.  A trigger author limits a trigger to chosen channels and chosen Slack users.
4.  A follow-up in the same Slack thread can reach the run that is already working on it.
5.  Every Slack delivery appears in the delivery history with the Slack user who caused it.

### Success Metrics

None at this time.

### Non-Goals

- A platform acknowledgement in Slack, such as a reaction or a link to the run.
- Runs that act as the person who mentioned the bot.
- Socket Mode.
- Kubernetes resources that declare a Slack source.
- One Slack app distributed to several workspaces through OAuth.
- Microsoft Teams and Discord.
- Reaction events.

---

## User Stories

### US-001: Connect a Slack workspace

**As a** tenant admin,  
**I want** to create the Slack app from a manifest and register it as a trigger source,  
**So that** people in my workspace can reach agents from Slack.

**Acceptance Criteria:**

- [ ] The new-source step offers Slack as a provider.
- [ ] The step shows the Slack app manifest with a copy button and the steps to create the app from it.
- [ ] The admin pastes the app's signing secret from Slack into the source form. kora does not generate a secret for a Slack source.
- [ ] After the source is created, kora shows the request URL to enter in the Slack app's event settings.
- [ ] Slack's request URL check succeeds against the new source, and the check creates no delivery.
- [ ] Replacing the secret asks the admin for the new secret from Slack.
- [ ] GitHub and Alertmanager sources keep their current setup and their generated secrets.

### US-002: Start an agent from a mention or a direct message

**As a** trigger author,  
**I want** a trigger that fires on mentions of the bot or on direct messages to it,  
**So that** the right people can start an agent where they already work.

**Acceptance Criteria:**

- [ ] A Slack source offers two event types: a mention in a channel and a direct message.
- [ ] The author can limit the trigger to channels with a match condition on the channel.
- [ ] The author can limit the trigger to Slack users by user id with the sender restriction.
- [ ] The prompt template can use the message text, the channel, the Slack user, the message time and the thread.
- [ ] Messages the bot or any other app posts never start a run.
- [ ] Slack's retries of one event start at most one run for a trigger with deduplication on.

### US-003: Follow up in the same thread

**As a** person working with an agent in Slack,  
**I want** my next message in the thread to reach the run that is already handling it,  
**So that** the agent keeps its context.

**Acceptance Criteria:**

- [ ] Every Slack delivery carries a thread field that identifies its thread. A message outside a thread starts a thread of its own.
- [ ] A routing trigger whose join key names the channel and the thread sends a follow-up to the open run for that thread.
- [ ] In a channel, a follow-up reaches kora only when it mentions the bot. In a direct message, every message reaches kora.

### US-004: See Slack deliveries

**As a** trigger author,  
**I want** Slack deliveries in the delivery history,  
**So that** I can see who started a run and why a message did not.

**Acceptance Criteria:**

- [ ] Each Slack delivery shows its event type, the trigger outcomes and its payload.
- [ ] The payload shows the Slack user id of the person who sent the message.
- [ ] A message excluded by the channel filter or the sender restriction shows as filtered with the cause.
- [ ] A message from a bot shows as matching no trigger.

---

## Proposed Solution

### Setup

1.  The admin opens the new-source step, picks Slack and copies the manifest.
2.  The admin creates a Slack app from the manifest in their workspace and copies its signing secret.
3.  The admin names the source, pastes the secret and creates the source.
4.  kora shows the request URL. The admin enters it in the Slack app's event settings, and Slack confirms the URL.
5.  The admin installs the app in the workspace and invites the bot to the channels it should hear.

The manifest asks only for what kora needs to hear mentions and direct messages. Replying in Slack goes through a Slack MCP server registered for the same Slack app. That server holds the bot token and declares the scopes it needs. kora's trigger source never holds a Slack token.

### Writing a Trigger

A Slack trigger is a webhook trigger on a Slack source. The author picks the mention event or the direct message event, narrows by channel and Slack user, and writes the prompt. Agent choice and thread follow-ups come from trigger routing. A routing trigger's join key can combine the channel and the thread, such as `{{ event.channel }}:{{ thread }}`.

---

## Scope

### In Scope

- Slack as a provider for webhook sources that takes its signing secret from Slack.
- Mentions in channels and direct messages to the bot.
- A Slack app manifest in the repository and in the new-source step.
- A thread field for routing join keys.

### Out of Scope

- Everything listed under Non-Goals.

---

## Product Decisions

**PD1:** kora posts nothing in Slack when a run starts. The first visible signal is the agent's own reply through the Slack MCP server. **Accepted cost:** a person sees nothing until the agent replies, and a run that fails before replying leaves no trace in Slack.

**PD2:** Runs act as the Slack source's identity like every webhook-triggered run today. The delivery records the Slack user id for audit. Acting as the person who mentioned the bot is a committed later phase ([Delegated Agent Identity](delegated-agent-identity.md)). **Accepted cost:** every person who can fire a trigger gets the source's access for that run, whatever their own access is.

**PD3:** Anyone in a channel the bot is in may fire it, including external users in a Slack Connect channel. Triggers narrow by channel and by Slack user id. **Accepted cost:** any member or external user in a shared channel can start a run with the source's access when the trigger does not narrow it.

**PD4:** One Slack app serves one workspace. The admin creates it from the manifest, and the Slack source is an ordinary webhook source. **Accepted cost:** each workspace needs its own app and its own source.

**PD5:** Hearing Slack and replying in Slack stay separate. The trigger source holds the signing secret, and the Slack MCP server holds the bot token. They are joined only by being the same Slack app, and no Slack credentials connection exists. **Accepted cost:** an admin configures two things for one Slack app.

### Decisions Delegated to the ADR

The ADR decides the following:

- How kora verifies Slack's requests and answers Slack's request URL check.
- How a source takes its secret from the admin.
- The event types, the delivery id and how bot messages are dropped.
- How the thread field is computed and where the manifest lives.

---

## Related Documents

- [Agent Webhook Triggers PRD](agent-webhook-triggers.md)
- [Trigger Payload Templating PRD](trigger-payload-templating.md)
- [Delegated Agent Identity PRD](delegated-agent-identity.md)
- [Slack Trigger Source ADR](../adr/2026-10-20-slack-trigger-source.md)
- Trigger Routing PRD (`docs/prd/trigger-routing.md`, on the trigger routing stack)
