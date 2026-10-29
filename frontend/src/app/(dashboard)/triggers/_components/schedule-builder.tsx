'use client';

/**
 * @tier 3 — triggers-scoped. The schedule builder: how often a trigger fires,
 * the cron expression that derives, its timezone and catch-up.
 *
 * One component for both surfaces. The New trigger wizard feeds it its own
 * state; a stored trigger's Configuration tab feeds it the stored schedule
 * through `useScheduleDraft`. Whatever changes here changes on both.
 */

import { useId, useState } from 'react';
import {
  Button,
  Callout,
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
  FormField,
  Grid,
  Icon,
  Input,
  RadioCard,
  RadioGroup,
  SectionLabel,
  SegmentedControl,
  Stack,
  Text,
  ToggleRow,
} from '@runkora/hades';
import {
  FREQS,
  INTERVAL_PRESETS,
  MINUTE_PRESETS,
  WEEKDAYS,
  cronFor,
  humanFor,
  isValidCron,
  resolveBrowserZone,
  specFromCron,
  timezoneOptions,
  toggleWeekday,
} from './trigger-wizard/cron';
import type { CronSpec, Frequency } from './trigger-wizard/cron';
import { useSchedulePreview } from './trigger-wizard/use-schedule-preview';

/** Everything the builder edits. The wizard's state carries these fields, so it passes as is. */
export interface ScheduleDraft extends CronSpec {
  /** The raw cron expression is open under "Advanced". */
  advancedOpen: boolean;
  timezone: string;
  catchUp: boolean;
}

/** What a stored trigger keeps of a schedule. */
export interface StoredSchedule {
  cronExpression: string;
  timezone: string;
  catchUp: boolean;
}

export function storedFromDraft(draft: ScheduleDraft): StoredSchedule {
  return {
    cronExpression: cronFor(draft),
    timezone: draft.timezone,
    catchUp: draft.catchUp,
  };
}

/**
 * The builder's view of a stored schedule. An expression the builder cannot
 * produce (a range, a month restriction) opens as `custom`, on its raw text.
 */
export function draftFromStored(stored: StoredSchedule): ScheduleDraft {
  const spec = specFromCron(stored.cronExpression);
  return {
    freq: 'custom',
    interval: '15',
    atMinute: '0',
    time: '02:00',
    weekdays: [1],
    dom: '1',
    ...spec,
    customCron: stored.cronExpression,
    advancedOpen: false,
    timezone: stored.timezone,
    catchUp: stored.catchUp,
  };
}

/**
 * Holds the builder's draft over a stored schedule. The draft is kept apart
 * from the expression because a half-typed field ("" in the interval box) has
 * no expression of its own; when the stored value changes from outside, after
 * a discard or a save, the draft is rebuilt from it.
 */
export function useScheduleDraft(
  value: StoredSchedule,
  setValue: (next: StoredSchedule) => void,
): [ScheduleDraft, (patch: Partial<ScheduleDraft>) => void] {
  const [draft, setDraft] = useState(() => draftFromStored(value));
  // Compared against the stored value as the builder would write it, not as it
  // arrived: a cron the builder does not spell the same way (`*/05`, `0 9 * * 5,1`)
  // would never match its own draft, and re-seeding on every render never settles.
  const own = storedFromDraft(draft);
  const settled = storedFromDraft(draftFromStored(value));
  const inSync =
    own.cronExpression === settled.cronExpression &&
    own.timezone === settled.timezone &&
    own.catchUp === settled.catchUp;
  if (!inSync) {
    const next = draftFromStored(value);
    setDraft(next);
    return [next, (patch) => update(next, patch)];
  }
  return [draft, (patch) => update(draft, patch)];

  function update(from: ScheduleDraft, patch: Partial<ScheduleDraft>) {
    const next = { ...from, ...patch };
    setDraft(next);
    setValue(storedFromDraft(next));
  }
}

/** Cities are what a person recognises; the IANA name is what the platform stores. */
const ZONE_CITY: Record<string, string> = {
  UTC: 'Coordinated Universal Time',
  'Europe/London': 'London · GMT/BST',
  'Australia/Sydney': 'Sydney · AEST/AEDT',
};

interface ScheduleBuilderProps {
  value: ScheduleDraft;
  onChange: (patch: Partial<ScheduleDraft>) => void;
  /** Zones offered. Defaults to the platform's three plus the browser's own. */
  zones?: string[];
  /** Shows the schedule as read-only text, for a caller who may not change it. */
  disabled?: boolean;
}

