# Trigger Routing

<!-- Filename: 2026-10-19-trigger-routing.md -->

**Status:** Accepted  
**Date:** 2026-10-19  
**Deciders:** Justin Wilkin  
**Technical Story:** [Trigger Routing PRD](../prd/trigger-routing.md)

---

## Context

### Background

A trigger binds a firing condition to one agent (`Trigger.AgentTypeID` in `backend/internal/triggers/models.go`). The worker claims a queued fire and calls `DispatchPending` (`backend/internal/triggers/service_dispatch.go`), and `startRunForTriggerWithFields` (`backend/internal/triggers/service_ingest.go`) does the following:

1.  Resolve the trigger's principal.
2.  Render the prompt with `renderWebhookPrompt` (`backend/internal/triggers/template.go`). Delivery values go in a `<delivery-data>` block marked as untrusted.
3.  Mint a SERVICE token for the principal.
4.  Call `StartRun` on the typed agents client (`backend/internal/platform/clients/agents/client.go`).

Creating a trigger grants its principal `agents:runs:<slug>:read` and `agents:runs:<slug>:write` (`triggerRunScopes` in `backend/internal/triggers/service.go`). `Trigger.TargetKind` is `agent`, and the column carries a `CHECK (target_kind = 'agent')` constraint.

[Trigger Fire Coalescing](2026-10-13-trigger-fire-coalescing.md) holds fires with the same rendered key for a window and replaces a waiting fire with the newest payload (`renderCoalesceKey` in `backend/internal/triggers/coalesce.go`). `trigger_delivery_runs` records the run each fire started.

orbit-agents records the starting trigger on a run as `StartedBy` (`backend/internal/agents/models.go`) and leaves it nil for a run a person started. `RunService.SendMessage` (`backend/internal/agents/run_service.go`) delivers a message to an `active` or `idle` run behind `POST /api/v1/runs/{id}/messages` and checks `agents:runs:<slug>:write` for the caller through `Authz.checkRun` (`backend/internal/agents/authz.go`). It treats every message as a person's message: the run takes the manual idle timeout from `IdleTimeouts.ForPersonMessage` (`backend/internal/agents/idle_timeout.go`), and the run's event stream records a human message.

[Decision Models and the System One API](2026-10-10-systemone-decision-models.md) added `POST /v1/systemone` and the `decision` capability to the inference gateway. A `choice` question carries `instructions` and a `criteria` map from option name to description. Its answer returns `choice`, a `probabilities` map over every option and a `confidence` value. The route requires `inference:invoke`, and each virtual model requires `inference:<family>:<name>:use`. No typed client for the inference gateway exists in `backend/internal/platform/clients/`.

### Problem Statement

A trigger cannot choose among agents or send a delivery to a run that is already working. An event with a fixed shape and an unknown intent, such as a GitHub `issue_comment` that mentions `@kora`, needs one entry point that routes each delivery.

### Goals

- A webhook trigger can route each delivery to one of several candidate agents, or to an open run started by a trigger.
- The platform owns the questions and the thresholds.
- The routing trigger never acts with access beyond what its creator holds.
- Every routed delivery records the decision, the scores, the threshold and the outcome.

### Non-Goals

- Classifiers that tenants define, and a workflow primitive.
- Context beyond the delivery, such as earlier comments on an issue.
- A Slack webhook provider. Routing works on any webhook source, and a Slack provider is separate work.
- Routing for scheduled and manual triggers.
- Joining runs that a person started.

### Which of the Three Questions This Touches

**What can it access?** A routing trigger holds run scopes for every candidate agent and the use scope of its decision model. Create requires the creator to be able to run each candidate and to hold inference:invoke and the model's use scope. An update that changes what runs or when requires the same. A delivery can start or join only a run of one of the routing trigger's candidate agents, under the routing trigger's own token.

**Who is this agent?** A joined run keeps its identity. A run started by a routing trigger runs under the routing trigger's principal like any trigger run. The decision call is attributed to that principal.

**How does it connect?** orbit-triggers gains one east-west call to the inference gateway. The decision model's credentials stay in the gateway.

---

## Decision Drivers

