'use client';

/**
 * @tier 3 — triggers-scoped. The schedule builder: how often a trigger fires,
 * the cron expression that derives, its timezone and catch-up.
 *
 * One component for both surfaces. The New trigger wizard feeds it its own
 * state; a stored trigger's Configuration tab feeds it the stored schedule
 * through `useScheduleDraft`. Whatever changes here changes on both.
 */

import { useId, useState, type ReactNode } from 'react';
import {
  Button,
  Card,
  Grid,
  Icon,
  Input,
  RadioCard,
  RadioGroup,
  SectionLabel,
  SegmentedControl,
  Separator,
  Stack,
  Text,
} from '@runkora/hades';
import {
  FALLBACK_TIME,
  FREQS,
  WEEKDAYS,
  builderPatchFor,
  cronBreakdown,
  monthDayNote,
  cronFor,
  fitsBuilder,
  humanFor,
  minuteMarks,
  resolveBrowserZone,
  specFromCron,
  timezoneOptions,
  toggleWeekday,
  utcLabel,
} from './trigger-wizard/cron';
import type { CronSpec, Frequency } from './trigger-wizard/cron';
import { PlainToggleRow } from './plain-toggle-row';
import { useSchedulePreview } from './trigger-wizard/use-schedule-preview';

/** Everything the builder edits. The wizard's state carries these fields, so it passes as is. */
export interface ScheduleDraft extends CronSpec {
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

/** What each zone's card says under its name. */
const ZONE_NOTE: Record<string, string> = {
  UTC: 'No daylight saving',
  'Europe/London': 'GMT / BST',
  'Australia/Sydney': 'AEST / AEDT',
};

const MODES = { builder: 'builder', custom: 'custom' } as const;

/** A short bold title over a line of help, above whatever it labels. */
function Labelled({
  title,
  hint,
  children,
}: {
  title: string;
  hint?: string;
  children: ReactNode;
}) {
  return (
    <Stack gap="snug">
      <Stack gap="tight">
        <Text as="span" size="supporting" weight="heavy">
          {title}
        </Text>
        {hint && (
          <Text size="caption" color="subtle">
            {hint}
          </Text>
        )}
      </Stack>
      {children}
    </Stack>
  );
}

interface ScheduleBuilderProps {
  value: ScheduleDraft;
  onChange: (patch: Partial<ScheduleDraft>) => void;
  /** Zones offered. Defaults to the platform's three plus the browser's own. */
  zones?: string[];
  /** Shows the schedule as read-only text, for a caller who may not change it. */
  disabled?: boolean;
}

/** When the schedule fires in words, then the scheduler's next fires beneath. */
function Summary({
  sentence,
  zone,
  fires,
  note,
  cron,
}: {
  sentence: string;
  zone: string;
  fires: { at: string; label: string }[];
  /** Replaces the sentence when there is nothing sound to describe. */
  note?: string;
  /** The expression the builder produced, shown at the trailing edge of the sentence's line. */
  cron?: string;
}) {
  const next = fires.slice(0, 3);
  return (
    <Stack gap="snug">
      <SectionLabel>Summary</SectionLabel>
      <Card subtle border="none">
        <Stack gap="comfortable" padding="cozy">
          <Stack direction="horizontal" gap="cozy" align="start">
            <Icon name="clock" size="sm" color="muted" label="" shrink />
            <Text size="supporting" color={note ? 'muted' : 'foreground'}>
              {note ?? (
                <>
                  <Text as="span" size="supporting" weight="heavy">
                    {sentence}
                  </Text>
                  <Text as="span" size="supporting" color="muted">
                    {` · ${zone}`}
                  </Text>
                </>
              )}
            </Text>
            {cron !== undefined && note === undefined && (
              <Stack grow justify="end" direction="horizontal">
                <Text as="span" mono size="supporting" color="muted">
                  {cron}
                </Text>
              </Stack>
            )}
          </Stack>
          {/* Only the scheduler can say when a run lands, so no fires are shown when it did not answer. */}
          {next.length > 0 && (
            <Stack gap="snug" paddingInline="comfortable">
              <Text size="caption" weight="strong" transform="uppercase" color="subtle">
                {`Next ${next.length} ${next.length === 1 ? 'run' : 'runs'}`}
              </Text>
              <Stack gap="none">
                {next.map((fire, i) => {
                  const utc = zone === 'UTC' ? null : utcLabel(fire.at);
                  return (
                    <Stack key={fire.at} gap="none">
                      {i > 0 && <Separator />}
                      <Stack
                        direction="horizontal"
                        justify="between"
                        align="center"
                        gap="comfortable"
                        paddingBlock="snug"
                      >
                        <Text as="span" size="supporting">
                          {fire.label}
                        </Text>
                        {utc && (
                          <Text as="span" size="caption" color="subtle">
                            {utc}
                          </Text>
                        )}
                      </Stack>
                    </Stack>
                  );
                })}
              </Stack>
            </Stack>
          )}
        </Stack>
      </Card>
    </Stack>
  );
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
  const custom = state.freq === 'custom';
  const breakdown = cronBreakdown(derivedCron);
  // The exact times come from the scheduler, not from this builder: two cron
  // interpretations that could disagree is exactly the bug worth avoiding.
  const preview = useSchedulePreview(derivedCron, state.timezone, breakdown.problem === undefined);
  const showTime = state.freq === 'daily' || state.freq === 'weekly' || state.freq === 'monthly';

  const summary = (
    <Summary
      sentence={human}
      zone={state.timezone}
      fires={preview.fires}
      cron={derivedCron}
      note={
        breakdown.problem !== undefined
          ? 'Fix the expression above to see when this runs.'
          : undefined
      }
    />
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

  // An expression typed by hand that the builder cannot show: going back replaces it.
  const unshowable = custom && state.customCron.trim() !== '' && !fitsBuilder(state.customCron);

  const chooseMode = (mode: string) => {
    if (mode === MODES.custom) {
      if (!custom) patch({ freq: 'custom', customCron: derivedCron });
      return;
    }
    if (custom) patch(builderPatchFor(state.customCron, state));
  };

  const interval = parseInt(state.interval, 10);

  return (
    <Stack gap="relaxed">
      <Labelled title="Set the schedule with">
        <RadioGroup
          value={custom ? MODES.custom : MODES.builder}
          onValueChange={chooseMode}
          aria-label="Set the schedule with"
        >
          <Grid cols={2} gap="cozy">
            <RadioCard
              value={MODES.builder}
              selected={!custom}
              onSelect={chooseMode}
              title="Cron builder"
              description={
                unshowable
                  ? `This expression can't be made with the cron builder. Switching replaces it with a daily run at ${FALLBACK_TIME}.`
                  : 'Choose how often and when. Covers most schedules.'
              }
              transparent
            />
            <RadioCard
              value={MODES.custom}
              selected={custom}
              onSelect={chooseMode}
              title="Custom expression"
              description="Write your own for full control, using standard cron syntax."
              transparent
            />
          </Grid>
        </RadioGroup>
      </Labelled>

      <Stack direction="horizontal" gap="cozy">
        <Separator orientation="vertical" />
        <Stack grow minWidth="none" gap="comfortable">
          {custom ? (
            <Labelled
              title="Expression"
              hint="Five fields separated by spaces. Each part is explained below as you type."
            >
              <Stack gap="snug">
                <Input
                  mono
                  aria-label="Cron expression"
                  value={state.customCron}
                  onChange={(e) => patch({ customCron: e.target.value, freq: 'custom' })}
                  placeholder="0 2 * * *"
                  filled={breakdown.problem === undefined ? undefined : 'danger'}
                />
                {breakdown.problem !== undefined && (
                  <Stack direction="horizontal" gap="snug" align="center">
                    <Icon name="circle-alert" size="xs" color="status-error" label="" shrink />
                    <Text size="caption" color="status-error">
                      {breakdown.problem}
                    </Text>
                  </Stack>
                )}
                <Grid cols={5} gap="snug">
                  {breakdown.fields.map((field) => (
                    <Card key={field.label} subtle border="none">
                      <Stack gap="none" align="center" padding="snug" textAlign="center">
                        <Text
                          as="span"
                          mono
                          weight="heavy"
                          color={field.problem ? 'status-error' : 'foreground'}
                        >
                          {field.value || '·'}
                        </Text>
                        <Text
                          as="span"
                          size="caption"
                          color={field.problem ? 'status-error' : 'subtle'}
                        >
                          {field.label}
                        </Text>
                      </Stack>
                    </Card>
                  ))}
                </Grid>
              </Stack>
            </Labelled>
          ) : (
            <>
              <Labelled title="Repeats">
                <SegmentedControl
                  aria-label="Frequency"
                  value={state.freq}
                  onChange={(v) => patch({ freq: v as Frequency })}
                  options={FREQS.map((f) => ({ value: f.id, label: f.label }))}
                />
              </Labelled>

              {state.freq === 'minutes' && (
                <Labelled
                  title="Every"
                  hint={`Counted from the top of each hour: runs at ${minuteMarks(
                    interval > 0 ? interval : 15,
                  )}.`}
                >
                  <Stack maxWidth="form-field">
                    <Input
                      id={`${id}-interval`}
                      aria-label="Run every"
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
                  </Stack>
                </Labelled>
              )}

              {state.freq === 'hourly' && (
                <Labelled
                  title="At minute"
                  hint={`Runs at 00:${state.atMinute.padStart(2, '0')}, 01:${state.atMinute.padStart(2, '0')}, 02:${state.atMinute.padStart(2, '0')} and so on.`}
                >
                  <Stack maxWidth="form-field">
                    <Input
                      id={`${id}-at-minute`}
                      aria-label="Minutes past the hour"
                      mono
                      inputMode="numeric"
                      value={state.atMinute}
                      onChange={(e) => patch({ atMinute: e.target.value })}
                      endAdornment={
                        <Text as="span" size="caption" color="subtle">
                          past the hour
                        </Text>
                      }
                    />
                  </Stack>
                </Labelled>
              )}

              {state.freq === 'weekly' && (
                <Labelled title="On" hint="Pick one or more days.">
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
                </Labelled>
              )}

              {state.freq === 'monthly' && (
                <Labelled title="On day" hint={monthDayNote(state.dom) ?? 'Day of the month.'}>
                  <Stack maxWidth="form-field">
                    <Input
                      id={`${id}-dom`}
                      aria-label="Day of month"
                      mono
                      inputMode="numeric"
                      value={state.dom}
                      onChange={(e) => patch({ dom: e.target.value })}
                      endAdornment={
                        <Text as="span" size="caption" color="subtle">
                          of the month
                        </Text>
                      }
                    />
                  </Stack>
                </Labelled>
              )}

              {showTime && (
                <Labelled title="At" hint="24-hour clock.">
                  <Stack maxWidth="form-field-sm">
                    <Input
                      id={`${id}-time`}
                      aria-label="At"
                      mono
                      type="time"
                      value={state.time}
                      onChange={(e) => patch({ time: e.target.value })}
                    />
                  </Stack>
                </Labelled>
              )}
            </>
          )}
        </Stack>
      </Stack>

      <Labelled
        title="Timezone"
        hint="Times above are read in this timezone, including daylight saving changes."
      >
        <RadioGroup
          value={state.timezone}
          onValueChange={(v) => patch({ timezone: v })}
          aria-label="Timezone"
        >
          <Grid cols={3} gap="cozy">
            {zoneOptions.map((z) => (
              <RadioCard
                key={z}
                value={z}
                selected={state.timezone === z}
                onSelect={(v) => patch({ timezone: v })}
                title={z}
                description={
                  ZONE_NOTE[z] ?? (z === browserZone ? 'Your browser timezone' : 'Saved timezone')
                }
                transparent
              />
            ))}
          </Grid>
        </RadioGroup>
      </Labelled>

      {summary}

      <Stack gap="snug">
        <SectionLabel>Missed runs</SectionLabel>
        <PlainToggleRow
          label="Catch up on missed runs"
          description="Replay ticks skipped while the platform was unavailable. Off by default: running the work twice is usually worse than skipping it."
          checked={state.catchUp}
          onChange={(next) => patch({ catchUp: next })}
        />
      </Stack>
    </Stack>
  );
}
