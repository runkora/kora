'use client';

/** @tier 3 — triggers-scoped. Shared Review step: the kind summarises itself,
 * the shared rows follow, and a failed create surfaces here without loss. */

import { Alert, Card, CardContent, SettingRow, Stack, Text } from '@orbit/terra';
import { sharedReviewRows } from './wizard-state';
import type { StepComponentProps } from './types';

export function StepReview({ state, context, definition, error }: StepComponentProps) {
  const rows = [
    { key: 'Name', value: state.name || '—', mono: true },
    { key: 'Kind', value: definition.title },
    ...definition.summarize(state, context),
    ...sharedReviewRows(state, context),
  ];

  return (
    <Stack gap="comfortable">
      {error && <Alert variant="error">{error}</Alert>}

      <Card inset>
        <CardContent padded compact>
          <Stack>
            {rows.map((r) => (
              <SettingRow
                key={r.key}
                label={r.key}
                value={
                  <Text as="span" size="caption" mono={r.mono} weight="emphasis">
                    {r.value}
                  </Text>
                }
              />
            ))}
          </Stack>
        </CardContent>
      </Card>

      <Stack gap="tight">
        <Text size="caption" color="subtle">
          Prompt
        </Text>
        <Text size="caption" mono color="muted" preserveWhitespace>
          {state.promptTemplate}
        </Text>
      </Stack>
    </Stack>
  );
}