export function ScheduleBuilder({
  value: state,
  onChange: patch,
  zones,
  disabled = false,
}: ScheduleBuilderProps) {
  const id = useId();
  const browserZone = resolveBrowserZone();
  const zoneOptions = zones ?? timezoneOptions(browserZone);
  const derivedCron = cronFor(state);
  const human = humanFor(state);
  // The exact times come from the scheduler, not from this builder: two cron
  // interpretations that could disagree is exactly the bug worth avoiding.
  const preview = useSchedulePreview(derivedCron, state.timezone);
  const showTime = state.freq === 'daily' || state.freq === 'weekly' || state.freq === 'monthly';

  const summary = (
    <Callout variant="info">
      <Stack direction="horizontal" gap="cozy" align="start">
        <Icon name="clock" size="sm" color="accent" label="" shrink />
        <Stack gap="tight" minWidth="none">
          {/* The scheduler's own answer leads when there is one. The builder's
              sentence describes what was configured; only the platform can say
              when it will actually fire, so it never gets the headline. */}
          {preview.fires.length > 0 ? (
            <>
              <Text size="supporting" weight="heavy">
                Next: {preview.fires[0]?.label}
              </Text>
              <Text size="caption" color="muted">
                {human} · {state.timezone}
              </Text>
            </>
          ) : (
            <>
              <Text size="supporting" weight="heavy">
                {human}
              </Text>
              <Text size="caption" color="muted">
                {state.timezone}
              </Text>
            </>
          )}

          {preview.fires.length > 1 && (
            <Stack gap="none" paddingTop="tight">
              {preview.fires.slice(1, 4).map((fire) => (
                <Text key={fire.at} size="micro" color="subtle">
                  then {fire.label}
                </Text>
              ))}
            </Stack>
          )}
        </Stack>
      </Stack>
    </Callout>
  );

  if (disabled) {
    return (
      <Stack gap="relaxed">
        {summary}
        <Stack gap="tight">
          <SectionLabel>Cron expression</SectionLabel>
          <Text mono size="supporting">
            {derivedCron}
          </Text>
        </Stack>
        <Stack gap="tight">
          <SectionLabel>Catch-up</SectionLabel>
          <Text size="supporting">{state.catchUp ? 'Run on recovery' : 'Skip missed runs'}</Text>
        </Stack>
      </Stack>
    );
  }

  return (
    <Stack gap="relaxed">
      <Stack gap="snug">
        <SectionLabel>How often</SectionLabel>
        <SegmentedControl
          aria-label="Frequency"
          value={state.freq}
          onChange={(v) => patch({ freq: v as Frequency })}
          options={FREQS.map((f) => ({ value: f.id, label: f.label }))}
        />
      </Stack>

      {state.freq === 'minutes' && (
        <Stack direction="horizontal" gap="comfortable" align="end">
          <Stack grow minWidth="form-field">
            <FormField label="Run every" id={`${id}-interval`} hint="Minutes between fires.">
              <Input
                id={`${id}-interval`}
                mono
                inputMode="numeric"
                value={state.interval}
                onChange={(e) => patch({ interval: e.target.value })}
                endAdornment={
                  <Text as="span" size="caption" color="subtle">
                    minutes
                  </Text>
                }
              />
            </FormField>
          </Stack>
          <Stack direction="horizontal" gap="snug" shrink={false}>
            {INTERVAL_PRESETS.map((v) => (
              <Button
                key={v}
                size="sm"
                variant={state.interval === v ? 'accent-outline' : 'outline-muted'}
                aria-pressed={state.interval === v}
                onClick={() => patch({ interval: v })}
              >
                {v}m
              </Button>
            ))}
          </Stack>
        </Stack>
      )}

      {state.freq === 'hourly' && (
        <Stack direction="horizontal" gap="comfortable" align="end">
          <Stack grow minWidth="form-field">
            <FormField label="Minutes past the hour" id={`${id}-at-minute`}>
              <Input
                id={`${id}-at-minute`}
                mono
                inputMode="numeric"
                value={state.atMinute}
                onChange={(e) => patch({ atMinute: e.target.value })}
              />
            </FormField>
          </Stack>
          <Stack direction="horizontal" gap="snug" shrink={false}>
            {MINUTE_PRESETS.map((v) => (
              <Button
                key={v}
                size="sm"
                variant={state.atMinute === v ? 'accent-outline' : 'outline-muted'}
                aria-pressed={state.atMinute === v}
                onClick={() => patch({ atMinute: v })}
              >
                {`:${v.padStart(2, '0')}`}
              </Button>
            ))}
          </Stack>
        </Stack>
      )}

      {state.freq === 'weekly' && (
        <Stack gap="snug">
          <SectionLabel>On these days</SectionLabel>
          <Stack direction="horizontal" gap="snug" wrap>
            {WEEKDAYS.map((d) => {
              const on = state.weekdays.includes(d.n);
              return (
                <Button
                  key={d.n}
                  size="sm"
                  variant={on ? 'accent-outline' : 'outline-muted'}
                  aria-pressed={on}
                  onClick={() => patch({ weekdays: toggleWeekday(state.weekdays, d.n) })}
                >
                  {d.s}
                </Button>
              );
            })}
          </Stack>
        </Stack>
      )}

      {(state.freq === 'monthly' || showTime) && (
        <Stack direction="horizontal" gap="comfortable">
          {state.freq === 'monthly' && (
            <Stack grow minWidth="form-field-sm">
              <FormField label="Day of month" id={`${id}-dom`}>
                <Input
                  id={`${id}-dom`}
                  mono
                  inputMode="numeric"
                  value={state.dom}
                  onChange={(e) => patch({ dom: e.target.value })}
                />
              </FormField>
            </Stack>
          )}
          {showTime && (
            <Stack grow minWidth="form-field-sm">
              <FormField label="At" id={`${id}-time`}>
                <Input
                  id={`${id}-time`}
                  mono
                  type="time"
                  value={state.time}
                  onChange={(e) => patch({ time: e.target.value })}
                />
              </FormField>
            </Stack>
          )}
        </Stack>
      )}

      {summary}

      <Collapsible open={state.advancedOpen} onOpenChange={(open) => patch({ advancedOpen: open })}>
        <CollapsibleTrigger asChild>
          <Button variant="outline" size="sm" fullWidth>
            <Stack direction="horizontal" align="center" gap="snug" grow>
              <Text as="span" size="caption" color="muted" weight="strong">
                Advanced: cron expression
              </Text>
              <Stack grow />
              <Text as="span" size="caption" mono color="accent">
                {derivedCron}
              </Text>
              <Icon
                name="chevron-down"
                size="xs"
                color="subtle"
                rotate={state.advancedOpen ? 180 : 0}
              />
            </Stack>
          </Button>
        </CollapsibleTrigger>
        <CollapsibleContent>
          <Stack gap="snug" paddingTop="snug">
            <Input
              mono
              aria-label="Cron expression"
              value={state.freq === 'custom' ? state.customCron : derivedCron}
              onChange={(e) => patch({ customCron: e.target.value, freq: 'custom' })}
              placeholder="0 2 * * *"
              filled={isValidCron(derivedCron) ? undefined : 'danger'}
            />
            <Stack direction="horizontal" align="center" gap="snug">
              <Text size="caption" color="subtle">
                Editing this switches the schedule to a custom expression.
              </Text>
              <Stack grow />
              {state.freq === 'custom' && (
                <Button size="xs" variant="secondary" onClick={() => patch({ freq: 'daily' })}>
                  Use builder
                </Button>
              )}
            </Stack>
          </Stack>
        </CollapsibleContent>
      </Collapsible>

      <Stack gap="snug">
        <SectionLabel>Timezone</SectionLabel>
        <RadioGroup
          value={state.timezone}
          onValueChange={(v) => patch({ timezone: v })}
          aria-label="Timezone"
        >
          <Grid cols={2} gap="cozy">
            {zoneOptions.map((z) => (
              <RadioCard
                key={z}
                value={z}
                selected={state.timezone === z}
                onSelect={(v) => patch({ timezone: v })}
                icon="globe"
                title={z}
                description={
                  ZONE_CITY[z] ?? (z === browserZone ? 'Your browser timezone' : 'Saved timezone')
                }
              />
            ))}
          </Grid>
        </RadioGroup>
      </Stack>

      <ToggleRow
        label="Catch up on missed runs"
        description="Replay ticks skipped while the platform was unavailable. Off by default: running the work twice is usually worse than skipping it."
        checked={state.catchUp}
        onChange={(next) => patch({ catchUp: next })}
      />
    </Stack>
  );
}
