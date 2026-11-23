# Generic Webhook Attribution

**Status:** Proposed  
**Date:** 2026-11-23  
**Deciders:** Justin Wilkin  
**Technical Story:** [Generic Webhook Attribution PRD](../prd/generic-webhook-attribution.md)

---

## Context

A run that a trigger starts or joins carries source links and an event summary. kora-triggers computes both at ingest (`service_ingest.go`) through two optional adapter interfaces and sends them to kora-agents with the start or join request:

*   `sourceLinker` (`backend/internal/triggers/source_links.go`) returns `[]agentsclient.RunSource`.
*   `eventSummarizer` (`backend/internal/triggers/event_summary.go`) returns `*agentsclient.RunEventSummary`. `setEventSummary` clamps it to the `MaxEventSummary` limits and drops a summary without an actor name, a verb or an object label.

The GitHub, Slack and Alertmanager adapters implement both interfaces in Go. The generic adapter (`backend/internal/triggers/adapter_generic.go`) implements neither.

The following parts already exist and need no change:

*   `Registry.ForSource` builds the generic adapter from the source's config for every delivery. The adapter that ingest passes to `addSourceLinks` and `addEventSummary` already carries the config.
*   The `TriggerSource` CRD carries `spec.config` as raw JSON (`backend/internal/platform/declarative/v1alpha1/types.go`).
*   `postgresRepository.UpdateSource` writes the `config` column and publishes `triggers.source.updated.v1` with `config` in `changed` when the config differs.
*   `SourceService.refuseUpdate` refuses every API change to a declared source.
*   kora-agents accepts an empty `provider` on a run source. The run page shows the fallback icon for it (`frontend/src/components/run-source-chips.tsx`).

kora-agents validates run sources in `authorizeRunSources` (`backend/internal/agents/run_sources.go`). It accepts at most 10 sources per request, a label of 1 to 100 runes, and an absolute `https` URL of at most 2,048 bytes. One invalid source refuses the whole start or join request.

### Security and Identity

The change touches no agent identity, grant or credential. Attribution is display data on a run. Templates hold fixed text and field paths and run no code or expression. Values come only from deliveries that passed the source's verification.

### Goals

*   Add an `attribution` block to the generic source config that produces source links and an event summary.
*   Accept the block from the API, the console and a `TriggerSource`.
*   Let the API replace the attribution of an API source after create.
*   Leave the built-in adapters and kora-agents unchanged.

### Non-Goals

*   A quote or a preview in a generic event summary.
*   A provider value, name or icon per generic source.
*   Changes to verification or event type settings after create.

---

## Decision Drivers

*   **No run fails over attribution:** A link or summary that kora-agents would refuse must not reach it.
*   **One validation path:** The API, the console and a `TriggerSource` must reach the same validation in `parseGenericConfig`.
*   **No admin-supplied code:** A template holds fixed text and field paths only.

---

## Decision 1: Where Attribution Is Stored

### Option 1A (chosen): An `attribution` key in the generic config

`genericConfig` gains an `attribution` field next to `verification` and `event_type`. The block is stored in `webhook_sources.config`, carried in `TriggerSource.spec.config`, and validated by `parseGenericConfig`. A config without `attribution` behaves as today.

### Option 1B (rejected): A separate column or table

A new column or table would need a migration, a new CRD field and a second path through the declared reconcile. The config column, the CRD field and the update event already carry the block.

---

## Decision 2: Template Syntax

### Option 2A (chosen): Fixed text with `{{ path }}` placeholders

Every attribution string is a template in the placeholder syntax that trigger prompts use (`templateVarPattern` in `backend/internal/triggers/template.go`). A placeholder holds a dotted path into the delivery payload in the `fieldKeyPattern` grammar (`backend/internal/triggers/payload.go`). The adapter resolves each path with `resolvePath` against the decoded payload. A path the payload does not contain renders empty. A template with no placeholder renders as written.

### Option 2B (rejected): Field paths only

Each attribution value would be one field path. A sender that sends ids and no URL could not have a link. A link URL needs a fixed host and fixed path segments around the ids.

### Option 2C (rejected): Go `text/template`

`text/template` adds conditionals, functions and pipelines. The generic webhook source excludes admin-supplied logic of this kind.

---

## Decision 3: Config Shape and Validation

The `attribution` block takes these keys:

```yaml
config:
  verification: { method: hmac_sha256, header: X-Signature }
  event_type: { header: X-Event }
  attribution:
    links:
      - label: "#{{ data.number }} {{ data.title }}"
        url: "https://tracker.example.com/projects/{{ data.project_id }}/issues/{{ data.id }}"
      - label: "{{ data.url }}"
        url: "{{ data.url }}"
    actor:
      name: "Tracker"
      kind: system
    object:
      label: "#{{ data.number }} {{ data.title }}"
      url: "https://tracker.example.com/projects/{{ data.project_id }}/issues/{{ data.id }}"
    verbs:
      issue.created: created
      issue.comment.created: commented on
```

`parseGenericConfig` refuses a block that breaks one of these rules:

