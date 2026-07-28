'use client';

/** @tier 3 — triggers-scoped. Shared Delivery step: the dispatch timeout and the
 * two safety behaviours that keep a broken source from flooding the platform. */

import { FormField, Input, Stack, Text, ToggleRow } from '@orbit/terra';
import { FIELD_IDS } from './wizard-state';
import type { StepComponentProps } from './types';

export function StepDelivery({ state, patch, fieldError, firstFieldRef }: StepComponentProps) {
  return (
    <Stack gap="relaxed">
      <Stack maxWidth="search-field">
        <FormField
          label="Timeout"
          id={FIELD_IDS.timeout}
          hint="Bounds starting the run, not running it. Orbit gives up after this window."
          error={fieldError(FIELD_IDS.timeout)}
        >
          <Input
            id={FIELD_IDS.timeout}
            ref={firstFieldRef}
            mono
            inputMode="numeric"
            value={String(state.timeoutSeconds)}
            onChange={(e) => patch({ timeoutSeconds: Number(e.target.value) || 0 })}
            endAdornment={
              <Text as="span" size="caption" color="subtle">
                seconds
              </Text>
            }
          />
        </FormField>
      </Stack>

      <Stack gap="cozy">
        <ToggleRow
          label="Suppress duplicates"
          description="Ignore a replay of a delivery already processed."
          checked={state.dedupeEnabled}
          onChange={(next) => patch({ dedupeEnabled: next })}
        />
        <ToggleRow
          label="Auto-pause on failure"
          description="Disable the trigger after ten consecutive failed deliveries, so a broken target stops burning runs."
          checked={state.autopauseEnabled}
          onChange={(next) => patch({ autopauseEnabled: next })}
        />
      </Stack>
    </Stack>
  );
}
