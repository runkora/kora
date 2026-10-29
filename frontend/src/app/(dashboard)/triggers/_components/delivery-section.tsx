'use client';

/**
 * @tier 3 — triggers-scoped. The Delivery section: everything between a delivery
 * arriving and its run starting. Used by the New trigger wizard's Delivery step
 * and by a stored trigger's Configuration tab.
 *
 * Capacity, overlap and the start timeout are three bounds on one waiting room,
 * so they are configured together: the parallel ceiling says how many may run,
 * the overflow choice says what happens to the rest, and the timeout says how
 * long any of them may wait. Split across two sections, the third bound read as
 * an unrelated setting. A summary sentence checks the three against each other,
 * and the safety switches follow.
 *
 * The caller owns the value and what a change does, and the section's title and
 * intro (the modal's heading, the wizard's step header). Coalescing needs `wait`,
 * so it is shown only under that overflow and ignored under any other.
 */

import { useId, useState } from 'react';
import type { ReactNode } from 'react';
import {
  Button,
  Callout,
  Card,
  CardContent,
  Grid,
  Icon,
  Input,
  RadioGroup,
  SectionLabel,
  Stack,
  Text,
} from '@runkora/hades';
import { LIMITS } from '../_types';
import { CoalesceEditor, coalesceFieldErrors } from './coalesce-editor';
import { DeliveryField, DeliveryRail } from './delivery-field';
import { OverflowOptionCard } from './overflow-option-card';
import { PlainToggleRow } from './plain-toggle-row';
import type { CoalesceDraft } from './coalesce-editor';
import { admissionSummary } from './trigger-wizard/wizard-state';
import type { TriggerKind, TriggerOverflow } from './trigger-wizard/types';

/** Every setting of the Delivery section, as one draft. A count field the user has
 * emptied is `NaN`: it is neither a valid number nor a silent 0, and reads as "required". */
export interface DeliveryValue {
  maxParallel: number;
  overflow: TriggerOverflow;
  maxQueueDepth: number;
  timeoutSeconds: number;
  dedupeEnabled: boolean;
  autopauseEnabled: boolean;
  coalesce: CoalesceDraft;
}

/** What is wrong with each bounded field, worded for the line under it. Same bounds as the API. */
export interface DeliveryErrors {
  queue?: string;
  key?: string;
  window?: string;
  timeout?: string;
}

export function deliveryErrors(value: DeliveryValue): DeliveryErrors {
  const { queueDepth, timeout } = LIMITS;
  const coalesce = value.overflow === 'wait' ? coalesceFieldErrors(value.coalesce) : {};
  const outside = (n: number, min: number, max: number) => !(n >= min && n <= max);
  return {
    ...(value.overflow === 'wait' &&
      outside(value.maxQueueDepth, queueDepth.min, queueDepth.max) && {
        queue: Number.isNaN(value.maxQueueDepth)
          ? 'Queue size is required.'
          : `Queue size must be between ${queueDepth.min} and ${queueDepth.max}.`,
      }),
    ...coalesce,
    ...(outside(value.timeoutSeconds, timeout.min, timeout.max) && {
      timeout: Number.isNaN(value.timeoutSeconds)
        ? 'Time to start is required.'
        : `Time to start must be between ${timeout.min} and ${timeout.max} seconds.`,
    }),
  };
}

/** Why this value cannot be saved yet, if anything: how many fields need fixing. */
export function deliveryBlocker(value: DeliveryValue): string | undefined {
  const count = Object.keys(deliveryErrors(value)).length;
  if (count === 0) return undefined;
  return `Fix ${count} ${count === 1 ? 'value' : 'values'} to save.`;
}

/** Whether two drafts are the same setting, for a section that saves on demand. */
export function sameDelivery(a: DeliveryValue, b: DeliveryValue): boolean {
  return (
    a.maxParallel === b.maxParallel &&
    a.overflow === b.overflow &&
    Object.is(a.maxQueueDepth, b.maxQueueDepth) &&
    Object.is(a.timeoutSeconds, b.timeoutSeconds) &&
    a.dedupeEnabled === b.dedupeEnabled &&
    a.autopauseEnabled === b.autopauseEnabled &&
    a.coalesce.enabled === b.coalesce.enabled &&
    (!a.coalesce.enabled ||
      (a.coalesce.key.trim() === b.coalesce.key.trim() &&
        a.coalesce.window.trim() === b.coalesce.window.trim()))
  );
}

interface OverflowOption {
  id: TriggerOverflow;
  label: string;
  description: string;
  /** The kinds this behaviour suits, called out on the card. */
  best: TriggerKind[];
}

