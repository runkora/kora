'use client';

/**
 * @tier 3 — triggers-scoped. Shared Delivery step: everything between a
 * delivery arriving and its run starting.
 *
 * Capacity, overlap and the start timeout are three bounds on one waiting room,
 * so they are configured together: the parallel ceiling says how many may run,
 * the overflow choice says what happens to the rest, and the timeout says how
 * long any of them may wait. Split across two steps, the third bound read as an
 * unrelated setting.
 */

import {
  Button,
  Callout,
  Card,
  CardContent,
  FormField,
  Grid,
  Icon,
  Input,
  RadioCard,
  RadioGroup,
  SectionLabel,
  Stack,
  Text,
  ToggleRow,
} from '@orbit/hades';
import type { IconName } from '@orbit/hades';
import { admissionSummary, parallelWord } from './wizard-state';
import { LIMITS } from '../../_types';
import { CoalesceEditor } from '../coalesce-editor';
import type { StepComponentProps, TriggerKind, TriggerOverflow } from './types';

interface OverflowOption {
  id: TriggerOverflow;
  label: string;
  icon: IconName;
  description: string;
  /** The kinds this behaviour suits, called out in the card. */
  best: TriggerKind[];
}

const OVERFLOW_OPTIONS: OverflowOption[] = [
  {
    id: 'wait',
    label: 'Wait',
    icon: 'clock',
    description: 'Hold deliveries in order and run them as capacity frees.',
    best: ['webhook', 'manual'],
  },
  {
    id: 'replace',
    label: 'Replace',
    icon: 'layers',
    description: 'Keep only the newest waiting delivery; older ones are superseded.',
    best: ['scheduled'],
  },
  {
    id: 'refuse',
    label: 'Refuse',
    icon: 'x-circle',
    // Deliberately not "with 429": only a manual fire has a caller to answer,
    // so the summary below the cards states the outcome for this kind.
    description: 'Turn the delivery away instead of queueing it.',
    best: [],
  },
];

export function StepDelivery({ state, patch, definition }: StepComponentProps) {
  return (
    <Stack gap="relaxed">
      <Stack gap="snug">
        <SectionLabel>Overlapping runs</SectionLabel>
        <Text size="caption" color="subtle">
          A trigger can fire faster than the agent finishes. This decides what happens to the
          overlap; the limits apply per trigger.
        </Text>
        <Card inset>
          <CardContent padded compact>
            <Stack direction="horizontal" align="center" gap="comfortable">
              <Stack gap="tight" grow minWidth="none">
                <Text as="span" size="supporting" weight="heavy">
                  Max parallel runs
                </Text>
                <Text as="span" size="caption" color="muted">
                  {parallelWord(state.maxParallel)}
                </Text>
              </Stack>
              <Stack direction="horizontal" align="center" gap="snug" shrink={false}>
                <Button
                  size="icon-sm"
                  variant="secondary"
                  aria-label="Fewer parallel runs"
                  disabled={state.maxParallel <= LIMITS.parallel.min}
                  onClick={() =>
                    patch({ maxParallel: Math.max(LIMITS.parallel.min, state.maxParallel - 1) })
                  }
                >
                  <Icon name="minus" size="xs" />
                </Button>
                <Text as="span" size="supporting" mono weight="heavy" tabular>
                  {state.maxParallel}
                </Text>
                <Button
                  size="icon-sm"
                  variant="secondary"
                  aria-label="More parallel runs"
                  disabled={state.maxParallel >= LIMITS.parallel.max}
                  onClick={() =>
                    patch({ maxParallel: Math.min(LIMITS.parallel.max, state.maxParallel + 1) })
                  }
                >
                  <Icon name="plus" size="xs" />
                </Button>
              </Stack>
            </Stack>
          </CardContent>
        </Card>
      </Stack>

      <Stack gap="snug">
        <SectionLabel>When that limit is reached</SectionLabel>
        <RadioGroup
          value={state.overflow}
          onValueChange={(v) => patch({ overflow: v as TriggerOverflow })}
          aria-label="Overflow behaviour"
        >
          <Grid cols={3} gap="cozy">
            {OVERFLOW_OPTIONS.map((o) => (
              <RadioCard
                key={o.id}
                value={o.id}
                selected={state.overflow === o.id}
                onSelect={(v) => patch({ overflow: v as TriggerOverflow })}
                icon={o.icon}
                title={o.label}
                description={
                  o.best.includes(definition.id)
                    ? `${o.description} Recommended here.`
                    : o.description
                }
              />
            ))}
          </Grid>
        </RadioGroup>
      </Stack>

      {/* The two numeric bounds share a row: stacked they read as two topics,
          side by side they read as what they are, twin limits on one queue. */}
      <Grid cols={2} gap="comfortable">
        {state.overflow === 'wait' && (
          <FormField
            label="Max queued deliveries"
            id="wizard-queue-depth"
            hint="Beyond this the oldest waiting delivery is dropped."
          >
            <Input
              id="wizard-queue-depth"
              mono
              inputMode="numeric"
              value={String(state.maxQueueDepth)}
              onChange={(e) => patch({ maxQueueDepth: Number(e.target.value) || 0 })}
              endAdornment={
                <Text as="span" size="caption" color="subtle">
                  waiting
                </Text>
              }
            />
          </FormField>
        )}
        <FormField
          label="Timeout"
          id="wizard-timeout"
          hint="Bounds the start request, which returns once the run is queued. Dispatch is abandoned after this window."
        >
          <Input
            id="wizard-timeout"
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
      </Grid>

      {/* All three bounds in one sentence, which is the only place they can be
          checked against each other. */}
      <Callout variant="info">
        <Stack direction="horizontal" gap="cozy" align="center">
          <Icon name="route" size="sm" color="accent" label="" shrink />
          <Text size="caption" color="muted">
            {admissionSummary(state, definition.id)}
          </Text>
        </Stack>
      </Callout>

      {/* Coalescing is a webhook feature and needs `wait`; elsewhere it is hidden. */}
      {definition.id === 'webhook' && state.overflow === 'wait' && (
        <CoalesceEditor
          value={{ key: state.coalesceKey, window: state.coalesceWindow }}
          onChange={(next) => patch({ coalesceKey: next.key, coalesceWindow: next.window })}
        />
      )}

      <Stack gap="cozy">
        <ToggleRow
          label="Suppress duplicates"
          description="Ignore a replay of a delivery already processed."
          checked={state.dedupeEnabled}
          onChange={(next) => patch({ dedupeEnabled: next })}
        />
        <ToggleRow
          label="Auto-pause on repeated failure"
          description="Disable the trigger after ten consecutive failed deliveries, so a broken target stops burning runs."
          checked={state.autopauseEnabled}
          onChange={(next) => patch({ autopauseEnabled: next })}
        />
      </Stack>
    </Stack>
  );
}
