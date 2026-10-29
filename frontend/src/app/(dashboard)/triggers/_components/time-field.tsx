'use client';

/**
 * @tier 3 — triggers-scoped. A 24-hour time of day as one field made of an hour and a minute
 * input, with the clock it is read on at the trailing edge. Replaces `<input type="time">`,
 * whose picker is drawn by the browser: it cannot take our styling, and it follows the system
 * locale into AM/PM whatever the page says.
 */

import { useState } from 'react';
import { Card, Input, Stack, Text } from '@runkora/hades';

interface TimeFieldProps {
  /** `HH:MM`, 24-hour. */
  value: string;
  onChange: (value: string) => void;
  /** Ids for the two inputs, so a label can point at the hour. */
  id: string;
  /** The clock the time is read on, shown at the trailing edge: "Sydney time". */
  zoneLabel?: string;
  disabled?: boolean;
}

const pad = (part: string) => part.padStart(2, '0');

/** Digits only, at most two, held to the field's range so a typed 99 never gets through. */
function clean(raw: string, max: number): string {
  const digits = raw.replace(/\D/g, '').slice(0, 2);
  return digits !== '' && Number(digits) > max ? String(max) : digits;
}

function split(value: string): { hour: string; minute: string } {
  const [hour = '', minute = ''] = value.split(':');
  return { hour: pad(hour), minute: pad(minute) };
}

export function TimeField({ value, onChange, id, zoneLabel, disabled }: TimeFieldProps) {
  // What is typed stays as typed, so clearing a field to retype it does not snap back to 00.
  const [typed, setTyped] = useState(() => split(value));
  const [emitted, setEmitted] = useState(value);
  // A time that came from outside (a different schedule, a reset) replaces what was typed.
  if (value !== emitted) {
    setTyped(split(value));
    setEmitted(value);
  }

  const set = (next: { hour: string; minute: string }) => {
    setTyped(next);
    // A part cleared to be retyped is not a time yet: the parent keeps the last whole one.
    if (next.hour === '' || next.minute === '') return;
    const time = `${pad(next.hour)}:${pad(next.minute)}`;
    setEmitted(time);
    onChange(time);
  };

  // Leaving a part empty puts back what the parent holds for it.
  const settle = () => {
    const held = split(value);
    setTyped((t) => ({
      hour: t.hour === '' ? held.hour : pad(t.hour),
      minute: t.minute === '' ? held.minute : pad(t.minute),
    }));
  };

  return (
    // One bordered field holding two inline inputs, so it reads as a single time while each
    // part is still its own text field. The field is as wide as its content: two digit-wide inputs and the zone label.
    <Stack align="start">
      <Card border="default">
        <Stack direction="horizontal" gap="none" align="center" padding="snug">
          <Stack shrink={false} style={{ width: '2.25rem' }}>
            <Input
              id={id}
              aria-label="Hour"
              aria-describedby={zoneLabel !== undefined ? `${id}-zone` : undefined}
              mono
              variant="inline"
              style={{ textAlign: 'center' }}
              inputMode="numeric"
              autoComplete="off"
              disabled={disabled}
              value={typed.hour}
              placeholder="HH"
              onChange={(e) => set({ ...typed, hour: clean(e.target.value, 23) })}
              onBlur={settle}
            />
          </Stack>
          <Text as="span" mono color="subtle">
            :
          </Text>
          <Stack shrink={false} style={{ width: '2.25rem' }}>
            <Input
              id={`${id}-minute`}
              aria-label="Minute"
              aria-describedby={zoneLabel !== undefined ? `${id}-zone` : undefined}
              mono
              variant="inline"
              style={{ textAlign: 'center' }}
              inputMode="numeric"
              autoComplete="off"
              disabled={disabled}
              value={typed.minute}
              placeholder="MM"
              onChange={(e) => set({ ...typed, minute: clean(e.target.value, 59) })}
              onBlur={settle}
            />
          </Stack>
          {zoneLabel !== undefined && (
            <>
              {/* Space between the minute and the label; the label keeps its original right padding. */}
              <Stack shrink={false} style={{ width: '1rem' }} />
              <Stack grow direction="horizontal" justify="end" paddingInline="snug">
                <Text as="span" id={`${id}-zone`} color="subtle">
                  {zoneLabel}
                </Text>
              </Stack>
            </>
          )}
        </Stack>
      </Card>
    </Stack>
  );
}
