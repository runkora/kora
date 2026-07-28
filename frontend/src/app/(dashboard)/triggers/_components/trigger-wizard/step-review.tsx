'use client';

/** @tier 3 — triggers-scoped. Shared Review step: the configuration grouped by
 * the step that owns it, each group jumping back to its step for edits. */

import { Alert, Button, Card, CardContent, CardHeader, Separator, Stack, Text } from '@orbit/terra';
import type { IconName } from '@orbit/terra';
import { parallelWord } from './wizard-state';
import type { ReviewRow, StepComponentProps } from './types';

/**
 * One review group: the rows a single wizard step produced, in the agent
 * builder's review motif, with an Edit action jumping back to that step.
 */
function ReviewGroup({
  icon,
  title,
  rows,
  onEdit,
  children,
}: {
  icon: IconName;
  title: string;
  rows: ReviewRow[];
  onEdit?: () => void;
  children?: React.ReactNode;
}) {
  return (
    <Card>
      <CardHeader
        compact
        divider
        leadingIcon={icon}
        title={title}
        action={
          onEdit && (
            <Button variant="ghost" size="xs" onClick={onEdit}>
              Edit
            </Button>
          )
        }
      />
      <CardContent padded compact>
        <Stack gap="none">
          {rows.map((row, i) => (
            <Stack key={row.key} gap="none">
              {i > 0 && <Separator span="full" />}
              <Stack direction="horizontal" align="center" gap="comfortable" paddingBlock="snug">
                <Text as="span" size="caption" color="subtle">
                  {row.key}
                </Text>
                <Stack grow />
                <Text as="span" size="caption" mono={row.mono ?? false} align="end">
                  {row.value}
                </Text>
              </Stack>
            </Stack>
          ))}
          {children}
        </Stack>
      </CardContent>
    </Card>
  );
}

export function StepReview({ state, context, definition, error, onGoTo }: StepComponentProps) {
  const agent = context.agents.find((a) => a.id === state.agentTypeId);
  const configStep = definition.steps[0];

  return (
    <Stack gap="comfortable">
      {error && <Alert variant="error">{error}</Alert>}

      <Stack gap="cozy">
        <ReviewGroup
          icon={definition.icon}
          title={configStep?.railTitle ?? definition.title}
          onEdit={configStep && onGoTo ? () => onGoTo(configStep.id) : undefined}
          rows={[
            { key: 'Name', value: state.name || '—', mono: true },
            { key: 'Kind', value: definition.title },
            ...definition.summarize(state, context),
          ]}
        >
          {/* The prompt is the step's one multi-line value; a chip would
              truncate it, so it gets its own block under the rows. */}
          <Separator span="full" />
          <Stack gap="tight" paddingBlock="snug">
            <Text as="span" size="caption" color="subtle">
              Prompt
            </Text>
            <Text size="caption" mono color="muted" preserveWhitespace>
              {state.promptTemplate || '—'}
            </Text>
          </Stack>
        </ReviewGroup>

        <ReviewGroup
          icon="route"
          title="Target"
          onEdit={onGoTo ? () => onGoTo('target') : undefined}
          rows={[
            { key: 'Agent', value: agent?.name?.trim() || agent?.slug || '—' },
            {
              key: 'Overlapping runs',
              value: `${parallelWord(state.maxParallel)}, then ${state.overflow}`,
            },
          ]}
        />

        <ReviewGroup
          icon="shield-check"
          title="Delivery"
          onEdit={onGoTo ? () => onGoTo('delivery') : undefined}
          rows={[
            { key: 'Timeout', value: `${state.timeoutSeconds}s`, mono: true },
            { key: 'Suppress duplicates', value: state.dedupeEnabled ? 'On' : 'Off' },
            { key: 'Auto-pause on failure', value: state.autopauseEnabled ? 'On' : 'Off' },
          ]}
        />
      </Stack>
    </Stack>
  );
}
