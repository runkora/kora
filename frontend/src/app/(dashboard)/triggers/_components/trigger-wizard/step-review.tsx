'use client';

/** @tier 3 — triggers-scoped. Shared Review step: the kind summarises itself,
 * the shared rows follow, and a failed create surfaces here without loss. */

import { Alert, Separator, SettingRow, Stack, Text } from '@orbit/terra';
import { sharedReviewRows } from './wizard-state';
import type { ReviewRow, StepComponentProps } from './types';

export function StepReview({ state, context, definition, error }: StepComponentProps) {
  const rows: ReviewRow[] = [
    {
      key: 'Trigger name',
      value: state.name || '—',
      mono: true,
      description: 'Shown in the triggers list and on every run it starts.',
    },
    { key: 'Kind', value: definition.title, description: definition.description },
    ...definition.summarize(state, context),
    ...sharedReviewRows(state, context),
  ];

  return (
    <Stack gap="comfortable">
      {error && <Alert variant="error">{error}</Alert>}

      <Stack>
        {rows.map((r, i) => (
          <Stack key={r.key} gap="none">
            {i > 0 && <Separator span="full" />}
            <SettingRow
              label={r.key}
              description={r.description}
              value={
                <Text as="span" size="caption" mono={r.mono} weight="emphasis">
                  {r.value}
                </Text>
              }
            />
          </Stack>
        ))}
      </Stack>

      <Stack gap="tight">
        <Text size="caption" color="subtle">
          Prompt
        </Text>
        <Text size="caption" mono color="muted" preserveWhitespace>
          {state.promptTemplate}
        </Text>
      </Stack>

      <Text size="caption" color="subtle">
        Nothing fires until you create it.
      </Text>
    </Stack>
  );
}