- A routing failure or a low-confidence answer must never start an agent the author did not list.
- Joining a run must not widen what a delivery can reach.
- Routing must reuse the queue, coalescing, admission and audit paths that every trigger already uses.
- The decision call must be policy-checked, budgeted and attributed like other inference.

---

## Considered Options

### D1. Where Routing Runs

**Option 1a (chosen):** Route in `DispatchPending` after the worker claims the fire. The fire has already passed matching, sender restriction, admission and the coalescing hold, and the dispatch timeout bounds the call.

**Option 1b (rejected):** Route in `fanOut` at ingest. The decision call would run before admission and the coalescing hold. A burst would make one model call per fire that coalescing then discards.

**Option 1c (rejected):** Route inside orbit-agents. orbit-agents does not read trigger configuration or delivery history, and the key-first lookup needs both.

### D2. Calling the Decision Model

**Option 2a (chosen):** Add a typed client `backend/internal/platform/clients/inference` that embeds `httpclient.Base` and exposes `SystemOne`. orbit-triggers calls it with the routing trigger's SERVICE token, the same token it mints for `StartRun`.

**Option 2b (rejected):** Call the gateway with orbit-triggers' own platform identity. Usage and budgets would be attributed to the platform component, and the trigger's spend would not be visible.

### D3. Questions and State

**Option 3a (chosen):** Ask two `choice` questions in one call, defined in code with a version constant.

**Option 3b (rejected):** Let the author write the questions. Tenant-defined classifiers are a non-goal of the PRD.

**Option 3c (rejected):** Route with a chat model prompted for JSON. The answer would be generated text parsed into numbers, and a decision model returns calibrated probabilities.

### D4. Routing Text for an Agent

**Option 4a (chosen):** Use the agent type's `Name` and `Description`.

**Option 4b (rejected):** Add a dedicated routing field to the agent type. It needs a change to the agents API and a second text to keep in step.

**Option 4c (rejected):** Let the author write routing notes per candidate on the trigger. The same agent would need its notes repeated on every routing trigger.

### D5. Which Runs a Delivery Can Join

**Option 5a (chosen):** A delivery can join an `active` or `idle` run of a candidate agent of the routing trigger, started by any trigger. A run a person started is never joined. The message travels under the routing trigger's token.

**Option 5b (rejected):** A delivery can join any trigger-started run in the tenant. The routing trigger would need run write on every agent in the tenant, or the message would travel under a token with access the routing trigger does not hold.

**Option 5c (rejected):** Only runs started by the same trigger. A comment routed by one trigger could not reach a run that another trigger started for the same pull request.

### D6. How Key-First Joining Finds a Run

**Option 6a (chosen):** A routing `join_key` template, rendered per delivery like the coalesce key. The rendered key is stored on the routing decision, and a later delivery of the same trigger with the same key joins the run that decision recorded.

**Option 6b (rejected):** Reuse the coalesce key. Two quick comments on one issue would merge, and the first comment would never reach routing. Key-first joining would also require coalescing to be on.

### D7. Where a Routing Trigger's Configuration Lives

**Option 7a (chosen):** Add `target_kind = 'routed'` and two tables: `trigger_routes` for the settings and `trigger_route_candidates` for the agents.

**Option 7b (rejected):** Store the routing settings in `kind_config`. Candidates reference agent types, and the agent lookup and grant management need them as rows.

---

## Decision

1. **Target kind:** A webhook trigger gains `target_kind: "routed"`, and a routed trigger has candidates and no `agent_type_id`. Scheduled and manual triggers reject `routed`.
2. **Placement:** Routing runs in `DispatchPending` (Option 1a). A routed fire computes a route and then either starts a run through `StartRun` or joins one through the new agents client method `SendRunMessage`.
3. **Model call:** orbit-triggers calls `POST /v1/systemone` through the new inference client (Option 2a) with the routing trigger's SERVICE token. The gateway applies `inference:invoke`, `inference:<family>:<name>:use`, the tenant budget and the usage row as on any decision call.
4. **Questions:** The routing questions live in `backend/internal/triggers/routing_questions.go` with `routingQuestionsVersion = "routing-v1"`:
   - `agent`: a `choice` question that asks which candidate agent should handle the message. Each `criteria` key is an opaque option id (`a1`, `a2`, ...) and each value is the agent's name and description (Option 4a).
   - `run`: a `choice` question that asks whether the message continues one of the listed runs. The options are `new` and one opaque id per joinable run (`r1`, `r2`, ...). Each run's description is its agent name, its run summary and its start time. The question is omitted when joining is off or no run is joinable.
