'use client';

/** @tier 3 · triggers-scoped. The event kind: a platform event in this tenant starts a run. */

import { useEffect, useState } from 'react';
import { SectionLabel, Stack, Text, ToggleRow } from '@runkora/hades';
import { useBrand } from '@runkora/theme/brand-provider';
import {
  describeMatchCondition,
  matchConditionFrom,
  matchContainsFrom,
  validateFilters,
  validateNamedPromptStep,
} from '../wizard-state';
import { PromptField, TriggerNameField } from '../../trigger-fields';
import { CatalogPicker } from '../../catalog-picker';
import { AddFilterButton, FilterRowsEditor } from '../../filter-rows-editor';
import { eventCatalog, eventTypeCatalog, toTemplateCatalog } from '../field-catalog';
import { fetchPlatformEventTypes } from '../../../_services/triggers-api';
import type { StepComponentProps, TriggerKindDefinition, WizardState } from '../types';

/** Loads the platform event catalog once per step. */
function usePlatformEventTypes(): { eventTypes: string[]; error: string | null } {
  const [answer, setAnswer] = useState<{ eventTypes: string[]; error: string | null }>({
    eventTypes: [],
    error: null,
  });
  useEffect(() => {
    let live = true;
    void fetchPlatformEventTypes().then((res) => {
      if (live) setAnswer(res);
    });
    return () => {
      live = false;
    };
  }, []);
  return answer;
}

function StepEvent({ state, patch }: StepComponentProps) {
  const { productName } = useBrand();
  const catalog = usePlatformEventTypes();
  const fields = toTemplateCatalog(eventCatalog());
  return (
    <Stack gap="relaxed">
      <TriggerNameField
        value={state.name}
        onChange={(name) => patch({ name })}
        hint="Shown in the triggers list and on every delivery."
        placeholder="on-run-failed"
      />

      <Stack gap="snug">
        <Stack gap="tight">
          <SectionLabel>Event</SectionLabel>
          <Text size="caption" color="subtle">
            {`The ${productName} event this trigger listens for. Only events in this tenant fire it.`}
          </Text>
        </Stack>
        <Stack direction="horizontal" gap="tight" align="center" paddingInline="snug">
          <Text as="span" size="supporting" color="muted">
            Listen for
          </Text>
          <CatalogPicker
            inline
            aria-label="Event type"
            value={state.eventType}
            placeholder="Choose an event"
            catalog={eventTypeCatalog(catalog.eventTypes, [])}
            searchPlaceholder="Search events…"
            onSelect={(eventType) => patch({ eventType })}
          />
        </Stack>
        {catalog.error !== null && (
          <Text size="caption" color="status-error">
            Could not load the event types. Close the wizard and open it again to retry.
          </Text>
        )}
      </Stack>

      <Stack gap="snug">
        <Stack gap="tight">
          <SectionLabel>Filters</SectionLabel>
          <Text size="caption" color="subtle">
            Only events matching every filter start a run. Fields are dotted paths in the event,
            such as data.entity.state. Values are alternatives.
          </Text>
        </Stack>
        <FilterRowsEditor
          rows={state.filters}
          fields={fields}
          onChange={(filters) => patch({ filters })}
          freeText
          hideAdd
        />
        <AddFilterButton rows={state.filters} onChange={(filters) => patch({ filters })} />
      </Stack>

      <ToggleRow
        label="Ignore its own runs"
        description="Skip events about runs this trigger started or messaged. Turn off only for a trigger meant to react to its own work."
        checked={state.ignoreOwnRuns}
        onChange={(ignoreOwnRuns) => patch({ ignoreOwnRuns })}
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
