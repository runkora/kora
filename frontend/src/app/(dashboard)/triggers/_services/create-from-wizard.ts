/**
 * @tier 3 — triggers-owned, with two feature-area consumers: the Triggers page
 * (`triggers/_components/triggers-page.tsx`) and the agent detail page
 * (`agents/[slug]/_components/agent-triggers-tab.tsx`), which imports it across
 * feature areas. Deliberately left here beside the wizard it executes rather
 * than promoted to `src/components/`: the two move together, and splitting the
 * draft type from the call that consumes it would buy nothing.
 *
 * Executes a wizard draft against the API.
 *
 * Shared by the platform Triggers page and the agent detail page so a trigger
 * created from either surface is created the same way. Kept out of both
 * components because the ordering matters: the source has to exist before the
 * trigger that names it, and a failure between the two leaves a real source
 * behind that the retry can reuse.
 */

import { createSource, createTrigger } from './triggers-api';
import { endpointFor } from '../_lib/endpoints';
import { toScheduleConfigRequest } from '../_types';
import type { TriggerCreateResult, TriggerDraft } from '../_components/trigger-wizard';

export interface CreateFromWizardOptions {
  /** Public base for webhook endpoints, e.g. "api.orbit.example.com". */
  ingestHost: string;
}

export async function createFromWizardDraft(
  draft: TriggerDraft,
  { ingestHost }: CreateFromWizardOptions,
): Promise<TriggerCreateResult> {
  let sourceId: string | undefined;
  let secret: string | undefined;

  if (draft.kind === 'webhook') {
    if (draft.source === undefined) return { ok: false, error: 'Choose a source.' };
    if ('existingId' in draft.source) {
      sourceId = draft.source.existingId;
    } else {
      const made = await createSource({
        name: draft.source.newName,
        provider: draft.source.provider,
      });
      if (made.error !== null || made.source === null) {
        return { ok: false, error: made.error ?? 'Failed to create the source.' };
      }
      sourceId = made.source.id;
      secret = made.source.secret;
    }
  }

  const made = await createTrigger({
    kind: draft.kind,
    name: draft.name,
    agent_type_id: draft.agentTypeId,
    prompt_template: draft.promptTemplate,
    ...(sourceId !== undefined && { source_id: sourceId }),
    ...(draft.kind === 'webhook' && { event_type: draft.eventType ?? '' }),
    ...(draft.matchCondition !== undefined && { match_condition: draft.matchCondition }),
    ...(draft.matchContains !== undefined && { match_contains: draft.matchContains }),
    ...(draft.senderRestriction !== undefined && {
      sender_restriction: draft.senderRestriction,
    }),
    ...(draft.kind === 'webhook' && draft.coalesce !== undefined && { coalesce: draft.coalesce }),
    ...(draft.schedule !== undefined && {
      schedule_config: toScheduleConfigRequest(draft.schedule),
    }),
    ...(draft.manualInputs !== undefined && {
      manual_config: {
        inputs: draft.manualInputs.map((f) => ({
          name: f.name,
          label: f.label,
          type: f.type,
          required: f.required,
          ...(f.choices !== undefined && { choices: f.choices }),
        })),
      },
    }),
    max_parallel: draft.maxParallel,
    overflow: draft.overflow,
    max_queue_depth: draft.maxQueueDepth,
    timeout_seconds: draft.timeoutSeconds,
    dedupe_enabled: draft.dedupeEnabled,
    autopause_enabled: draft.autopauseEnabled,
  });
  if (made.error !== null || made.trigger === null) {
    // A just-created source outlives the failure on purpose: it is real, and
    // the retry can pick it from the existing-sources list.
    return { ok: false, error: made.error ?? 'Failed to create the trigger.' };
  }

  return {
    ok: true,
    triggerId: made.trigger.id,
    ...(sourceId !== undefined && {
      endpoint: endpointFor(ingestHost, sourceId),
    }),
    ...(secret !== undefined && { secret }),
  };
}
