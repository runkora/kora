import { describe, expect, it } from 'vitest';

import {
  builderPatchFor,
  cronBreakdown,
  cronFor,
  describeCron,
  humanFor,
  isValidCron,
  minuteMarks,
  monthDayNote,
  ordinal,
  specFromCron,
  timezoneOptions,
  toggleWeekday,
  utcLabel,
  zoneClockName,
  type CronSpec,
} from './cron';

function spec(overrides: Partial<CronSpec> = {}): CronSpec {
  return {
    freq: 'daily',
    interval: '15',
    atMinute: '0',
    time: '02:00',
    weekdays: [1],
    dom: '1',
    customCron: '0 2 * * *',
    ...overrides,
  };
}

describe('cronFor', () => {
  it('builds a minutes interval', () => {
    expect(cronFor(spec({ freq: 'minutes', interval: '15' }))).toBe('*/15 * * * *');
    expect(cronFor(spec({ freq: 'minutes', interval: '5' }))).toBe('*/5 * * * *');
  });

  it('falls back to 15 minutes when the interval is unusable', () => {
    expect(cronFor(spec({ freq: 'minutes', interval: '' }))).toBe('*/15 * * * *');
    expect(cronFor(spec({ freq: 'minutes', interval: '0' }))).toBe('*/15 * * * *');
  });

  it('builds an hourly expression from minutes past the hour', () => {
    expect(cronFor(spec({ freq: 'hourly', atMinute: '0' }))).toBe('0 * * * *');
    expect(cronFor(spec({ freq: 'hourly', atMinute: '30' }))).toBe('30 * * * *');
  });

  it('builds a daily expression from the time', () => {
    expect(cronFor(spec({ freq: 'daily', time: '02:00' }))).toBe('0 2 * * *');
    expect(cronFor(spec({ freq: 'daily', time: '09:45' }))).toBe('45 9 * * *');
  });

  it('builds a weekly expression with sorted days', () => {
    expect(cronFor(spec({ freq: 'weekly', time: '09:00', weekdays: [1] }))).toBe('0 9 * * 1');
    expect(cronFor(spec({ freq: 'weekly', time: '09:00', weekdays: [5, 1, 3] }))).toBe(
      '0 9 * * 1,3,5',
    );
  });

  it('falls back to Monday when no weekday is selected', () => {
    expect(cronFor(spec({ freq: 'weekly', time: '09:00', weekdays: [] }))).toBe('0 9 * * 1');
  });

  it('builds a monthly expression from the day of month', () => {
    expect(cronFor(spec({ freq: 'monthly', time: '02:00', dom: '3' }))).toBe('0 2 3 * *');
    expect(cronFor(spec({ freq: 'monthly', time: '02:00', dom: '' }))).toBe('0 2 1 * *');
  });

  it('passes a custom expression through untouched', () => {
    expect(cronFor(spec({ freq: 'custom', customCron: '*/7 3 * * 2' }))).toBe('*/7 3 * * 2');
  });
});

describe('ordinal', () => {
  it('suffixes the common cases', () => {
    expect(ordinal(1)).toBe('1st');
    expect(ordinal(2)).toBe('2nd');
    expect(ordinal(3)).toBe('3rd');
    expect(ordinal(4)).toBe('4th');
  });

  it('suffixes the teens as th', () => {
    expect(ordinal(11)).toBe('11th');
    expect(ordinal(12)).toBe('12th');
    expect(ordinal(13)).toBe('13th');
  });

  it('suffixes the twenties by their last digit', () => {
    expect(ordinal(21)).toBe('21st');
    expect(ordinal(22)).toBe('22nd');
    expect(ordinal(23)).toBe('23rd');
    expect(ordinal(31)).toBe('31st');
  });
});

