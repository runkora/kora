'use client';

/** @tier 3 — triggers-scoped. The scheduled kind: a cron builder that always
 * shows the expression it derives, plus timezone and catch-up. */

import {
  Button,
  Callout,
  Card,
  CardContent,
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
  Textarea,
  ToggleRow,
} from '@orbit/terra';
import {
  FREQS,
  INTERVAL_PRESETS,
  MINUTE_PRESETS,
  WEEKDAYS,
  cronFor,
  humanFor,
  resolveBrowserZone,
  timezoneOptions,
  toggleWeekday,
} from '../cron';
import type { Frequency } from '../cron';
import { FIELD_IDS, VALID, firstBlocker, validateName, validatePrompt } from '../wizard-state';
import type {
  DoneContentProps,
  StepComponentProps,
  StepValidation,
  TriggerKindDefinition,
  WizardState,
} from '../types';

/** Cities are what a person recognises; the IANA name is what the platform stores. */
const ZONE_CITY: Record<string, string> = {
  UTC: 'Coordinated Universal Time',
  'Europe/London': 'London · GMT/BST',
  'Australia/Sydney': 'Sydney · AEST/AEDT',
};

function StepSchedule({ state, patch, fieldError, firstFieldRef }: StepComponentProps) {
  const derivedCron = cronFor(state);
  const human = humanFor(state);
  const zones = timezoneOptions(resolveBrowserZone());
  const showTime = state.freq === 'daily' || state.freq === 'weekly' || state.freq === 'monthly';

  return (
    <Stack gap="relaxed">
      <FormField
        label="Trigger name"
        id={FIELD_IDS.name}
        hint="Shown in the triggers list and on every run it starts."
        error={fieldError(FIELD_IDS.name)}
        required
      >
        <Input
          id={FIELD_IDS.name}
          ref={firstFieldRef}
          mono
          value={state.name}
          onChange={(e) => patch({ name: e.target.value })}
          placeholder="nightly-report"
          maxLength={128}
        />
      </FormField>

      <FormField
        label="How often"
        id="wizard-freq"
        hint="The builder writes the cron expression; you can edit it below."
      >
        <Stack direction="horizontal">
          <SegmentedControl
            aria-label="How often"
            value={state.freq}
            onChange={(v) => patch({ freq: v as Frequency })}
            options={FREQS.map((f) => ({ value: f.id, label: f.label }))}
          />
        </Stack>
      </FormField>

      {state.freq === 'minutes' && (
        <Stack direction="horizontal" gap="comfortable" align="end">
          <Stack maxWidth="search-field" grow>
            <FormField label="Run every" id="wizard-interval" hint="Minutes between fires.">
              <Input
                id="wizard-interval"
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
          <Stack maxWidth="search-field" grow>
            <FormField
              label="Minutes past the hour"
              id="wizard-at-minute"
              hint="0 runs at the top of every hour."
            >
              <Input
                id="wizard-at-minute"
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
          <Text size="caption" color="subtle">
            At least one day stays selected.
          </Text>
        </Stack>
      )}

      {(state.freq === 'monthly' || showTime) && (
        <Stack direction="horizontal" gap="comfortable">
          {state.freq === 'monthly' && (
            <Stack maxWidth="search-field" grow>
              <FormField label="Day of month" id="wizard-dom" hint="1 to 31.">
                <Input
                  id="wizard-dom"
                  mono
                  inputMode="numeric"
                  value={state.dom}
                  onChange={(e) => patch({ dom: e.target.value })}
                />
              </FormField>
            </Stack>
          )}
          {showTime && (
            <Stack maxWidth="search-field" grow>
              <FormField label="At" id="wizard-time" hint="In the timezone chosen below.">
                <Input
                  id="wizard-time"
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

      <Callout variant="info">
        <Stack direction="horizontal" gap="cozy" align="center">
          <Icon name="clock" size="sm" color="accent" label="" shrink />
          <Stack gap="tight">
            <Text size="supporting" weight="heavy">
              {human}
            </Text>
            <Text size="caption" color="muted">
              {state.timezone}
            </Text>
          </Stack>
        </Stack>
      </Callout>

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

      <FormField
        label="Timezone"
        id="wizard-timezone"
        hint="Daylight saving is applied for you; the platform stores the zone, not an offset."
      >
        <RadioGroup
          id="wizard-timezone"
          value={state.timezone}
          onValueChange={(v) => patch({ timezone: v })}
          aria-label="Timezone"
        >
          <Grid cols={2} gap="cozy">
            {zones.map((z) => (
              <RadioCard
                key={z}
                value={z}
                selected={state.timezone === z}
                onSelect={(v) => patch({ timezone: v })}
                icon="globe"
                title={z}
                description={ZONE_CITY[z] ?? 'Your browser timezone'}
              />
            ))}
          </Grid>
        </RadioGroup>
      </FormField>

      <ToggleRow
        label="Catch up on missed runs"
        description="Replay ticks skipped while the platform was unavailable. Off by default: running the work twice is usually worse than skipping it."
        checked={state.catchUp}
        onChange={(next) => patch({ catchUp: next })}
      />

      <FormField
        label="Prompt"
        id={FIELD_IDS.prompt}
        hint="What the run starts with. Every fire uses this prompt."
        error={fieldError(FIELD_IDS.prompt)}
        required
      >
        <Textarea
          id={FIELD_IDS.prompt}
          value={state.promptTemplate}
          onChange={(e) => patch({ promptTemplate: e.target.value })}
          placeholder="Summarise yesterday's failed deliveries and open an issue for anything unresolved."
          rows={4}
        />
      </FormField>
    </Stack>
  );
}

function validateCron(state: WizardState): StepValidation {
  if (!cronFor(state).trim())
    return { valid: false, blocker: 'Needs a cron expression', fieldId: 'wizard-freq' };
  return VALID;
}

function DoneContent({ state }: DoneContentProps) {
  return (
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
  );
}

export const scheduledKind: TriggerKindDefinition = {
  id: 'scheduled',
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
      description: 'Pick how often the agent should run, and in which timezone.',
      Component: StepSchedule,
      validate: (state) =>
        firstBlocker(
          () => validateName(state),
          () => validateCron(state),
          () => validatePrompt(state),
        ),
    },
  ],
  summarize: (state) => [
    {
      key: 'How often',
      value: humanFor(state),
      description: `Cron ${cronFor(state)} in ${state.timezone}.`,
    },
    { key: 'Timezone', value: state.timezone, mono: true, description: 'Daylight saving applied.' },
    {
      key: 'Catch up on missed runs',
      value: state.catchUp ? 'On' : 'Off',
      description: 'Replay ticks skipped while the platform was unavailable.',
    },
  ],
  draftPatch: (state) => ({
    schedule: {
      cronExpression: cronFor(state),
      timezone: state.timezone,
      catchUp: state.catchUp,
    },
  }),
  doneContent: DoneContent,
};
