'use client';

/** @tier 3 — triggers-scoped. Shared Review step: the configuration grouped by
 * the step that owns it, each group jumping back to its step for edits. */

import { Alert, Stack, Text, Wizard } from '@orbit/hades';
import { coalesceDraftOf, coalesceFrom, parallelWord } from './wizard-state';
import type { StepComponentProps } from './types';

export function StepReview({ state, context, definition, error, onGoTo }: StepComponentProps) {
  const agent = context.agents.find((a) => a.id === state.agentTypeId);
  const configStep = definition.steps[0];

  const coalesce = coalesceFrom(coalesceDraftOf(state));
  const coalesceRows =
    definition.id === 'webhook' && state.overflow === 'wait'
      ? [
          {
            label: 'Coalescing',
            value: coalesce ? `${coalesce.key} within ${coalesce.windowSeconds}s` : 'Off',
            mono: coalesce !== undefined,
          },
        ]
      : [];

  const configRows = [
    { label: 'Name', value: state.name || '—', mono: true },
    { label: 'Kind', value: definition.title },
    ...definition
      .summarize(state, context)
      .map((r) => ({ label: r.key, value: r.value, mono: r.mono })),
  ];

  return (
    <Stack gap="comfortable">
      {error && <Alert variant="error">{error}</Alert>}

      <Wizard.ReviewStep>
        <Wizard.ReviewCard
          icon={definition.icon}
          title={configStep?.railTitle ?? definition.title}
          onEdit={configStep && onGoTo ? () => onGoTo(configStep.id) : undefined}
          rows={configRows}
        >
          {/* The prompt is the step's one multi-line value; a chip would
              truncate it, so it gets its own block under the rows. */}
          <Stack gap="tight" paddingBlock="snug">
            <Text as="span" size="caption" color="subtle">
              Prompt
            </Text>
            <Text size="caption" mono color="muted" preserveWhitespace>
              {state.promptTemplate || '—'}
            </Text>
          </Stack>
        </Wizard.ReviewCard>

        <Wizard.ReviewCard
          icon="route"
          title="Target"
          onEdit={onGoTo ? () => onGoTo('target') : undefined}
          rows={[{ label: 'Agent', value: agent?.name?.trim() || agent?.slug || '—' }]}
        />

        <Wizard.ReviewCard
          icon="shield-check"
          title="Delivery"
          onEdit={onGoTo ? () => onGoTo('delivery') : undefined}
          rows={[
            {
              label: 'Overlapping runs',
              value: `${parallelWord(state.maxParallel)}, then ${state.overflow}`,
            },
            { label: 'Timeout', value: `${state.timeoutSeconds}s`, mono: true },
            ...coalesceRows,
            { label: 'Suppress duplicates', value: state.dedupeEnabled ? 'On' : 'Off' },
            { label: 'Auto-pause on failure', value: state.autopauseEnabled ? 'On' : 'Off' },
          ]}
        />
      </Wizard.ReviewStep>
    </Stack>
  );
}