describe('humanFor', () => {
  it('describes minute and hourly schedules', () => {
    expect(humanFor(spec({ freq: 'minutes', interval: '10' }))).toBe('Every 10 minutes');
    expect(humanFor(spec({ freq: 'hourly', atMinute: '0' }))).toBe(
      'Every hour at the top of the hour',
    );
    expect(humanFor(spec({ freq: 'hourly', atMinute: '15' }))).toBe(
      'Every hour at 15 minutes past',
    );
  });

  it('describes a daily schedule', () => {
    expect(humanFor(spec({ freq: 'daily', time: '02:00' }))).toBe('Every day at 02:00');
  });

  it('collapses the working week to "weekdays"', () => {
    expect(humanFor(spec({ freq: 'weekly', time: '09:00', weekdays: [1, 2, 3, 4, 5] }))).toBe(
      'Every week on weekdays at 09:00',
    );
  });

  it('collapses a full week to "every day"', () => {
    expect(humanFor(spec({ freq: 'weekly', time: '09:00', weekdays: [0, 1, 2, 3, 4, 5, 6] }))).toBe(
      'Every week on every day at 09:00',
    );
  });

  it('lists partial weeks in Monday-first order', () => {
    expect(humanFor(spec({ freq: 'weekly', time: '09:00', weekdays: [0, 3] }))).toBe(
      'Every week on Wed, Sun at 09:00',
    );
  });

  it('names Monday when nothing is selected', () => {
    expect(humanFor(spec({ freq: 'weekly', time: '09:00', weekdays: [] }))).toBe(
      'Every week on Monday at 09:00',
    );
  });

  it('describes a monthly schedule with an ordinal', () => {
    expect(humanFor(spec({ freq: 'monthly', time: '06:30', dom: '22' }))).toBe(
      'Monthly on the 22nd at 06:30',
    );
  });

  it('does not pretend to read a custom expression', () => {
    expect(humanFor(spec({ freq: 'custom', customCron: '*/7 3 * * 2' }))).toBe('Custom schedule');
  });
});

describe('toggleWeekday', () => {
  it('adds a day that is not selected', () => {
    expect(toggleWeekday([1], 3)).toEqual([1, 3]);
  });

  it('removes a selected day', () => {
    expect(toggleWeekday([1, 3], 3)).toEqual([1]);
  });

  it('refuses to empty the selection', () => {
    expect(toggleWeekday([3], 3)).toEqual([3]);
  });
});

describe('timezoneOptions', () => {
  it('appends the browser zone when it is not already listed', () => {
    expect(timezoneOptions('America/New_York')).toEqual([
      'UTC',
      'Europe/London',
      'Australia/Sydney',
      'America/New_York',
    ]);
  });

  it('does not duplicate a zone already offered', () => {
    expect(timezoneOptions('Europe/London')).toEqual(['UTC', 'Europe/London', 'Australia/Sydney']);
  });

  it('tolerates an unknown browser zone', () => {
    expect(timezoneOptions(undefined)).toEqual(['UTC', 'Europe/London', 'Australia/Sydney']);
  });
});

describe('isValidCron', () => {
  it('accepts five-field expressions', () => {
    expect(isValidCron('0 2 * * *')).toBe(true);
    expect(isValidCron('*/15 * * * *')).toBe(true);
    expect(isValidCron('0 9 * * 1-5')).toBe(true);
    expect(isValidCron('30 3 1 * *')).toBe(true);
  });

  it('tolerates surrounding and repeated whitespace', () => {
    expect(isValidCron('  0   2 * * *  ')).toBe(true);
  });

  it('rejects the wrong number of fields', () => {
    expect(isValidCron('')).toBe(false);
    expect(isValidCron('0 2 * *')).toBe(false);
    expect(isValidCron('0 2 * * * *')).toBe(false);
  });

  it('rejects a field with characters no cron field carries', () => {
    expect(isValidCron('0 2 * * every-day!')).toBe(false);
    expect(isValidCron('0 2 * * $')).toBe(false);
  });
});

