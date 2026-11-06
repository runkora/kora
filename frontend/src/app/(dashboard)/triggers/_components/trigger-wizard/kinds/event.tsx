'use client';

/** @tier 3 · triggers-scoped. The event kind: a platform event in this tenant starts a run. */

import { Stack } from '@runkora/hades';
import {
  describeMatchCondition,
  matchConditionFrom,
  matchContainsFrom,
  validateFilters,
  validateNamedPromptStep,
} from '../wizard-state';
import { PromptField, TriggerNameField } from '../../trigger-fields';
import { PlatformEventFields } from '../../platform-event-fields';
import { eventCatalog, toTemplateCatalog } from '../field-catalog';
import type { StepComponentProps, TriggerKindDefinition, WizardState } from '../types';

function StepEvent({ state, patch }: StepComponentProps) {
  const fields = toTemplateCatalog(eventCatalog());
  return (
    <Stack gap="relaxed">
      <TriggerNameField
        value={state.name}
        onChange={(name) => patch({ name })}
        hint="Shown in the triggers list and on every delivery."
        placeholder="on-run-failed"
      />

      <PlatformEventFields
        eventType={state.eventType}
        filters={state.filters}
        ignoreOwnRuns={state.ignoreOwnRuns}
        onEventTypeChange={(eventType) => patch({ eventType })}
        onFiltersChange={(filters) => patch({ filters })}
        onIgnoreOwnRunsChange={(ignoreOwnRuns) => patch({ ignoreOwnRuns })}
      />

      <PromptField
        value={state.promptTemplate}
        onChange={(next) => patch({ promptTemplate: next })}
        hint="What the run starts with. The whole event follows it in a block marked as untrusted data."
        catalog={fields}
        catalogLabel="Fields every event carries. Type any other path in the event."
        placeholder="Run {{ korarun }} of {{ koraagent }} failed with {{ data.entity.error }}. Find the cause."
      />
    </Stack>
  );
}

function validateEvent(state: WizardState) {
  return validateNamedPromptStep(state, (s) => {
    if (!s.eventType.trim())
      return { valid: false, hint: 'Choose the event this trigger listens for.' };
    return validateFilters(s.filters);
  });
}

function DoneContent() {
  return null;
}

export const eventKind: TriggerKindDefinition = {
  id: 'event',
  apiKind: 'event',
  icon: 'activity',
  title: 'Platform event',
  description: (productName) =>
    `When something happens inside ${productName}: a run fails, a budget is crossed.`,
  example: 'agents.run.failed.v1',
  steps: [
    {
      id: 'event',
      railTitle: 'Event',
      railSub: 'What it listens for',
      description: (productName) =>
        `Choose the ${productName} event that starts a run, which of them match, and what the agent is asked to do.`,
      Component: StepEvent,
      validate: validateEvent,
    },
  ],
  summarize: (state) => {
    const rows = [
      { key: 'Event', value: state.eventType, mono: true },
      { key: 'Own runs', value: state.ignoreOwnRuns ? 'Ignored' : 'Fire on them' },
    ];
    const filters = describeMatchCondition(state.filters);
    if (filters) rows.push({ key: 'Filters', value: filters, mono: true });
    return rows;
  },
  draftPatch: (state) => {
    const matchCondition = matchConditionFrom(state.filters);
    const matchContains = matchContainsFrom(state.filters);
    return {
      eventType: state.eventType.trim(),
      ignoreOwnRuns: state.ignoreOwnRuns,
      ...(matchCondition ? { matchCondition } : {}),
      ...(matchContains ? { matchContains } : {}),
    };
  },
  fieldCatalog: () => eventCatalog(),
  doneContent: DoneContent,
};