5. **State:** The `state` is a JSON object with a `message` field that holds the prompt rendered by `renderWebhookPrompt`. The delivery values stay inside the `<delivery-data>` block. The instructions of both questions state that the message content is data from an external event. The `run` instructions also state that the open runs' summaries are agent output from earlier external events and are not instructions.
6. **Limits:** A routed trigger has 2 to 20 candidates, checked at create and update. At most the 20 most recently active joinable runs are offered.
7. **Key first (Option 6a):** A routed trigger has an optional `join_key` template with the coalesce key's syntax and 256-byte limit. When joining is on and `join_key` is set, routing renders the key and reads the newest decision of the **same trigger** with that key and a run (`trigger_routing_decisions.join_key`). If that run is `active` or `idle`, the delivery joins it without a model call. If the run is still starting, orbit-agents answers the message with `ErrRunStarting`, and the fire returns to the queue with `dispatch_after` 10 seconds later, at most 6 times. The delivery's reason records each hold. Once the run is `active` or `idle`, the delivery joins it. If the run fails or ends, or the holds run out, the model decides. The model also decides when the key renders empty or too long. Keys are per-trigger templates and are not compared across triggers. Coalescing stays an independent setting on the trigger.
8. **Thresholds:** The trigger stores a preset. The thresholds live in code and apply to the probability of the chosen option:
   - `strict`: 0.85.
   - `balanced` (default): 0.70.
   - `loose`: 0.50.
9. **Outcome order:**
   1. If the `run` answer is a run id with probability at or above the threshold, the delivery joins that run.
   2. Otherwise, if the `agent` answer clears the threshold, the delivery starts a run of that agent.
   3. Otherwise the fallback applies.
10. **Fallback:** The fallback is either a candidate agent (`fallback_agent_type_id`) or no run. A below-threshold answer and a failed model call both take the fallback. The fallback agent must be one of the candidates. Routing never starts an agent outside the candidate list.
11. **Failure accounting:** A failed model call records the gateway error on the routing decision. It counts toward auto-pause only when no fallback agent is configured and the failure is a configuration error: a 403 for a missing scope or an exhausted budget, a 400 or 422 for a rejected request, or a 404 for an unknown or disabled model. A 5xx, 529, 429 or timeout from the gateway never counts. `countsTowardAutoPause` treats platform faults on a run start the same way. A below-threshold answer does not count toward auto-pause.
12. **Joinable runs (Option 5a):** orbit-triggers lists runs through `GET /api/v1/runs` with two new filters: `started_by=trigger` and repeated `agent_type_id`. Joinable runs have these properties:
    - The run is in the trigger's tenant.
    - A trigger started it (`StartedBy` set).
    - Its state is `active` or `idle`.
    - Its agent type is a candidate of the routing trigger.
13. **Join access:** `SendRunMessage` calls `RunService.SendMessage` with the routing trigger's SERVICE token, and `checkRun` requires `agents:runs:<slug>:write` for that token. A joined run's agent acts with its own agent type's grant. Joining a run of a candidate agent reaches no access beyond starting a new run of that agent.
14. **Trigger messages:** A message is a trigger message only when the caller is a SERVICE principal whose subject is a trigger (`trigger:<id>`) or a trigger source (`trigger-source:<name>`). `POST /api/v1/runs/{id}/messages` accepts optional `trigger_id`, `trigger_name` and `delivery_id`, checked as on a run start. A trigger's own principal that names no trigger is attributed to its own trigger. A trigger message keeps the run's trigger idle timeout and is sent to the session-manager with `fromPerson` false. The event stream records it as a trigger message with the trigger's name. Any other caller, including any other SERVICE principal, takes `ForPersonMessage` and the human message event, and a request from it that sets any of the three attribution fields is rejected with 403.
15. **Lost race:** If the chosen run leaves `active` or `idle` before the message arrives (`ErrRunStarting` or `ErrRunNotAcceptingMessages`), the delivery starts a new run of that run's agent and records the change.
16. **Grants:** A routed trigger's principal holds one grant with the following scopes:
    - `triggerRunScopes(slug)` for every candidate.
    - `inference:invoke`.
    - `inference:<family>:<name>:use` for the decision model.

    An update that changes the candidates or the model replaces the grant.