describe('describeCron', () => {
  it('phrases a stored expression exactly as the builder would', () => {
    expect(describeCron('*/15 * * * *')).toBe('Every 15 minutes');
    expect(describeCron('0 * * * *')).toBe('Every hour at the top of the hour');
    expect(describeCron('15 * * * *')).toBe('Every hour at 15 minutes past');
    expect(describeCron('0 2 * * *')).toBe('Every day at 02:00');
    expect(describeCron('30 9 * * 1,2,3,4,5')).toBe('Every week on weekdays at 09:30');
    expect(describeCron('0 3 1 * *')).toBe('Monthly on the 1st at 03:00');
  });

  it('falls back to Custom schedule for shapes the builder cannot produce', () => {
    expect(describeCron('0 9 * * 1-5')).toBe('Custom schedule');
    expect(describeCron('0 2 1 6 *')).toBe('Custom schedule');
    expect(describeCron('not a cron')).toBe('Custom schedule');
    expect(describeCron('')).toBe('Custom schedule');
  });
});

describe('cronBreakdown', () => {
  it('names the field that is out of range', () => {
    const b = cronBreakdown('0 25 * * 1');
    expect(b.problem).toBe('Hour must be between 0 and 23.');
    expect(b.fields.map((f) => f.problem !== undefined)).toEqual([
      false,
      true,
      false,
      false,
      false,
    ]);
  });

  it('asks for five fields when there are not five', () => {
    expect(cronBreakdown('0 2 * *').problem).toBe('Needs five fields separated by spaces.');
  });

  it('leaves ranges, steps, lists and names to the scheduler', () => {
    expect(cronBreakdown('*/30 9-17 * * 1-5').problem).toBeUndefined();
    expect(cronBreakdown('0 9 * * MON,FRI').problem).toBeUndefined();
    expect(cronBreakdown('0 9 1,15 * *').problem).toBeUndefined();
  });
});

describe('builderPatchFor', () => {
  const current = spec();
  it('fills the builder from an expression it can show', () => {
    expect(builderPatchFor('0 6 * * 1,4', current)).toMatchObject({
      freq: 'weekly',
      time: '06:00',
      weekdays: [1, 4],
    });
  });
  it('replaces one it cannot show with a daily run at the fallback time', () => {
    expect(builderPatchFor('*/30 9-17 * * 1-5', current)).toEqual({
      freq: 'daily',
      time: '09:00',
    });
  });
});

describe('schedule display helpers', () => {
  it('lists the minutes an interval lands on', () => {
    expect(minuteMarks(15)).toBe(':00, :15, :30 and :45');
    expect(minuteMarks(30)).toBe(':00 and :30');
    expect(minuteMarks(5)).toBe(':00, :05, :10, :15 and so on');
  });
  it('names a zone by its clock', () => {
    expect(zoneClockName('Australia/Sydney')).toBe('Sydney time');
    expect(zoneClockName('America/New_York')).toBe('New York time');
    expect(zoneClockName('UTC')).toBe('UTC');
  });
  it('reads an instant in UTC', () => {
    expect(utcLabel('2026-11-01T19:00:00Z')).toBe('Sun 1 Nov, 19:00 UTC');
    expect(utcLabel('nope')).toBeNull();
  });
});

describe('specFromCron ranges', () => {
  it('leaves a time that does not exist as a raw expression', () => {
    expect(specFromCron('0 25 * * 1')).toBeNull();
    expect(specFromCron('61 * * * *')).toBeNull();
    expect(specFromCron('0 6 32 * *')).toBeNull();
  });
});

describe('monthDayNote', () => {
  it('says nothing up to the 28th and notes the months skipped after it', () => {
    expect(monthDayNote('28')).toBeUndefined();
    expect(monthDayNote('')).toBeUndefined();
    expect(monthDayNote('29')).toMatch(/leap/);
    expect(monthDayNote('30')).toMatch(/11 times/);
    expect(monthDayNote('31')).toMatch(/7 times/);
  });
});