const OVERFLOW_OPTIONS: OverflowOption[] = [
  {
    id: 'wait',
    label: 'Wait',
    description: 'Queue them and run them in order as runs finish.',
    best: ['webhook', 'manual'],
  },
  {
    id: 'replace',
    label: 'Replace',
    description: 'Keep only the newest waiting event. Older ones are skipped.',
    best: ['scheduled'],
  },
  {
    id: 'refuse',
    label: 'Refuse',
    // Deliberately not "with 429": only a manual fire has a caller to answer,
    // so the summary below states the outcome for this kind.
    description: "Turn them away. They're recorded as dropped.",
    best: [],
  },
];

/** Why the recommended behaviour suits this kind, under the cards. */
function RecommendationNote({ kind }: { kind: TriggerKind }) {
  if (kind === 'scheduled')
    return (
      <Text size="caption" color="muted">
        <Text as="span" size="caption" weight="heavy">
          Replace
        </Text>{' '}
        is recommended for scheduled triggers: if a run is still going, only the latest tick needs
        to run next.
      </Text>
    );
  return (
    <Text size="caption" color="muted">
      <Text as="span" size="caption" weight="heavy">
        Wait
      </Text>{' '}
      {kind === 'manual'
        ? 'is recommended for manual triggers, so a fire that arrives while runs are busy is queued instead of lost.'
        : 'is recommended for webhook triggers, so a burst of events is still handled in full.'}
    </Text>
  );
}

/**
 * A whole-number field that holds what the user typed. The number is `NaN` while the
 * text is empty, so the box can be cleared to type a new value; digits only, because a
 * decimal or exponent typed into a count would be sent as a non-integer. The text
 * re-syncs whenever the external value stops matching it (discard, save, another trigger).
 */
function useWholeNumberText(value: number, commit: (next: number) => void) {
  const format = (n: number) => (Number.isNaN(n) ? '' : String(n));
  const [text, setText] = useState(format(value));
  const parse = (t: string) => (t === '' ? Number.NaN : Number(t));
  const shown = Object.is(parse(text), value) ? text : format(value);
  return {
    text: shown,
    onChange: (raw: string) => {
      const digits = raw.replace(/\D/g, '');
      setText(digits);
      commit(parse(digits));
    },
  };
}

interface DeliverySectionProps {
  value: DeliveryValue;
  onChange: (next: Partial<DeliveryValue>) => void;
  kind: TriggerKind;
  /** An example coalescing key in the trigger's own fields. */
  coalesceKeyExample?: string;
  disabled?: boolean;
  /** Rendered under the section, e.g. a stored trigger's save and discard buttons. */
  actions?: ReactNode;
}