17. **Creation authority:** Create reads each candidate from orbit-agents with the caller's bearer and delegation. Each candidate must report `can_run`. A candidate without it rejects the request with 403 and names the agent. A candidate the caller cannot read is rejected with 400 as not found in the tenant, as for a single-agent trigger, so the response does not reveal that it exists. The policy evaluator orbit-triggers already holds (`policy.NewEvaluator` in `backend/cmd/orbit-triggers/main.go`) checks `inference:invoke` and the model's use scope against the caller's claims. A missing scope rejects the request with 403 and lists the missing scopes. An update that changes what runs or when applies the same checks to the current candidates, and a candidate change applies them to the new ones as well. Those updates change the candidates, the model, the fallback agent, the join settings, the event, the filters, the prompt template or enablement. A rename or a description change needs only `triggers:write`. A replay or a source change requires `can_run` on every candidate of a routed trigger on the source. `CreateTrigger` and `UpdateTrigger` gain the caller's claims as a parameter.
18. **Delegated agents:** `refuseDelegatedBinding` applies to every candidate and to the fallback agent.

### Data Model

The migration is generated with `make migrate-create service=orbit-triggers name=add_trigger_routing`:

```sql
-- +goose Up
-- +goose StatementBegin
ALTER TABLE triggers DROP CONSTRAINT triggers_target_kind_check;
ALTER TABLE triggers ALTER COLUMN agent_type_id DROP NOT NULL;
ALTER TABLE triggers ALTER COLUMN agent_slug DROP NOT NULL;
ALTER TABLE triggers ADD CONSTRAINT triggers_target_kind_check CHECK (
    (target_kind = 'agent' AND agent_type_id IS NOT NULL)
    OR (target_kind = 'routed' AND agent_type_id IS NULL)
);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE trigger_routes (
    trigger_id TEXT NOT NULL,
    tenant_id TEXT NOT NULL,
    model TEXT NOT NULL,
    confidence TEXT DEFAULT 'balanced' NOT NULL,
    fallback_agent_type_id TEXT,
    join_runs BOOLEAN DEFAULT true NOT NULL,
    join_key TEXT,
    CONSTRAINT trigger_routes_pkey PRIMARY KEY (trigger_id),
    CONSTRAINT trigger_routes_trigger_id_fkey FOREIGN KEY (trigger_id) REFERENCES triggers(id) ON DELETE CASCADE,
    CONSTRAINT trigger_routes_confidence_check CHECK (confidence IN ('strict', 'balanced', 'loose')),
    CONSTRAINT trigger_routes_join_key_length_check CHECK (join_key IS NULL OR octet_length(join_key) <= 256)
);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE trigger_route_candidates (
    trigger_id TEXT NOT NULL,
    agent_type_id TEXT NOT NULL,
    agent_slug TEXT NOT NULL,
    CONSTRAINT trigger_route_candidates_pkey PRIMARY KEY (trigger_id, agent_type_id),
    CONSTRAINT trigger_route_candidates_trigger_id_fkey FOREIGN KEY (trigger_id) REFERENCES triggers(id) ON DELETE CASCADE
);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE trigger_routing_decisions (
    delivery_id TEXT NOT NULL,
    trigger_id TEXT NOT NULL,
    tenant_id TEXT NOT NULL,
    question_version TEXT NOT NULL,
    model TEXT,
    outcome TEXT NOT NULL,
    join_key TEXT,
    key_matched BOOLEAN DEFAULT false NOT NULL,
    agent_type_id TEXT,
    run_id TEXT,
    threshold NUMERIC(4, 3),
    probability NUMERIC(4, 3),
    confidence NUMERIC(4, 3),
    scores JSONB DEFAULT '{}'::jsonb NOT NULL,
    fallback_reason TEXT,
    error TEXT,
    created_at TIMESTAMPTZ DEFAULT now() NOT NULL,
    CONSTRAINT trigger_routing_decisions_pkey PRIMARY KEY (delivery_id, trigger_id),
    CONSTRAINT trigger_routing_decisions_delivery_id_fkey FOREIGN KEY (delivery_id) REFERENCES trigger_deliveries(id) ON DELETE CASCADE,
    CONSTRAINT trigger_routing_decisions_outcome_check CHECK (outcome IN ('new_run', 'joined_run', 'fallback_run', 'no_run')),
    CONSTRAINT trigger_routing_decisions_join_key_length_check CHECK (join_key IS NULL OR octet_length(join_key) <= 256)
);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE INDEX trigger_routing_decisions_join_key_idx
    ON trigger_routing_decisions (trigger_id, join_key, created_at DESC)
    WHERE join_key IS NOT NULL AND run_id IS NOT NULL;
-- +goose StatementEnd

-- +goose StatementBegin
ALTER TABLE trigger_pending_deliveries ADD COLUMN hold_count SMALLINT DEFAULT 0 NOT NULL;
-- +goose StatementEnd

-- +goose StatementBegin
ALTER TABLE trigger_delivery_runs DROP CONSTRAINT trigger_delivery_runs_status_check;
ALTER TABLE trigger_delivery_runs ADD CONSTRAINT trigger_delivery_runs_status_check
    CHECK (status IN ('dispatched', 'joined', 'unrouted', 'failed', 'filtered', 'denied', 'superseded', 'dropped', 'paused'));
-- +goose StatementEnd
```

