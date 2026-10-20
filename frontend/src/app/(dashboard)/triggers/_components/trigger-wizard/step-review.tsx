'use client';

/** @tier 3 — triggers-scoped. Shared Review step: the configuration grouped by
 * the step that owns it, each group jumping back to its step for edits. */

import { Alert, Stack, Text, Wizard } from '@runkora/hades';
import { coalesceDraftOf, coalesceFrom, isRouted, parallelWord } from './wizard-state';
import type { RoutingDraft, RoutingOptions, StepComponentProps, WizardState } from './types';

const CONFIDENCE_LABEL: Record<RoutingDraft['confidence'], string> = {
  strict: 'Strict',
  balanced: 'Balanced',
  loose: 'Loose',
};

/** The Target card's rows for a routed trigger: who kora chooses between, and how. */
function routingRows(routing: RoutingDraft, options: RoutingOptions) {
  const agents = options.candidates.status === 'ready' ? options.candidates.items : [];
  const name = (id: string) => {
    const a = agents.find((x) => x.id === id);
    return a?.name?.trim() || a?.slug || id;
  };
  return [
    { label: 'Candidates', value: routing.candidateIds.map(name).join(', ') },
    { label: 'Decision model', value: routing.modelId, mono: true },
    { label: 'Confidence', value: CONFIDENCE_LABEL[routing.confidence] },
    {
      label: 'When unsure',
      value: routing.unsure === 'fallback' ? `Send to ${name(routing.fallbackId)}` : 'No run',
    },
    {
      label: 'Join open runs',
      value: routing.joinOpenRuns ? routing.joinKey.trim() || 'On' : 'Off',
      mono: routing.joinOpenRuns && routing.joinKey.trim() !== '',
    },
  ];
}

function targetRows(state: WizardState, context: StepComponentProps['context']) {
  if (context.routing !== undefined && isRouted(state, context))
    return routingRows(state.routing, context.routing);
  const agent = context.agents.find((a) => a.id === state.agentTypeId);
  return [{ label: 'Agent', value: agent?.name?.trim() || agent?.slug || '—' }];
}

export function StepReview({ state, context, definition, error, onGoTo }: StepComponentProps) {
  const configStep = definition.steps[0];

  const coalesce = coalesceFrom(coalesceDraftOf(state));
  const coalesceRows =
    definition.apiKind === 'webhook' && state.overflow === 'wait'
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
          rows={targetRows(state, context)}
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