*   `genericKeys` and `checkGenericKeys` check the keys of `attribution`, of each link, of `actor` and of `object` for exact spelling.
*   `links` holds at most 10 entries, and each entry has a non-empty `label` and `url`.
*   `actor.name` and `object.label` are set together or not at all.
*   `actor.kind` is `person`, `bot` or `system`, and is required when `actor` is set.
*   `actor.url` and `object.url` are optional.
*   Every placeholder in every template matches `fieldKeyPattern`.
*   A `url` template is at most 2,048 bytes, and every other template and every `verbs` key and value is at most 300 runes.

---

## Decision 4: Rendering Links and the Summary

`SourceLinks` and `EventSummary` on the generic adapter return nil for a config without an `attribution` block.

### Links

`SourceLinks` renders each link and keeps it when all of these hold:

*   The rendered URL parses as an absolute `https` URL with a host and is at most 2,048 bytes.
*   The rendered label is non-empty after `cleanText`.

The adapter truncates a kept label to 100 runes. Every kept link has kind `trigger` and an empty provider. kora-agents removes a URL the run already has.

### Option 4A (chosen): Substitute values as they are and check the rendered URL

The adapter inserts each resolved value into the URL template without escaping. A template that is a single placeholder, such as `{{ data.url }}`, passes a URL from the payload through.

### Option 4B (rejected): Path-escape every value in a URL template

Escaping would turn a URL from the payload into one escaped path segment. Templates such as `{{ data.url }}` could not be written.

### Summary

`EventSummary` fills the summary fields as follows:

*   The actor name, the actor URL, the object label and the object URL are rendered templates.
*   The actor kind is the configured kind.
*   The verb is `verbs[eventType]`, or the event type when `verbs` has no entry for it.
*   An actor URL or object URL that is not an http or https URL (`agentsclient.ValidEventLink`) is left empty.

---

## Decision 5: Changing Attribution After Create

### Option 5A (chosen): An `attribution` field on the source update request

`UpdateSourceRequest` gains `Attribution *json.RawMessage`. When present, the value replaces the whole attribution block, and an empty object removes it. `UpdateSource` refuses the field on a source whose provider is not `generic`, merges the block into the stored config, and validates the result through `Registry.ForSource`.

*   A declared source returns `declarative.ErrManagedByResource` for the field.
*   `refuseUpdate` refuses an attribution change in a tenant whose API authoring is disabled with `declarative.ErrAPIAuthoringDisabled`.
*   An attribution change needs no `requireRunAccessForSource` check. It changes no run that a trigger starts.
*   The existing `triggers.source.updated.v1` event records the change.

### Option 5B (rejected): Accept the whole `config` on update

The API would accept a full config and refuse a change to `verification` or `event_type`. The update would have to compare two configs field by field to enforce what Option 5A enforces by accepting one key.

---

## Decision 6: Console

*   `GenericSourceDraft` (`frontend/src/app/(dashboard)/triggers/_lib/generic-source.ts`) gains the attribution fields and writes them into `config.attribution` on create.
*   `generic-source-setup.tsx` gains an attribution section with a list of links, the actor, the object and the verb map.
*   `trigger-detail-modal.tsx` gains an attribution editor for a generic API source next to secret rotation. The editor calls the source update with `attribution`. It shows the attribution read-only for a declared source.
*   An editor change applies to every trigger on the source. The editor states this next to the save action.

Hades covers the form fields. A missing component goes through the Hades proposal process in `AGENTS.md`.

---

## Rationale

None at this time.

---

## Consequences

### Positive

*   Any sender of webhooks gets source links and an event summary without a kora release.
*   kora-agents, the CRD schema and the database schema stay unchanged.

### Negative

*   A template with a mistyped path saves and renders empty. The admin sees the error only on the run page.
*   Values the payload does not carry, such as a user name or a project prefix, cannot appear in a label.
*   Links from every generic source show the same fallback icon.

### Neutral

*   This decision supersedes in part the non-goal "Editing a generic source's config through the API after create" in the [Generic Webhook Source ADR](2026-10-27-generic-webhook-source.md).

---

## Implementation

1.  **Backend:**
    *   Add the attribution config type, its validation and the `genericKeys` entries in `adapter_generic.go`.
    *   Implement the link and summary rendering of Decision 4.
    *   Add `Attribution` to `UpdateSourceRequest` and handle it in `UpdateSource` and `refuseUpdate`.
    *   Add unit tests with generic payload fixtures for validation, rendering, link dropping and the update path.
    *   Update `docs/guides/create-a-trigger.md` and `docs/guides/declarative-resources.md` with the attribution block.
2.  **Frontend:** Add the attribution section to the generic source setup and the attribution editor to the trigger detail modal.
3.  **Verification:** On the local cluster, create a generic source with attribution through the API and through a `TriggerSource`, replay a delivery, and confirm the links and the summary on the run page. Change the attribution through the API and confirm that a declared source refuses the change.

---

## Related Decisions

*   [Generic Webhook Source](2026-10-27-generic-webhook-source.md)
*   [Run Sources](2026-11-07-run-sources.md)
*   [Run Page Record](2026-11-11-run-page-record.md)
*   [Trigger Payload Templating](2026-09-28-trigger-payload-templating.md)
*   [Declarative Resources](2026-10-24-declarative-resources.md)