The Down migration reverses each step. The `trigger_delivery_runs` status `joined` records a delivery that joined a run and carries its `run_id`. `unrouted` records a delivery that started nothing under the no-run fallback. `scores` holds the probability of every option, keyed by agent type id and run id. `join_key` holds the rendered key whenever one rendered. The decision that started a run and every decision that joined it carry the same key. The key-first lookup reads the newest row for `(trigger_id, join_key)` with a run through `trigger_routing_decisions_join_key_idx`. A key-first join records `key_matched = true` and no model, scores or threshold. `hold_count` counts the times a fire returned to the queue to wait for a starting run. Retention deletes decisions with their delivery through the foreign key.

### API

Trigger create and update accept a `routing` object on a webhook trigger with `target_kind: "routed"`:

```json
{
  "kind": "webhook",
  "target_kind": "routed",
  "routing": {
    "candidates": ["at_code_reviewer", "at_coder", "at_security_reviewer"],
    "model": "liquid/d1",
    "confidence": "balanced",
    "fallback_agent_type_id": "at_code_reviewer",
    "join_runs": true,
    "join_key": "{{ repository.full_name }}#{{ issue.number }}"
  }
}
```

`fallback_agent_type_id: null` selects the no-run fallback, `join_key` is optional and validated like `coalesce.key`, and an update with `routing` replaces the stored object. Changing `target_kind` after create is rejected.

A delivery run row for a routed trigger carries the decision:

```json
{
  "status": "dispatched",
  "run_id": "run_7c21e4b2",
  "routing": {
    "outcome": "new_run",
    "key_matched": false,
    "join_key": "acme/platform#1427",
    "model": "liquid/d1",
    "agent_type_id": "at_code_reviewer",
    "probability": 0.91,
    "threshold": 0.7,
    "scores": {
      "agents": {
        "at_code_reviewer": 0.91,
        "at_security_reviewer": 0.06,
        "at_coder": 0.03
      }
    },
    "fallback_reason": null,
    "error": null
  }
}
```

The API does not return `question_version`.

### Console

The target step and the delivery detail follow the routing stories under `Pages/Triggers`. The model list reads `GET /api/models` and keeps models whose `capabilities` include `decision`. orbit-triggers adds `GET /api/v1/triggers/routing/candidates` to return the tenant's agent types with a `runnable` flag from orbit-agents' `can_run` for the caller. The list holds the agent types the caller may read. The wizard lists non-runnable and delegated agents as disabled with their reason.

---

## Consequences

### Positive

