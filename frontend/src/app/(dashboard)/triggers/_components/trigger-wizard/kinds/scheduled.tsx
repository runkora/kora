'use client';

/** @tier 3 — triggers-scoped. The scheduled kind: a cron builder that always
 * shows the expression it derives, plus timezone and catch-up. */

import { Card, CardContent, Icon, Stack, Text } from '@runkora/hades';
import { cronFor, humanFor } from '../cron';
import { VALID, validateNamedPromptStep } from '../wizard-state';
import { PromptField, TriggerNameField } from '../../trigger-fields';
import { ScheduleBuilder } from '../../schedule-builder';
import { scheduledCatalog, toTemplateCatalog } from '../field-catalog';
import type {
  DoneContentProps,
  StepComponentProps,
  TriggerKindDefinition,
  WizardState,
} from '../types';

function StepSchedule({ state, patch }: StepComponentProps) {
  return (
    <Stack gap="relaxed">
      <TriggerNameField
        value={state.name}
        onChange={(name) => patch({ name })}
        hint="Shown in the triggers list and on every fire."
        placeholder="nightly-report"
      />

      <ScheduleBuilder value={state} onChange={patch} />

      <PromptField
        value={state.promptTemplate}
        onChange={(next) => patch({ promptTemplate: next })}
        hint="What the run starts with. Every fire uses this prompt."
        catalog={toTemplateCatalog(scheduledCatalog())}
        catalogLabel="Filled by the scheduler at each fire."
        placeholder="Summarise yesterday's failed deliveries and open an issue for anything unresolved."
      />
    </Stack>
  );
}

function validateSchedule(state: WizardState) {
  return validateNamedPromptStep(state, (s) => {
    if (!cronFor(s).trim())
      return { valid: false, hint: 'The schedule does not resolve to a cron expression.' };
    return VALID;
  });
}

function DoneContent({ state }: DoneContentProps) {
  return (
    <Stack minWidth="prose" maxWidth="prose">
      <Card inset>
        <CardContent padded compact>
          <Stack direction="horizontal" align="center" gap="cozy">
            <Icon name="clock" size="sm" color="accent" label="" shrink />
            <Stack gap="tight" minWidth="none">
              <Text as="span" size="supporting" weight="heavy">
                {humanFor(state)}
              </Text>
              <Text as="span" size="caption" mono color="muted">
                {`${cronFor(state)} · ${state.timezone}`}
              </Text>
            </Stack>
          </Stack>
        </CardContent>
      </Card>
    </Stack>
  );
}

export const scheduledKind: TriggerKindDefinition = {
  id: 'scheduled',
  apiKind: 'scheduled',
  icon: 'clock',
  title: 'Schedule',
  description: 'Run on a recurring cadence, in a timezone you choose.',
  example: 'Every day at 02:00 · every 15m',
  // A schedule that overlaps itself is nearly always a mistake, so it starts
  // serial and drops the stale tick rather than queueing a backlog.
  defaults: { maxParallel: 1, overflow: 'replace' },
  steps: [
    {
      id: 'schedule',
      railTitle: 'Schedule',
      railSub: 'When it fires',
      description:
        'Pick how often the agent should run and in which timezone. Nothing fires until you create the trigger.',
      Component: StepSchedule,
      validate: validateSchedule,
    },
  ],
  summarize: (state) => [
    { key: 'Runs', value: humanFor(state) },
    { key: 'Cron', value: cronFor(state), mono: true },
    { key: 'Timezone', value: state.timezone, mono: true },
    { key: 'Catch-up', value: state.catchUp ? 'On' : 'Off' },
  ],
  draftPatch: (state) => ({
    schedule: {
      cronExpression: cronFor(state),
      timezone: state.timezone,
      catchUp: state.catchUp,
    },
  }),
  fieldCatalog: () => scheduledCatalog(),
  doneContent: DoneContent,
};