export function DeliverySection({
  value,
  onChange,
  kind,
  coalesceKeyExample,
  disabled = false,
  actions,
}: DeliverySectionProps) {
  const queueId = useId();
  const timeoutId = useId();
  const errors = deliveryErrors(value);
  const queueText = useWholeNumberText(value.maxQueueDepth, (n) => onChange({ maxQueueDepth: n }));
  const timeoutText = useWholeNumberText(value.timeoutSeconds, (n) =>
    onChange({ timeoutSeconds: n }),
  );
  const invalid = Object.keys(errors).length > 0;
  const { parallel } = LIMITS;
  return (
    <Stack gap="relaxed">
      <Stack gap="relaxed">
        <Stack gap="comfortable">
          <SectionLabel>Capacity</SectionLabel>

          <Stack direction="horizontal" align="center" gap="comfortable">
            <Stack gap="tight" grow minWidth="none">
              <Text as="span" size="supporting" weight="heavy">
                Max parallel runs
              </Text>
              <Text as="span" size="caption" color="muted">
                Maximum concurrency is {parallel.max}.
              </Text>
            </Stack>
            <Stack
              direction="horizontal"
              align="center"
              gap="comfortable"
              shrink={false}
              disabled={disabled}
            >
              <Button
                size="icon-sm"
                variant="secondary"
                aria-label="Fewer parallel runs"
                disabled={disabled || value.maxParallel <= parallel.min}
                onClick={() =>
                  onChange({ maxParallel: Math.max(parallel.min, value.maxParallel - 1) })
                }
              >
                <Icon name="minus" size="xs" />
              </Button>
              <Text as="span" size="supporting" mono weight="heavy" tabular>
                {value.maxParallel}
              </Text>
              <Button
                size="icon-sm"
                variant="secondary"
                aria-label="More parallel runs"
                disabled={disabled || value.maxParallel >= parallel.max}
                onClick={() =>
                  onChange({ maxParallel: Math.min(parallel.max, value.maxParallel + 1) })
                }
              >
                <Icon name="plus" size="xs" />
              </Button>
            </Stack>
          </Stack>
        </Stack>

        <Stack gap="cozy">
          <Stack gap="tight">
            <Text as="span" size="supporting" weight="heavy">
              When {value.maxParallel} {value.maxParallel === 1 ? 'is' : 'are'} already in progress
            </Text>
            <Text as="span" size="caption" color="muted">
              What happens to new events once that limit is reached.
            </Text>
          </Stack>
          <RadioGroup
            value={value.overflow}
            onValueChange={(v) => onChange({ overflow: v as TriggerOverflow })}
            aria-label="Overflow behaviour"
          >
            <Stack disabled={disabled}>
              <Grid cols={{ base: 1, md: 3 }} gap="cozy">
                {OVERFLOW_OPTIONS.map((o) => (
                  <OverflowOptionCard
                    key={o.id}
                    value={o.id}
                    selected={value.overflow === o.id}
                    disabled={disabled}
                    onSelect={(v) => onChange({ overflow: v as TriggerOverflow })}
                    title={o.label}
                    description={o.description}
                    recommended={o.best.includes(kind)}
                  />
                ))}
              </Grid>
            </Stack>
          </RadioGroup>
          {kind === 'webhook' && value.overflow !== 'wait' && value.coalesce.enabled && (
            <Callout variant="warning">
              <Text size="caption">
                Coalescing only works with Wait. Saving with{' '}
                {OVERFLOW_OPTIONS.find((o) => o.id === value.overflow)?.label ?? value.overflow}{' '}
                turns it off.
              </Text>
            </Callout>
          )}
          <RecommendationNote kind={kind} />
        </Stack>

        {value.overflow === 'wait' && (
          <DeliveryRail>
            <DeliveryField
              id={queueId}
              label="Queue size"
              hint="Once this many are waiting, the oldest is dropped to make room."
              error={errors.queue}
            >
              <Stack maxWidth="form-field">
                <Input
                  id={queueId}
                  mono
                  disabled={disabled}
                  inputMode="numeric"
                  value={queueText.text}
                  filled={errors.queue ? 'danger' : undefined}
                  aria-invalid={errors.queue ? true : undefined}
                  aria-describedby={errors.queue ? `${queueId}-error` : undefined}
                  onChange={(e) => queueText.onChange(e.target.value)}
                  endAdornment={
                    <Text as="span" size="caption" color="subtle">
                      waiting
                    </Text>
                  }
                />
              </Stack>
            </DeliveryField>
            {/* Coalescing is a webhook feature and needs `wait`; elsewhere it is hidden. */}
            {kind === 'webhook' && (
              <CoalesceEditor
                value={value.coalesce}
                keyPlaceholder={coalesceKeyExample}
                disabled={disabled}
                onChange={(coalesce) => onChange({ coalesce })}
              />
            )}
          </DeliveryRail>
        )}

        <DeliveryField
          id={timeoutId}
          label="Time to start"
          hint="How long an admitted event has to start its run before it's abandoned."
          error={errors.timeout}
        >
          <Stack maxWidth="form-field">
            <Input
              id={timeoutId}
              mono
              disabled={disabled}
              inputMode="numeric"
              value={timeoutText.text}
              filled={errors.timeout ? 'danger' : undefined}
              aria-invalid={errors.timeout ? true : undefined}
              aria-describedby={errors.timeout ? `${timeoutId}-error` : undefined}
              onChange={(e) => timeoutText.onChange(e.target.value)}
              endAdornment={
                <Text as="span" size="caption" color="subtle">
                  seconds
                </Text>
              }
            />
          </Stack>
        </DeliveryField>
      </Stack>

      {/* All three bounds in one sentence, which is the only place they can be
          checked against each other. */}
      <Stack gap="comfortable">
        <SectionLabel>Summary</SectionLabel>
        <Card subtle border="none">
          <CardContent padded compact>
            <Stack direction="horizontal" gap="cozy" align="start">
              <Icon name="menu" size="sm" color="muted" label="" shrink />
              <Text size="supporting">
                {invalid
                  ? 'Fix the errors above to see what these settings do.'
                  : admissionSummary(value, kind).map((seg, i) =>
                      seg.strong ? (
                        <Text key={i} as="span" size="supporting" weight="heavy">
                          {seg.text}
                        </Text>
                      ) : (
                        seg.text
                      ),
                    )}
              </Text>
            </Stack>
          </CardContent>
        </Card>
      </Stack>

      <Stack gap="comfortable">
        <SectionLabel>Safety</SectionLabel>
        <Stack gap="cozy">
          <PlainToggleRow
            label="Suppress duplicates"
            description="Ignore an event that replays one already processed."
            checked={value.dedupeEnabled}
            disabled={disabled}
            onChange={(next) => onChange({ dedupeEnabled: next })}
          />
          <PlainToggleRow
            label="Auto-pause on repeated failure"
            description="Turn the trigger off after 10 failed deliveries in a row."
            checked={value.autopauseEnabled}
            disabled={disabled}
            onChange={(next) => onChange({ autopauseEnabled: next })}
          />
        </Stack>
      </Stack>
      {actions}
    </Stack>
  );
}