- One webhook source can serve several agents through one trigger.
- Follow-ups with the same join key reach the run that is already working without a model call.
- Routing decisions sit in the delivery history beside the run outcomes.

### Negative

- Every routed fire that misses the key adds one decision model call to dispatch latency and spend.
- A delivery can join a run that another trigger started. The joined run's agent receives a message from a source its own trigger does not listen to.
- A routed trigger's grant spans every candidate. Removing a candidate requires an update to replace the grant.
- An update that replaces the grant stores the trigger only while its stored grant is still the one it replaced. A concurrent update that replaced it first wins, and the other deletes its new grant and answers 409. Revoking the replaced grant is retried a few times. A revocation that still fails is returned as an error, and the log names the grant that is still live. Durable cleanup of such a grant is follow-up work.
- Routing quality depends on agent descriptions that agent owners write for people.
- orbit-triggers needs mesh access to the inference gateway: a DestinationRule entry and an AuthorizationPolicy principal.

### Neutral

- Single-agent triggers keep their behavior and their API.
- The inference gateway, the System One route and the decision catalog do not change.

---

## Implementation

1.  `backend/internal/platform/clients/inference/client.go`: the typed client with `SystemOne`, and `ErrModelNotFound` for a 404.
2.  `backend/internal/platform/clients/agents/client.go`: `SendRunMessage` with `ErrRunStarting` apart from `ErrRunNotAcceptingMessages`, `ListRuns` with the new filters, and `ListAgentTypes` with each agent type's name and description.
3.  `backend/internal/agents/server.go` and `backend/internal/agents/postgres.go`: the `started_by=trigger` and repeated `agent_type_id` filters on `GET /api/v1/runs`, and a `reason` detail on the 409 of `POST /api/v1/runs/{id}/messages` that tells a starting run apart from a closed one.
4.  `backend/internal/agents/idle_timeout.go` and `backend/internal/agents/run_service.go`: the attribution fields, the idle timeout and the event for trigger messages.
5.  `backend/cmd/orbit-triggers/migrations/`: the `add_trigger_routing` migration.
6.  `backend/internal/triggers/`: the `routing` model and validation, `routing_questions.go`, the routing step in `DispatchPending`, the repository methods for routes, candidates, decisions and the key lookup, and the caller checks on create and on updates that change what runs or when.
7.  `backend/cmd/orbit-triggers/config/base.yaml`, `backend/cmd/orbit-triggers/main.go` and `charts/orbit/`: the inference gateway URL, the client and the mesh wiring.
8.  `frontend/src/app/(dashboard)/triggers/`: the target step and the Routing section of the delivery detail built from the proposed components.

---

## Validation

Unit tests cover:

- Outcome order: a run answer above the threshold, an agent answer above it, both below, and a failed call with and without a fallback agent.
- Key-first joining: a match on an open run, a match on a closed run, a match on a starting run until it opens and until the holds run out, no `join_key`, a key that renders empty or too long, and the same key on another trigger.
- Failure accounting: a configuration error and a platform fault from the gateway, each with and without a fallback agent.
- The lost race between the decision and `SendRunMessage`.
- Creation authority: a caller who cannot run one candidate, a caller missing inference:invoke or the model use scope, a creator without explicit run scopes, and a rename without run access.
- Validation of candidate counts, a fallback outside the candidates, and a delegated candidate.

`TestPostgres_*` tests cover the join-key lookup on `trigger_routing_decisions` (newest row with a run, scoped to the trigger), the routing decision write and the hold that returns a fire to the queue.

---

## Related Decisions

- [Decision Models and the System One API](2026-10-10-systemone-decision-models.md)
- [Trigger Fire Coalescing](2026-10-13-trigger-fire-coalescing.md)
- [Trigger Payload Templating](2026-09-28-trigger-payload-templating.md)
- [Extended Trigger Types and an Operable Delivery Pipeline](2026-06-20-extended-trigger-types.md)
- [Agent Triggers and Webhook Ingestion](2026-06-17-agent-triggers-and-webhook-ingestion.md)
- [Policy Gateway and Authorisation Model](2026-04-14-policy-gateway-and-authorisation-model.md)

---

## References

- [TypeSafe Choice questions](https://docs.typesafe.ai/primitives/choice)
